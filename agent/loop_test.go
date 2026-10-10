package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zucchiniEvader/baggy/tool"
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

// seen records the transcript each model call received and replays replies.
type seen struct {
	replies []Message
	calls   [][]Message
	onCall  func(n int) // runs inside call n (1-based), before the reply
}

func (s *seen) Call(_ context.Context, _ string, msgs []Message, _ []tool.Tool, _ func(Event)) Message {
	s.calls = append(s.calls, append([]Message(nil), msgs...))
	if s.onCall != nil {
		s.onCall(len(s.calls))
	}
	return s.replies[len(s.calls)-1]
}

func say(s string) Message {
	return Message{Role: RoleAssistant, StopReason: StopEnd, Parts: []Part{{Kind: PartText, Text: s}}}
}
func ask(id string) Message {
	return Message{Role: RoleAssistant, StopReason: StopToolUse, Parts: []Part{{Kind: PartToolCall, CallID: id, Name: "echo", Args: json.RawMessage(`{}`)}}}
}
func userText(s string) Message {
	return Message{Role: RoleUser, Parts: []Part{{Kind: PartText, Text: s}}}
}

func lastText(ms []Message) string { return ms[len(ms)-1].Parts[0].Text }

func TestEventsCarryCallsAndPersistEveryMessage(t *testing.T) {
	var kinds []EventKind
	var ended []Role
	var toolEnd Event
	m := &seen{replies: []Message{ask("c1"), say("done")}}
	Run(context.Background(), m, []Message{userText("go")}, Options{
		Tools: []tool.Tool{echo{}},
		Emit: func(e Event) {
			kinds = append(kinds, e.Kind)
			if e.Kind == EventMessageEnd {
				ended = append(ended, e.Message.Role)
			}
			if e.Kind == EventToolEnd {
				toolEnd = e
			}
		},
	})
	if want := []Role{RoleAssistant, RoleTool, RoleAssistant}; !reflect.DeepEqual(ended, want) {
		t.Errorf("message_end roles %v, want %v", ended, want)
	}
	if toolEnd.Call == nil || toolEnd.Call.Name != "echo" || toolEnd.Call.CallID != "c1" || toolEnd.Result == nil || toolEnd.Result.Text != `{}` {
		t.Errorf("tool_end: %+v", toolEnd)
	}
	if kinds[0] != EventTurnStart || kinds[1] != EventMessageStart || kinds[len(kinds)-1] != EventTurnEnd {
		t.Errorf("kinds %v", kinds)
	}
}

func TestSteeringReachesTheNextModelCall(t *testing.T) {
	steer := NewQueue(QueueAll)
	m := &seen{replies: []Message{ask("c1"), say("ok")}}
	m.onCall = func(n int) {
		if n == 1 {
			steer.Push(userText("change direction")) // arrives while the model is replying
		}
	}
	out := Run(context.Background(), m, []Message{userText("go")}, Options{Tools: []tool.Tool{echo{}}, Steering: steer})

	if len(m.calls) != 2 || lastText(m.calls[1]) != "change direction" || m.calls[1][len(m.calls[1])-2].Role != RoleTool {
		t.Fatalf("second call saw %+v", m.calls[1])
	}
	if len(out) != 4 || out[2].Role != RoleUser {
		t.Fatalf("out %+v", out)
	}
}

func TestQueuedBeforeTheRunIsTakenFirst(t *testing.T) {
	steer := NewQueue(QueueAll)
	steer.Push(userText("early"))
	m := &seen{replies: []Message{say("ok")}}
	Run(context.Background(), m, []Message{userText("go")}, Options{Steering: steer})
	if lastText(m.calls[0]) != "early" || steer.Len() != 0 {
		t.Fatalf("first call saw %+v", m.calls[0])
	}
}

func TestFollowUpRunsWhenTheModelWouldStop(t *testing.T) {
	steer, follow := NewQueue(QueueOneAtATime), NewQueue(QueueOneAtATime)
	follow.Push(userText("f1"), userText("f2"))
	m := &seen{replies: []Message{say("a"), say("b"), say("c"), say("d")}}
	m.onCall = func(n int) {
		if n == 2 {
			steer.Push(userText("s1")) // beats the remaining follow-up
		}
	}
	out := Run(context.Background(), m, []Message{userText("go")}, Options{Steering: steer, FollowUp: follow})

	var texts []string
	for _, msg := range out {
		texts = append(texts, msg.Parts[0].Text)
	}
	if want := []string{"a", "f1", "b", "s1", "c", "f2", "d"}; !reflect.DeepEqual(texts, want) {
		t.Fatalf("texts %v, want %v", texts, want)
	}
	if follow.Len() != 0 || steer.Len() != 0 {
		t.Fatalf("queues not drained: %d %d", follow.Len(), steer.Len())
	}
}

