package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/tool"
)

type fakeTool struct{}

func (fakeTool) Name() string            { return "echo" }
func (fakeTool) Description() string     { return "echoes" }
func (fakeTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (fakeTool) Execute(_ context.Context, a json.RawMessage) tool.Result {
	return tool.Result{Text: string(a)}
}

// fakeProvider plays one scripted event list per Stream call and records requests.
type fakeProvider struct {
	scripts [][]Event
	reqs    []Request
}

func (f *fakeProvider) ID() string    { return "fake" }
func (f *fakeProvider) Model() string { return "m" }
func (f *fakeProvider) Stream(_ context.Context, req Request) <-chan Event {
	f.reqs = append(f.reqs, req)
	evs := f.scripts[len(f.reqs)-1]
	ch := make(chan Event, len(evs))
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	return ch
}

func TestModelDrivesLoop(t *testing.T) {
	call := agent.Part{Kind: agent.PartToolCall, CallID: "c1", Name: "echo", Args: json.RawMessage(`{"x":1}`)}
	f := &fakeProvider{scripts: [][]Event{
		{
			{Kind: EventStart},
			{Kind: EventThinkingDelta, Delta: "hm"},
			{Kind: EventToolCall, Part: &call},
			{Kind: EventDone, Message: &agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopToolUse, Parts: []agent.Part{call}}},
		},
		{
			{Kind: EventTextDelta, Delta: "do"},
			{Kind: EventTextDelta, Delta: "ne"},
			{Kind: EventDone, Message: &agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopEnd, Parts: []agent.Part{{Kind: agent.PartText, Text: "done"}}}},
		},
	}}

	var deltas []string
	user := agent.Message{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: "hi"}}}
	out := agent.Run(context.Background(), Model{Provider: f, MaxTokens: 99, Thinking: ThinkingLow}, []agent.Message{user}, agent.Options{
		System: "sys",
		Tools:  []tool.Tool{fakeTool{}},
		Emit: func(e agent.Event) {
			if e.Kind == "text_delta" || e.Kind == "thinking_delta" {
				deltas = append(deltas, string(e.Kind)+":"+e.Delta)
			}
		},
	})

	if len(out) != 3 || out[1].Parts[0].Text != `{"x":1}` || out[2].Parts[0].Text != "done" {
		t.Fatalf("unexpected transcript: %+v", out)
	}
	if got := len(deltas); got != 3 || deltas[0] != "thinking_delta:hm" || deltas[2] != "text_delta:ne" {
		t.Fatalf("deltas: %v", deltas)
	}
	r := f.reqs[1]
	if r.System != "sys" || r.MaxTokens != 99 || r.Thinking != ThinkingLow || len(r.Messages) != 3 ||
		len(r.Tools) != 1 || r.Tools[0].Name != "echo" || len(r.CachePoints) != 1 || r.CachePoints[0] != 3 {
		t.Fatalf("second request: %+v", r)
	}
}

func TestModelStreamWithoutDone(t *testing.T) {
	f := &fakeProvider{scripts: [][]Event{{{Kind: EventStart}}}}
	got := Model{Provider: f}.Call(context.Background(), "", nil, nil, func(agent.Event) {})
	if got.StopReason != agent.StopError || got.Provider != "fake" {
		t.Fatalf("got %+v", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f = &fakeProvider{scripts: [][]Event{{}}}
	if got := (Model{Provider: f}).Call(ctx, "", nil, nil, func(agent.Event) {}); got.StopReason != agent.StopAborted {
		t.Fatalf("got %+v", got)
	}
}
