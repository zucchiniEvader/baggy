package main

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/tool"
)

type rowKind int

const (
	rowUser rowKind = iota
	rowAssistant
	rowTool
	rowNotice
)

// row is one line of the transcript as drawn. Rows come from agent events.
type row struct {
	kind     rowKind
	text     string
	thinking string
	failed   bool // an assistant row that ended in an error, or a tool that failed
	stopped  bool // an assistant row cut off by Stop
	call     agent.Part
	result   *tool.Result
}

// chat is the window's state. Everything here belongs to the main thread:
// the run goroutine reaches it only through post.
type chat struct {
	model  agent.Model
	system string
	tools  []tool.Tool
	// post runs fn on the main thread and draws a frame; a window's Update.
	post func(fn func())

	msgs   []agent.Message // the transcript the next run starts from
	rows   []row
	cur    int            // index in rows of the reply streaming, or -1
	toolAt map[string]int // call id to its row

	running bool
	cancel  context.CancelFunc
	steer   *agent.Queue // messages typed while a run is going
	queued  int          // of those, not yet taken by the run

	draft string
	list  ui.ListState
	focus bool
}

func newChat(model agent.Model, system string, tools []tool.Tool, post func(func())) *chat {
	c := &chat{model: model, system: system, tools: tools, post: post, cur: -1,
		toolAt: map[string]int{}, steer: agent.NewQueue(agent.QueueAll), focus: true}
	c.list.FollowEnd = true
	return c
}

func userMessage(text string) agent.Message {
	return agent.Message{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: text}}}
}

func messageText(m agent.Message) string {
	var b strings.Builder
	for _, p := range m.Parts {
		if p.Kind == agent.PartText {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// notice adds a line that is not part of the transcript.
func (c *chat) notice(text string) { c.rows = append(c.rows, row{kind: rowNotice, text: text}) }

// apply folds one agent event into the rows.
func (c *chat) apply(e agent.Event) {
	switch e.Kind {
	case agent.EventMessageStart:
		if e.Message == nil { // a model reply is starting
			c.rows = append(c.rows, row{kind: rowAssistant})
			c.cur = len(c.rows) - 1
		}
	case agent.EventTextDelta:
		if c.cur >= 0 {
			c.rows[c.cur].text += e.Delta
		}
	case agent.EventThinkingDelta:
		if c.cur >= 0 {
			c.rows[c.cur].thinking += e.Delta
		}
	case agent.EventToolStart:
		c.toolAt[e.Call.CallID] = len(c.rows)
		c.rows = append(c.rows, row{kind: rowTool, call: *e.Call})
	case agent.EventToolEnd:
		if i, ok := c.toolAt[e.Call.CallID]; ok {
			r := *e.Result
			c.rows[i].result, c.rows[i].failed = &r, r.IsError
			delete(c.toolAt, e.Call.CallID)
		}
	case agent.EventMessageEnd:
		m := *e.Message
		c.msgs = append(c.msgs, m)
		switch m.Role {
		case agent.RoleUser: // an injected steering message
			c.rows = append(c.rows, row{kind: rowUser, text: messageText(m)})
			c.queued = max(c.queued-1, 0)
		case agent.RoleAssistant:
			c.endReply(m)
		}
	}
}

func (c *chat) endReply(m agent.Message) {
	if c.cur < 0 {
		return
	}
	r := &c.rows[c.cur]
	if text := messageText(m); text != "" {
		r.text = text
	}
	switch m.StopReason {
	case agent.StopError:
		r.failed = true
	case agent.StopAborted:
		r.stopped = true
	}
	if r.text == "" && r.thinking == "" && !r.stopped && !r.failed { // only tool calls
		c.rows = append(c.rows[:c.cur], c.rows[c.cur+1:]...)
		for id, i := range c.toolAt { // later rows moved up
			if i > c.cur {
				c.toolAt[id] = i - 1
			}
		}
	}
	c.cur = -1
}

// send handles the draft: a prompt when idle, a steering message while a run
// is going.
func (c *chat) send() {
	text := strings.TrimSpace(c.draft)
	if text == "" {
		return
	}
	c.draft = ""
	c.focus = true
	if c.running {
		c.steer.Push(userMessage(text))
		c.queued++
		return
	}
	m := userMessage(text)
	c.msgs = append(c.msgs, m)
	c.rows = append(c.rows, row{kind: rowUser, text: text})
	c.start()
}

// start runs the agent over the transcript in a goroutine.
func (c *chat) start() {
	ctx, cancel := context.WithCancel(context.Background())
	c.running, c.cancel = true, cancel
	msgs := append([]agent.Message(nil), c.msgs...)
	go func() {
		agent.Run(ctx, c.model, msgs, agent.Options{
			System:   c.system,
			Tools:    c.tools,
			Steering: c.steer,
			Emit:     func(e agent.Event) { c.post(func() { c.apply(e) }) },
		})
		c.post(c.finish)
	}()
}

// finish runs after a run ends. Steering that arrived too late for the run
// starts the next one.
func (c *chat) finish() {
	c.running = false
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.cur = -1
	if next := c.steer.Drain(); len(next) > 0 {
		for _, m := range next {
			c.msgs = append(c.msgs, m)
			c.rows = append(c.rows, row{kind: rowUser, text: messageText(m)})
			c.queued = max(c.queued-1, 0)
		}
		c.start()
	}
}

// stop cancels the run and drops what is queued.
func (c *chat) stop() {
	if c.cancel != nil {
		c.cancel()
	}
	c.steer.Clear()
	c.queued = 0
}
