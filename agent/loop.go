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

// Event is what a UI or store observes while a turn runs.
type Event struct {
	Kind    string // turn_start, text_delta, thinking_delta, tool_start, tool_end, message_end, turn_end
	Delta   string
	Message *Message
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
}

// Run appends to msgs until the model stops asking for tools, and returns the
// new messages. Tool calls of one assistant message run in parallel.
func Run(ctx context.Context, m Model, msgs []Message, o Options) []Message {
	emit := o.Emit
	if emit == nil {
		emit = func(Event) {}
	}
	byName := map[string]tool.Tool{}
	for _, t := range o.Tools {
		byName[t.Name()] = t
	}

	var out []Message
	emit(Event{Kind: "turn_start"})
	for {
		all := append(append([]Message{}, msgs...), out...)
		reply := m.Call(ctx, o.System, all, o.Tools, emit)
		out = append(out, reply)
		emit(Event{Kind: "message_end", Message: &reply})

		if reply.StopReason != StopToolUse {
			break
		}
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
				emit(Event{Kind: "tool_start"})
				r := execute(ctx, byName, o.Hooks, c)
				results[i] = Part{Kind: PartToolResult, CallID: c.CallID, Name: c.Name, Text: r.Text, IsError: r.IsError}
				emit(Event{Kind: "tool_end"})
			}()
		}
		wg.Wait()
		out = append(out, Message{Role: RoleTool, Parts: results})
		if ctx.Err() != nil {
			break
		}
	}
	emit(Event{Kind: "turn_end"})
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
	r := t.Execute(ctx, c.Args)
	if h.AfterToolCall != nil {
		r = h.AfterToolCall(c, r)
	}
	return r
}
