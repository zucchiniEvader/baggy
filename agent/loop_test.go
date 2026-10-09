package agent

import (
	"context"
	"encoding/json"
	"strings"
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

type strict struct{ got json.RawMessage }

func (strict) Name() string        { return "strict" }
func (strict) Description() string { return "needs n" }
func (strict) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`)
}
func (s *strict) Execute(_ context.Context, a json.RawMessage) tool.Result {
	s.got = a
	return tool.Result{Text: "ran"}
}

func TestExecuteValidatesArguments(t *testing.T) {
	s := &strict{}
	tools := map[string]tool.Tool{"strict": s}

	r := execute(context.Background(), tools, Hooks{}, Part{Name: "strict", Args: json.RawMessage(`{}`)})
	if !r.IsError || s.got != nil || !strings.Contains(r.Text, "n: is required") {
		t.Fatalf("missing argument ran the tool: %+v", r)
	}

	r = execute(context.Background(), tools, Hooks{}, Part{Name: "strict", Args: json.RawMessage(`{"n":"7"}`)})
	if r.IsError || string(s.got) != `{"n":7}` {
		t.Fatalf("coerced call: %+v, args %s", r, s.got)
	}
}
