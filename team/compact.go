package team

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/provider/catalog"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/tool"
)

// Compaction summarizes the older part of a chat when a request would grow
// past its share of the model's context window.
type Compaction struct {
	// Window is the model's context window; nil looks it up in the catalog.
	Window func(model string) int
	// Threshold is the share of the window a request may fill before it
	// compacts (0.8 when zero); Keep the share the newest messages kept
	// verbatim may take (0.25 when zero).
	Threshold, Keep float64
	// NoFlush skips the memory flush that runs first.
	NoFlush bool
	// FlushTimeout bounds the flush (two minutes when zero).
	FlushTimeout time.Duration
}

const flushPrompt = "[Context is about to be compacted: the messages before %q will be replaced by a summary. " +
	"Save what is durable and not yet in your memory: facts with memory_update, events with memory_log. " +
	"Only those two tools run now; nothing you say reaches the chat. Reply DONE when you are finished.]"

const summarySystem = "You write the summary an assistant continues a conversation from, after its older messages are dropped. Write it for the assistant, in the second person."

const summaryPrompt = `Summarize the conversation in <transcript> so that the assistant can continue it without the original messages. Use these sections, leaving out any that would be empty:

## Goal
## Decisions and facts
## Work done (files, commands, results)
## Open items
## The user's preferences

Keep names, ids, paths, numbers and quotes the user insisted on exactly. If the transcript starts with an earlier summary, fold it in. At most 700 words.

<transcript>
%s
</transcript>`

func (c *Compaction) window(model string) int {
	if c.Window != nil {
		if w := c.Window(model); w > 0 {
			return w
		}
	}
	return catalog.Window(model)
}

func or(v, def float64) float64 {
	if v <= 0 {
		return def
	}
	return v
}

// compactor is the Compact hook of a turn.
func (t *Team) compactor(b store.Bot, chat store.Chat, model agent.Model, modelID, system string, tools []tool.Tool) func(context.Context, []agent.Message) ([]agent.Message, bool) {
	c := t.Compaction
	return func(ctx context.Context, msgs []agent.Message) ([]agent.Message, bool) {
		window := c.window(modelID)
		fixed := agent.TextTokens(system)
		for _, tl := range tools {
			fixed += agent.TextTokens(tl.Description()) + agent.TextTokens(string(tl.Schema()))
		}
		if fixed+agent.EstimateTokens(msgs) < int(or(c.Threshold, 0.8)*float64(window)) {
			return nil, false
		}
		k := cutPoint(msgs, int(or(c.Keep, 0.25)*float64(window)))
		if k <= 0 {
			return nil, false
		}
		keepID := msgs[k].ID
		if !c.NoFlush && t.Memory {
			t.flush(ctx, b, model, system, tools, msgs, oneLine(textOf(msgs[k]), 60))
		}
		summary, err := summarize(ctx, model, msgs[:k], window)
		if err != nil {
			t.emit(Event{Kind: EventError, ChatID: chat.ID, BotID: b.ID, Err: fmt.Errorf("compaction: %w", err)})
			return nil, false
		}
		m := agent.Message{ID: store.NewID("msg"), Role: agent.RoleUser, Kind: KindCompaction,
			Parts: []agent.Part{{Kind: agent.PartText, Text: "[Summary of the conversation before this point]\n\n" + summary}}}
		e, err := t.Store.Append(chat.ID, b.ID, "", store.Meta{KeepID: keepID}, m)
		if err != nil {
			t.emit(Event{Kind: EventError, ChatID: chat.ID, BotID: b.ID, Err: err})
			return nil, false
		}
		t.emit(Event{Kind: EventEntry, ChatID: chat.ID, BotID: b.ID, Entry: &e})
		return append([]agent.Message{m}, msgs[k:]...), true
	}
}

// cutPoint is where the kept part starts: the oldest user message (not a
// tool result) whose tail fits keep tokens, so a call and its result are
// never parted. It is 0 when nothing can go.
func cutPoint(msgs []agent.Message, keep int) int {
	size, k := 0, 0
	for i := len(msgs) - 1; i > 0; i-- {
		size += agent.MessageTokens(msgs[i])
		if size > keep && k > 0 {
			break
		}
		if msgs[i].Role == agent.RoleUser && msgs[i].ID != "" && msgs[i].Kind != KindCompaction {
			k = i
		}
	}
	return k
}

// flush is the silent memory turn before a compaction: the same request
// with a prompt to save what is durable, only memory_update and memory_log
// allowed to run. What it says goes nowhere; a failure is ignored.
func (t *Team) flush(ctx context.Context, b store.Bot, model agent.Model, system string, tools []tool.Tool, msgs []agent.Message, keepStart string) {
	timeout := t.Compaction.FlushTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	prompt := agent.Message{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: fmt.Sprintf(flushPrompt, keepStart)}}}
	agent.Run(ctx, model, append(append([]agent.Message(nil), msgs...), prompt), agent.Options{
		System: system,
		Tools:  tools,
		Hooks: agent.Hooks{BeforeToolCall: func(call agent.Part) *tool.Result {
			if call.Name == "memory_update" || call.Name == "memory_log" {
				return nil
			}
			return &tool.Result{Text: "Only memory_update and memory_log run now.", IsError: true}
		}},
	})
}

// summarize asks the model for a summary of msgs as a transcript in text,
// which any model reads whoever wrote it.
func summarize(ctx context.Context, model agent.Model, msgs []agent.Message, window int) (string, error) {
	text := Transcript(msgs)
	if max := window * 2; len(text) > max { // about half the window in tokens
		text = "[…]\n" + text[len(text)-max:]
	}
	req := agent.Message{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: fmt.Sprintf(summaryPrompt, text)}}}
	reply := model.Call(ctx, summarySystem, []agent.Message{req}, nil, func(agent.Event) {})
	if reply.StopReason == agent.StopError || reply.StopReason == agent.StopAborted {
		if t := errorText(reply); t != "" {
			return "", errors.New(t)
		}
		return "", errors.New("the summary call failed")
	}
	s := strings.TrimSpace(textOf(reply))
	if s == "" {
		return "", errors.New("the summary is empty")
	}
	return s, nil
}

func errorText(m agent.Message) string {
	var b strings.Builder
	for _, p := range m.Parts {
		if p.Kind == agent.PartText && p.IsError {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// Transcript is msgs as plain text: who said what, tool calls and their
// results cut short.
func Transcript(msgs []agent.Message) string {
	var s strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case agent.RoleUser:
			if m.Kind == KindCompaction {
				s.WriteString("Earlier summary:\n" + textOf(m) + "\n\n")
				continue
			}
			s.WriteString("User: " + textOf(m))
			for _, p := range m.Parts {
				if p.Kind == agent.PartImage {
					s.WriteString(" [image]")
				}
			}
			s.WriteString("\n\n")
		case agent.RoleAssistant:
			if t := strings.TrimSpace(textOf(m)); t != "" {
				s.WriteString("Assistant: " + t + "\n\n")
			}
			for _, p := range m.Parts {
				if p.Kind == agent.PartToolCall {
					s.WriteString("Assistant called " + p.Name + " " + clip(string(p.Args), 300) + "\n")
				}
			}
		case agent.RoleTool:
			for _, p := range m.Parts {
				if p.Kind == agent.PartToolResult {
					status := "result"
					if p.IsError {
						status = "error"
					}
					s.WriteString(p.Name + " " + status + ": " + clip(p.Text, 500) + "\n\n")
				}
			}
		}
	}
	return s.String()
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
