package provider

import (
	"context"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/provider/catalog"
	"github.com/spike/gobot/tool"
)

// Model adapts a Provider to agent.Model, the loop's only dependency.
type Model struct {
	Provider  Provider
	MaxTokens int // 0: the adapter's default
	Thinking  ThinkingLevel
}

// ModelID is the model the provider runs.
func (m Model) ModelID() string { return m.Provider.Model() }

// Call sends the transcript and forwards text and thinking deltas to emit.
// The loop sends the same prefix again on its next call, so the whole
// transcript is a cache point. A stream that ends without a done event is an
// error (an abort when ctx is cancelled), like any other failed call.
func (m Model) Call(ctx context.Context, system string, msgs []agent.Message, tools []tool.Tool, emit func(agent.Event)) agent.Message {
	req := Request{
		System:      system,
		Messages:    msgs,
		CachePoints: []int{len(msgs)},
		MaxTokens:   m.MaxTokens,
		Thinking:    m.Thinking,
	}
	for _, t := range tools {
		req.Tools = append(req.Tools, ToolSpec{Name: t.Name(), Description: t.Description(), Parameters: t.Schema()})
	}

	for e := range m.Provider.Stream(ctx, req) {
		switch e.Kind {
		case EventTextDelta:
			emit(agent.Event{Kind: agent.EventTextDelta, Delta: e.Delta})
		case EventThinkingDelta:
			emit(agent.Event{Kind: agent.EventThinkingDelta, Delta: e.Delta})
		case EventDone:
			reply := *e.Message
			if info, ok := catalog.Find(m.Provider.Model()); ok && reply.Usage.Cost == 0 {
				reply.Usage.Cost = info.Cost(reply.Usage)
			}
			return reply
		}
	}
	reply := agent.Message{Role: agent.RoleAssistant, Provider: m.Provider.ID(), Model: m.Provider.Model()}
	if ctx.Err() != nil {
		reply.StopReason = agent.StopAborted
		return reply
	}
	reply.StopReason = agent.StopError
	reply.Parts = []agent.Part{{Kind: agent.PartText, Text: "provider stream ended without a result", IsError: true}}
	return reply
}
