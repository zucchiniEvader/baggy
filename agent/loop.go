package agent

import (
	"context"
	"sync"

	"github.com/spike/gobot/tool"
)

// Model is the slice of provider.Provider the loop needs; defined here so the
// agent package does not import provider.
type Model interface {
	// Call runs one request over msgs and returns the assistant message,
	// forwarding deltas to emit as they arrive.
	Call(ctx context.Context, system string, msgs []Message, tools []tool.Tool, emit func(Event)) Message
}

// EventKind names what happened in a run.
type EventKind string

const (
	EventTurnStart     EventKind = "turn_start"
	EventMessageStart  EventKind = "message_start" // Message is nil until the stream ends, except for injected messages
	EventTextDelta     EventKind = "text_delta"
	EventThinkingDelta EventKind = "thinking_delta"
	EventMessageEnd    EventKind = "message_end" // Message is final: persist it here
	EventToolStart     EventKind = "tool_start"  // Call is set
	EventToolEnd       EventKind = "tool_end"    // Call and Result are set
	EventTurnEnd       EventKind = "turn_end"
)

// Event is what a UI or store observes while a turn runs. Run delivers events
// one at a time, in order, even while tools run in parallel.
type Event struct {
	Kind    EventKind
	Delta   string       // text_delta, thinking_delta
	Message *Message     // message_end, and message_start of an injected message
	Call    *Part        // tool_start, tool_end
	Result  *tool.Result // tool_end
}

type Hooks struct {
	// BeforeToolCall may return a non-nil Result to block the call.
	BeforeToolCall func(call Part) *tool.Result
	AfterToolCall  func(call Part, r tool.Result) tool.Result
}

type Options struct {
	System string
	Tools  []tool.Tool
	Hooks  Hooks
	Emit   func(Event)

	// Steering is read before the first model call and after each batch of
	// tool results; its messages reach the model on its next call.
	// FollowUp is read when the model would otherwise stop (steering first).
	// Either may be nil. A failed or aborted reply ends the run without
	// reading them.
	Steering *Queue
	FollowUp *Queue

	// Compact, when set, runs before each model call with the messages it
	// would send; when it returns ok, the call and the rest of the run use
	// its replacement (a summary and what was kept) instead.
	Compact func(ctx context.Context, msgs []Message) (replacement []Message, ok bool)
}

// Run appends to msgs until the model stops asking for tools and no queued
// message is left, and returns the new messages, tool results and injected
// queue messages included. Tool calls of one assistant message run in parallel.
func Run(ctx context.Context, m Model, msgs []Message, o Options) []Message {
	var mu sync.Mutex
	emitTo := o.Emit
	if emitTo == nil {
		emitTo = func(Event) {}
	}
	emit := func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		emitTo(e)
	}
	byName := map[string]tool.Tool{}
	for _, t := range o.Tools {
		byName[t.Name()] = t
	}

	var out []Message
	add := func(ms ...Message) {
		for _, msg := range ms {
			out = append(out, msg)
			emit(Event{Kind: EventMessageStart, Message: &msg})
			emit(Event{Kind: EventMessageEnd, Message: &msg})
		}
	}

	emit(Event{Kind: EventTurnStart})
	add(o.Steering.Drain()...)
	base, since := msgs, 0 // the call sends base and out[since:]
	for {
		all := append(append([]Message{}, base...), out[since:]...)
		if o.Compact != nil {
			if rep, ok := o.Compact(ctx, all); ok {
				base, since, all = rep, len(out), rep
			}
		}
		emit(Event{Kind: EventMessageStart})
		reply := m.Call(ctx, o.System, all, o.Tools, emit)
		out = append(out, reply)
		emit(Event{Kind: EventMessageEnd, Message: &reply})

		if reply.StopReason == StopError || reply.StopReason == StopAborted || ctx.Err() != nil {
			break
		}
		if reply.StopReason == StopToolUse {
			var calls []Part
			for _, p := range reply.Parts {
				if p.Kind == PartToolCall {
					calls = append(calls, p)
				}
			}
			results := make([]Part, len(calls))
			var wg sync.WaitGroup
			for i, c := range calls {
				wg.Add(1)
				go func() {
					defer wg.Done()
					emit(Event{Kind: EventToolStart, Call: &c})
					r := execute(ctx, byName, o.Hooks, c)
					results[i] = Part{Kind: PartToolResult, CallID: c.CallID, Name: c.Name, Text: r.Text, IsError: r.IsError}
					emit(Event{Kind: EventToolEnd, Call: &c, Result: &r})
				}()
			}
			wg.Wait()
			add(Message{Role: RoleTool, Parts: results})
			if ctx.Err() != nil {
				break
			}
			add(o.Steering.Drain()...)
			continue
		}
		next := o.Steering.Drain()
		if len(next) == 0 {
			next = o.FollowUp.Drain()
		}
		if len(next) == 0 {
			break
		}
		add(next...)
	}
	emit(Event{Kind: EventTurnEnd})
	return out
}

func execute(ctx context.Context, tools map[string]tool.Tool, h Hooks, c Part) tool.Result {
	if h.BeforeToolCall != nil {
		if r := h.BeforeToolCall(c); r != nil {
			return *r
		}
	}
	t, ok := tools[c.Name]
	if !ok {
		return tool.Result{Text: "unknown tool " + c.Name, IsError: true}
	}
	args, err := tool.ValidateArgs(t.Name(), t.Schema(), c.Args)
	if err != nil {
		return tool.Result{Text: err.Error(), IsError: true}
	}
	r := t.Execute(ctx, args)
	if h.AfterToolCall != nil {
		r = h.AfterToolCall(c, r)
	}
	return r
}
