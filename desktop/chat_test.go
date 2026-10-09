package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/tool"
)

type echoTool struct{}

func (echoTool) Name() string            { return "echo" }
func (echoTool) Description() string     { return "echo" }
func (echoTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (echoTool) Execute(_ context.Context, a json.RawMessage) tool.Result {
	return tool.Result{Text: "echoed " + string(a)}
}

// scripted plays replies in order; gate, when set, blocks call n until closed.
type scripted struct {
	replies []agent.Message
	n       int
	gate    map[int]chan struct{}
	entered chan int // receives the number of each call as it starts
	seen    [][]agent.Message
}

func (s *scripted) Call(_ context.Context, _ string, msgs []agent.Message, _ []tool.Tool, emit func(agent.Event)) agent.Message {
	s.n++
	s.seen = append(s.seen, append([]agent.Message(nil), msgs...))
	if s.entered != nil {
		s.entered <- s.n
	}
	for _, p := range s.replies[s.n-1].Parts {
		if p.Kind == agent.PartThinking {
			emit(agent.Event{Kind: agent.EventThinkingDelta, Delta: p.Text})
		}
	}
	if g := s.gate[s.n]; g != nil {
		<-g
	}
	r := s.replies[s.n-1]
	if text := messageText(r); text != "" {
		emit(agent.Event{Kind: agent.EventTextDelta, Delta: text})
	}
	return r
}

func reply(stop agent.StopReason, parts ...agent.Part) agent.Message {
	return agent.Message{Role: agent.RoleAssistant, StopReason: stop, Parts: parts}
}

// harness runs posted functions on the test goroutine, as the window would.
type harness struct {
	t     *testing.T
	posts chan func()
	chat  *chat
}

func newHarness(t *testing.T, m agent.Model) *harness {
	h := &harness{t: t, posts: make(chan func(), 256)}
	h.chat = newChat(m, "sys", []tool.Tool{echoTool{}}, func(fn func()) { h.posts <- fn })
	return h
}

// pump runs posted functions until cond holds.
func (h *harness) pump(cond func() bool) {
	h.t.Helper()
	deadline := time.After(5 * time.Second)
	for !cond() {
		select {
		case fn := <-h.posts:
			fn()
		case <-deadline:
			h.t.Fatalf("timed out; rows %+v", h.chat.rows)
		}
	}
}

func (h *harness) idle() func() bool { return func() bool { return !h.chat.running } }

func TestRunBuildsRows(t *testing.T) {
	call := agent.Part{Kind: agent.PartToolCall, CallID: "c1", Name: "echo", Args: json.RawMessage(`{"x":1}`)}
	m := &scripted{replies: []agent.Message{
		reply(agent.StopToolUse, agent.Part{Kind: agent.PartThinking, Text: "hm"}, call),
		reply(agent.StopEnd, agent.Part{Kind: agent.PartText, Text: "done"}),
	}}
	h := newHarness(t, m)
	h.chat.draft = "  hi  "
	h.chat.send()
	h.pump(h.idle())

	c := h.chat
	if len(c.rows) != 4 {
		t.Fatalf("rows %+v", c.rows)
	}
	if c.rows[0].kind != rowUser || c.rows[0].text != "hi" {
		t.Errorf("user row %+v", c.rows[0])
	}
	if r := c.rows[1]; r.kind != rowAssistant || r.thinking != "hm" {
		t.Errorf("thinking-only reply keeps its row: %+v", r)
	}
	if r := c.rows[2]; r.kind != rowTool || r.result == nil || r.result.Text != `echoed {"x":1}` || r.failed {
		t.Errorf("tool row %+v", r)
	}
	if r := c.rows[3]; r.text != "done" || r.failed {
		t.Errorf("final row %+v", r)
	}
	if len(c.msgs) != 4 || c.msgs[2].Role != agent.RoleTool {
		t.Errorf("transcript has %d messages", len(c.msgs))
	}
	if c.draft != "" {
		t.Errorf("draft %q", c.draft)
	}
}

func TestToolOnlyReplyLeavesNoEmptyRow(t *testing.T) {
	call := agent.Part{Kind: agent.PartToolCall, CallID: "c1", Name: "echo", Args: json.RawMessage(`{}`)}
	m := &scripted{replies: []agent.Message{reply(agent.StopToolUse, call), reply(agent.StopEnd, agent.Part{Kind: agent.PartText, Text: "ok"})}}
	h := newHarness(t, m)
	h.chat.draft = "go"
	h.chat.send()
	h.pump(h.idle())
	if kinds := []rowKind{h.chat.rows[0].kind, h.chat.rows[1].kind, h.chat.rows[2].kind}; len(h.chat.rows) != 3 || kinds[1] != rowTool || kinds[2] != rowAssistant {
		t.Fatalf("rows %+v", h.chat.rows)
	}
}

func TestSteeringWhileRunning(t *testing.T) {
	call := agent.Part{Kind: agent.PartToolCall, CallID: "c1", Name: "echo", Args: json.RawMessage(`{}`)}
	gate := make(chan struct{})
	m := &scripted{
		gate:    map[int]chan struct{}{1: gate},
		entered: make(chan int, 4),
		replies: []agent.Message{
			reply(agent.StopToolUse, call),
			reply(agent.StopEnd, agent.Part{Kind: agent.PartText, Text: "changed course"}),
		},
	}
	h := newHarness(t, m)
	h.chat.draft = "start"
	h.chat.send()
	<-m.entered

	h.chat.draft = "actually, stop that"
	h.chat.send()
	if h.chat.queued != 1 || h.chat.steer.Len() != 1 {
		t.Fatalf("queued %d, queue %d", h.chat.queued, h.chat.steer.Len())
	}
	close(gate)
	h.pump(h.idle())

	last := m.seen[1]
	if got := messageText(last[len(last)-1]); got != "actually, stop that" || last[len(last)-2].Role != agent.RoleTool {
		t.Fatalf("second call saw %+v", last)
	}
	if h.chat.queued != 0 {
		t.Errorf("queued %d", h.chat.queued)
	}
	var users int
	for _, r := range h.chat.rows {
		if r.kind == rowUser {
			users++
		}
	}
	if users != 2 {
		t.Errorf("rows %+v", h.chat.rows)
	}
}

func TestStopMarksTheReply(t *testing.T) {
	gate := make(chan struct{})
	m := &scripted{gate: map[int]chan struct{}{1: gate}, entered: make(chan int, 4), replies: []agent.Message{reply(agent.StopAborted)}}
	h := newHarness(t, m)
	h.chat.draft = "go"
	h.chat.send()
	<-m.entered
	h.chat.stop()
	close(gate)
	h.pump(h.idle())
	if r := h.chat.rows[1]; !r.stopped || r.failed {
		t.Fatalf("row %+v", r)
	}
}

func TestViewTypeAndSend(t *testing.T) {
	m := &scripted{replies: []agent.Message{reply(agent.StopEnd, agent.Part{Kind: agent.PartText, Text: "hello there"})}}
	h := newHarness(t, m)
	tt := ui.NewTester(h.chat.view, 760, 640)

	if !tt.HasText("Ask for something.") || !tt.HasText("Send") {
		t.Fatalf("empty view: %q", tt.Texts())
	}
	tt.Type("hi bot")
	tt.Key(0, ui.KeyEnter)
	h.pump(h.idle())
	tt.Frame()
	if !tt.HasText("hi bot") || !tt.HasText("hello there") {
		t.Fatalf("after reply: %q", tt.Texts())
	}
}
