package main

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/tool"
)

// Declined is the result a bot reads for a command the user did not allow.
const Declined = "The user declined to run this command."

// approvals holds the shell commands waiting for the user. While on, every
// shell call waits for Allow or Deny on its card; Stop denies them.
type approvals struct {
	a  *app
	on atomic.Bool

	mu      sync.Mutex
	waiting map[string]chan bool // call id to its answer
	chatOf  map[string]string
}

func newApprovals(a *app) *approvals {
	return &approvals{a: a, waiting: map[string]chan bool{}, chatOf: map[string]string{}}
}

// approve is team.Team's Approve hook; it runs on a turn's goroutine.
func (p *approvals) approve(ctx context.Context, b store.Bot, chat store.Chat, call agent.Part) *tool.Result {
	if call.Name != "shell" || !p.on.Load() {
		return nil
	}
	answer := make(chan bool, 1)
	p.mu.Lock()
	p.waiting[call.CallID] = answer
	p.chatOf[call.CallID] = chat.ID
	p.mu.Unlock()
	p.a.post(func() { p.a.markPending(chat.ID, call.CallID, true) })
	defer func() {
		p.mu.Lock()
		delete(p.waiting, call.CallID)
		delete(p.chatOf, call.CallID)
		p.mu.Unlock()
		p.a.post(func() { p.a.markPending(chat.ID, call.CallID, false) })
	}()
	select {
	case ok := <-answer:
		if ok {
			return nil
		}
	case <-ctx.Done():
	}
	return &tool.Result{Text: Declined, IsError: true}
}

// decide answers a waiting call.
func (p *approvals) decide(callID string, ok bool) {
	p.mu.Lock()
	ch := p.waiting[callID]
	p.mu.Unlock()
	if ch != nil {
		select {
		case ch <- ok:
		default:
		}
	}
}

// denyAll denies what a chat has waiting.
func (p *approvals) denyAll(chatID string) {
	p.mu.Lock()
	var ids []string
	for id, c := range p.chatOf {
		if c == chatID {
			ids = append(ids, id)
		}
	}
	p.mu.Unlock()
	for _, id := range ids {
		p.decide(id, false)
	}
}

func (a *app) markPending(chatID, callID string, on bool) {
	v := a.views[chatID]
	if v == nil {
		return
	}
	for i := len(v.rows) - 1; i >= 0; i-- {
		if r := &v.rows[i]; r.kind == rowTool && r.call.CallID == callID {
			r.pending = on && r.result == nil
			return
		}
	}
}

func (a *app) approvalButtons(c *ui.Context, chatID, callID string) {
	ui.Row(c).Gap(8).Children(func() {
		if ui.PrimaryButton(c.Key("allow-"+callID), L("Allow")).Clicked() {
			a.approvals.decide(callID, true)
		}
		if ui.Button(c.Key("deny-"+callID), L("Deny")).Clicked() {
			a.approvals.decide(callID, false)
		}
	})
}
