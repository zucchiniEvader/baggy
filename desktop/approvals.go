package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/team"
	"github.com/zucchiniEvader/baggy/tool"
)

// Declined is the result a bot reads for an action the user did not allow.
const Declined = "The user declined to run this command."

// Commands modes: what happens before a shell command or a plugin call that
// does not only read.
const (
	ModeRun    = "run"    // it runs
	ModeAsk    = "ask"    // it waits for Allow or Deny
	ModeReview = "review" // the bot's model reviews it; only what it doubts waits
)

// approvals holds the actions waiting for the user; Stop denies them.
type approvals struct {
	a    *app
	mode atomic.Value // string

	mu      sync.Mutex
	waiting map[string]*waiting // by call id
}

// waiting is an action on hold.
type waiting struct {
	id, chat, bot string
	call          agent.Part
	reason        string // why a review asked
	since         time.Time
	answer        chan bool
}

func newApprovals(a *app) *approvals {
	p := &approvals{a: a, waiting: map[string]*waiting{}}
	p.mode.Store(ModeRun)
	return p
}

func (p *approvals) setMode(m string) { p.mode.Store(m) }

// gated reports whether an action goes through approval: a shell command,
// or a plugin tool its server does not mark read-only.
func (p *approvals) gated(call agent.Part) bool {
	if call.Name == "shell" {
		return true
	}
	if !strings.Contains(call.Name, "__") || p.a.team.Plugins == nil {
		return false
	}
	t, ok := p.a.team.Plugins.Lookup(call.Name)
	return !ok || !t.Annotations.ReadOnlyHint
}

// rule is what Always Allow saves for an action: the exact command, or the
// plugin tool.
func rule(call agent.Part) string {
	if call.Name == "shell" {
		return "shell:" + commandOf(call)
	}
	return "tool:" + call.Name
}

func (p *approvals) rules() []string {
	var out []string
	for _, r := range strings.Split(p.a.st.Setting("allow_rules"), "\n") {
		if r != "" {
			out = append(out, r)
		}
	}
	return out
}

func (p *approvals) allowed(call agent.Part) bool {
	want := rule(call)
	for _, r := range p.rules() {
		if r == want {
			return true
		}
	}
	return false
}

func (p *approvals) removeRule(rule string) {
	var rs []string
	for _, r := range p.rules() {
		if r != rule {
			rs = append(rs, r)
		}
	}
	p.a.st.SetSetting("allow_rules", strings.Join(rs, "\n"))
}

func (p *approvals) alwaysAllow(call agent.Part) {
	rs := append(p.rules(), rule(call))
	sort.Strings(rs)
	p.a.st.SetSetting("allow_rules", strings.Join(rs, "\n"))
}

// describe is an action as a review and a card read it.
func describe(call agent.Part) string {
	if call.Name == "shell" {
		return "the shell command: " + commandOf(call)
	}
	return fmt.Sprintf("the plugin tool %s with %s", call.Name, oneLine(string(call.Args), 600))
}

// approve is team.Team's Approve hook; it runs on a turn's goroutine.
func (p *approvals) approve(ctx context.Context, b store.Bot, chat store.Chat, call agent.Part) *tool.Result {
	mode, _ := p.mode.Load().(string)
	if mode == ModeRun || !p.gated(call) || p.allowed(call) {
		return nil
	}
	reason := ""
	if mode == ModeReview {
		if m, err := p.a.team.Model(b); err == nil {
			ok, why := team.Review(ctx, m, b, p.a.team.LastRequest(chat.ID), describe(call), p.a.st.WorkDir(b))
			if ok {
				return nil
			}
			reason = why
		}
	}
	w := &waiting{id: call.CallID, chat: chat.ID, bot: b.ID, call: call, reason: reason, since: time.Now(), answer: make(chan bool, 1)}
	p.mu.Lock()
	p.waiting[w.id] = w
	p.mu.Unlock()
	p.a.post(func() {
		p.a.markPending(chat.ID, call.CallID, true)
		if p.a.notify != nil && !p.a.watching(chat.ID) {
			p.a.notify(b.Name, L("wants to run a command"), oneLine(describe(call), 200), chat.ID)
		}
	})
	defer func() {
		p.mu.Lock()
		delete(p.waiting, w.id)
		p.mu.Unlock()
		p.a.post(func() { p.a.markPending(chat.ID, call.CallID, false) })
	}()
	select {
	case ok := <-w.answer:
		if ok {
			return nil
		}
	case <-ctx.Done():
	}
	return &tool.Result{Text: Declined, IsError: true}
}