func TestErrorEndsTheRunWithoutReadingQueues(t *testing.T) {
	follow := NewQueue(QueueAll)
	follow.Push(userText("later"))
	m := &seen{replies: []Message{{Role: RoleAssistant, StopReason: StopError}}}
	out := Run(context.Background(), m, []Message{userText("go")}, Options{FollowUp: follow})
	if len(out) != 1 || follow.Len() != 1 {
		t.Fatalf("out %d, queued %d", len(out), follow.Len())
	}
}

func TestParallelToolEventsAreSerialized(t *testing.T) {
	reply := Message{Role: RoleAssistant, StopReason: StopToolUse}
	for i := 0; i < 8; i++ {
		reply.Parts = append(reply.Parts, Part{Kind: PartToolCall, CallID: string(rune('a' + i)), Name: "echo", Args: json.RawMessage(`{}`)})
	}
	m := &seen{replies: []Message{reply, say("ok")}}
	n := 0 // unsynchronized on purpose: -race flags concurrent Emit calls
	Run(context.Background(), m, []Message{userText("go")}, Options{Tools: []tool.Tool{echo{}}, Emit: func(Event) { n++ }})
	if n == 0 {
		t.Fatal("no events")
	}
}

func TestCompactReplacesWhatCallsSend(t *testing.T) {
	var sent [][]Message
	m := modelFunc(func(msgs []Message) Message {
		sent = append(sent, msgs)
		if len(sent) == 1 {
			return Message{Role: RoleAssistant, StopReason: StopToolUse, Parts: []Part{{Kind: PartToolCall, CallID: "c", Name: "echo", Args: []byte(`{}`)}}}
		}
		return Message{Role: RoleAssistant, StopReason: StopEnd, Parts: []Part{{Kind: PartText, Text: "done"}}}
	})
	summary := Message{Role: RoleUser, Kind: "compaction", Parts: []Part{{Kind: PartText, Text: "summary"}}}
	calls := 0
	out := Run(context.Background(), m, []Message{userText("a"), userText("b")}, Options{
		Tools: []tool.Tool{echo{}},
		Compact: func(_ context.Context, msgs []Message, _ bool) ([]Message, bool) {
			calls++
			if calls == 2 { // between the calls: keep only the summary and the tool exchange
				return append([]Message{summary}, msgs[2:]...), true
			}
			return nil, false
		},
	})
	if len(sent) != 2 || len(sent[1]) != 3 || sent[1][0].Kind != "compaction" || sent[1][1].Role != RoleAssistant || sent[1][2].Role != RoleTool {
		t.Fatalf("second call sent %+v", sent[1])
	}
	if len(out) != 3 { // the reply, the result, the last reply: the summary is not the run's
		t.Fatalf("out %d", len(out))
	}
}

func TestEstimateTokensUsesLastUsage(t *testing.T) {
	msgs := []Message{
		userText("hello there"),
		{Role: RoleAssistant, Usage: Usage{Input: 100, Output: 20}, Parts: []Part{{Kind: PartText, Text: "hi"}}},
		userText("0123456789012345678901234567890123456789"),
	}
	if got := EstimateTokens(msgs); got != 120+4+10 {
		t.Fatalf("estimate %d", got)
	}
}

type modelFunc func(msgs []Message) Message

func (f modelFunc) Call(_ context.Context, _ string, msgs []Message, _ []tool.Tool, _ func(Event)) Message {
	return f(append([]Message(nil), msgs...))
}

func TestInterruptCutsTheReplyAndReadsSteering(t *testing.T) {
	steer, intr := NewQueue(QueueAll), NewInterrupt()
	entered := make(chan struct{})
	var second []Message
	n := 0
	m := ctxModel(func(ctx context.Context, msgs []Message) Message {
		n++
		if n == 1 {
			close(entered)
			<-ctx.Done()
			return Message{Role: RoleAssistant, StopReason: StopAborted, Parts: []Part{{Kind: PartThinking, Text: "hm"}, {Kind: PartText, Text: "partial"}}}
		}
		second = msgs
		return Message{Role: RoleAssistant, StopReason: StopEnd, Parts: []Part{{Kind: PartText, Text: "ok"}}}
	})
	go func() {
		<-entered
		steer.Push(userText("actually, stop that"))
		if !intr.Interrupt() {
			t.Error("no step to interrupt")
		}
	}()
	out := Run(context.Background(), m, []Message{userText("go")}, Options{Steering: steer, Interrupt: intr})
	if len(second) != 3 || second[1].StopReason != StopEnd || len(second[1].Parts) != 1 || second[1].Parts[0].Text != "partial" || lastText(second) != "actually, stop that" {
		t.Fatalf("second call saw %+v", second)
	}
	if len(out) != 3 || lastText(out) != "ok" {
		t.Fatalf("out %+v", out)
	}
	if intr.Interrupt() {
		t.Fatal("an interrupt between runs reported a step")
	}
}

