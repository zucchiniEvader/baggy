package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/spike/gobot/tool"
)

type echo struct{}

func (echo) Name() string            { return "echo" }
func (echo) Description() string     { return "echo" }
func (echo) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (echo) Execute(_ context.Context, a json.RawMessage) tool.Result {
	return tool.Result{Text: string(a)}
}

// scripted replies with a tool call first, then a final answer.
type scripted struct{ n int }

func (s *scripted) Call(_ context.Context, _ string, msgs []Message, _ []tool.Tool, _ func(Event)) Message {
	s.n++
	if s.n == 1 {
		return Message{Role: RoleAssistant, StopReason: StopToolUse, Parts: []Part{
			{Kind: PartToolCall, CallID: "1", Name: "echo", Args: json.RawMessage(`{"x":1}`)},
		}}
	}
	return Message{Role: RoleAssistant, StopReason: StopEnd, Parts: []Part{{Kind: PartText, Text: "done"}}}
}

func TestRunToolRoundTrip(t *testing.T) {
	out := Run(context.Background(), &scripted{}, []Message{{Role: RoleUser}}, Options{Tools: []tool.Tool{echo{}}})
	if len(out) != 3 || out[1].Role != RoleTool || out[1].Parts[0].Text != `{"x":1}` {
		t.Fatalf("unexpected transcript: %+v", out)
	}
}