// decide answers a waiting action.
func (p *approvals) decide(id string, ok bool) {
	p.mu.Lock()
	w := p.waiting[id]
	p.mu.Unlock()
	if w != nil {
		select {
		case w.answer <- ok:
		default:
		}
	}
}

// in lists a chat's waiting actions, oldest first.
func (p *approvals) in(chatID string) []*waiting {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []*waiting
	for _, w := range p.waiting {
		if w.chat == chatID {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].since.Before(out[j].since) })
	return out
}

// denyAll denies what a chat has waiting.
func (p *approvals) denyAll(chatID string) {
	for _, w := range p.in(chatID) {
		p.decide(w.id, false)
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

// approvalButtons are Allow, Always Allow and Deny for a waiting action.
func (a *app) approvalButtons(c *ui.Context, chatID, callID string) {
	ui.Row(c).Gap(8).Children(func() {
		if smallButton(c.Key("allow-"+callID), L("Allow"), primary).Clicked() {
			a.approvals.decide(callID, true)
		}
		if smallButton(c.Key("always-"+callID), L("Always Allow"), secondary).Tooltip(L("Allow this exact command (or this plugin tool) from now on")).Clicked() {
			a.approvals.mu.Lock()
			w := a.approvals.waiting[callID]
			a.approvals.mu.Unlock()
			if w != nil {
				a.approvals.alwaysAllow(w.call)
			}
			a.approvals.decide(callID, true)
		}
		if smallButton(c.Key("deny-"+callID), L("Deny"), secondary).Clicked() {
			a.approvals.decide(callID, false)
		}
	})
}

// approvalBar is the chat's waiting actions that have no card of their own,
// as a script's calls, above the composer.
func (a *app) approvalBar(c *ui.Context, ch store.Chat) {
	var ws []*waiting
	v := a.views[ch.ID]
	for _, w := range a.approvals.in(ch.ID) {
		carded := false
		if v != nil {
			for _, r := range v.rows {
				carded = carded || (r.kind == rowTool && r.call.CallID == w.id)
			}
		}
		if !carded {
			ws = append(ws, w)
		}
	}
	if len(ws) == 0 {
		return
	}
	t := c.Theme()
	p := pal(c)
	ui.Column(c).Shrink(0).MaxWidth(contentWidth).FillWidth().Margin(0, ui.Auto).Padding(0, 28, 8).Children(func() {
		ui.Column(c).Radius(cardRadius).Background(p.surface).Border(1, p.border).Padding(10, 12).Gap(10).Children(func() {
			for _, w := range ws {
				ui.Column(c.Key("wait-" + w.id)).Gap(6).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, iconShield).FontSize(iconSize).TextColor(p.orange).Shrink(0)
						ui.Text(c, L("%s wants to run %s", a.name(w.bot), describe(w.call))).FontSize(13).Grow(1).MinWidth(0).Selectable()
					})
					if w.reason != "" {
						ui.Text(c, L("Auto-review: %s", w.reason)).FontSize(12).TextColor(t.TextMuted).Padding(0, 0, 0, 22)
					}
					ui.Row(c).Padding(0, 0, 0, 22).Children(func() { a.approvalButtons(c, ch.ID, w.id) })
				})
			}
		})
	})
}

func commandOf(call agent.Part) string {
	var a struct{ Command string }
	json.Unmarshal(call.Args, &a)
	return oneLine(a.Command, 400)
}