func TestInterruptCancelsRunningTools(t *testing.T) {
	intr := NewInterrupt()
	started := make(chan struct{})
	n := 0
	m := ctxModel(func(ctx context.Context, msgs []Message) Message {
		n++
		if n == 1 {
			return Message{Role: RoleAssistant, StopReason: StopToolUse, Parts: []Part{{Kind: PartToolCall, CallID: "c", Name: "wait", Args: []byte(`{}`)}}}
		}
		return Message{Role: RoleAssistant, StopReason: StopEnd, Parts: []Part{{Kind: PartText, Text: "after " + msgs[len(msgs)-1].Parts[0].Text}}}
	})
	go func() { <-started; intr.Interrupt() }()
	out := Run(context.Background(), m, []Message{userText("go")}, Options{Tools: []tool.Tool{waiter{started}}, Interrupt: intr})
	if lastText(out) != "after "+Interrupted {
		t.Fatalf("out %+v", out)
	}
}

type ctxModel func(ctx context.Context, msgs []Message) Message

func (f ctxModel) Call(ctx context.Context, _ string, msgs []Message, _ []tool.Tool, _ func(Event)) Message {
	return f(ctx, append([]Message(nil), msgs...))
}

type waiter struct{ started chan struct{} }

func (waiter) Name() string            { return "wait" }
func (waiter) Description() string     { return "waits" }
func (waiter) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (w waiter) Execute(ctx context.Context, _ json.RawMessage) tool.Result {
	close(w.started)
	<-ctx.Done()
	return tool.Result{}
}

func TestRetryTransientAndCompactOnOverflow(t *testing.T) {
	n := 0
	var retries []string
	fail := func(s string) Message {
		return Message{Role: RoleAssistant, StopReason: StopError, Parts: []Part{{Kind: PartText, Text: s, IsError: true}}}
	}
	m := modelFunc(func(msgs []Message) Message {
		n++
		switch n {
		case 1:
			return fail("529: Overloaded")
		case 2:
			return fail("400: prompt is too long: 210000 tokens > 200000 maximum")
		}
		return Message{Role: RoleAssistant, StopReason: StopEnd, Parts: []Part{{Kind: PartText, Text: lastText(msgs)}}}
	})
	forced := false
	out := Run(context.Background(), m, []Message{userText("a"), userText("b")}, Options{
		Retry: &Retry{Max: 2, Base: time.Millisecond, MaxDelay: time.Millisecond},
		Compact: func(_ context.Context, msgs []Message, force bool) ([]Message, bool) {
			if !force {
				return nil, false
			}
			forced = true
			return []Message{userText("summary"), msgs[len(msgs)-1]}, true
		},
		Emit: func(e Event) {
			if e.Kind == EventRetry {
				retries = append(retries, e.Delta)
			}
		},
	})
	if n != 3 || !forced || len(retries) != 2 || len(out) != 1 || lastText(out) != "b" {
		t.Fatalf("calls %d forced %v retries %q out %+v", n, forced, retries, out)
	}

	n = 0
	out = Run(context.Background(), modelFunc(func([]Message) Message { n++; return fail("insufficient_quota: billing") }), []Message{userText("a")},
		Options{Retry: &Retry{Max: 3, Base: time.Millisecond, MaxDelay: time.Millisecond}})
	if n != 1 || out[0].StopReason != StopError {
		t.Fatalf("quota retried: %d", n)
	}
}

func TestErrorClasses(t *testing.T) {
	for s, want := range map[string]bool{"529: Overloaded": true, "stream ended before message_stop": true, "unexpected EOF": true, "429: rate limit": true,
		"401: invalid x-api-key": false, "429: insufficient_quota": false, "prompt is too long": false} {
		if IsRetryable(s) != want {
			t.Errorf("IsRetryable(%q) = %v", s, !want)
		}
	}
	if !IsContextOverflow("This model's maximum context length is 128000 tokens") {
		t.Error("overflow not seen")
	}
}

func TestPausedReplyGoesBack(t *testing.T) {
	var seen [][]Message
	m := modelFunc(func(msgs []Message) Message {
		seen = append(seen, msgs)
		if len(seen) == 1 {
			return Message{Role: RoleAssistant, StopReason: StopPause, Parts: []Part{{Kind: PartServer, Raw: []byte(`{"type":"server_tool_use"}`)}}}
		}
		return Message{Role: RoleAssistant, StopReason: StopEnd, Parts: []Part{{Kind: PartText, Text: "found"}}}
	})
	out := Run(context.Background(), m, []Message{userText("search")}, Options{})
	if len(seen) != 2 || len(seen[1]) != 2 || seen[1][1].StopReason != StopPause || lastText(out) != "found" {
		t.Fatalf("seen %+v out %+v", seen, out)
	}
}
