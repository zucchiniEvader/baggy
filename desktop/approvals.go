package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/host"
	"github.com/zucchiniEvader/baggy/store"
)

// Commands modes, kept in pref.commands; the host reads them before an
// action (host.Mode).
const (
	ModeRun    = host.ModeRun
	ModeAsk    = host.ModeAsk
	ModeReview = host.ModeReview
)

// approvals is the window's side of the host's approvals: the actions
// waiting, which the host holds, and Always Allow's rules.
type approvals struct{ a *app }

// waiting is an action on hold.
type waiting = host.Waiting

// rules are Always Allow's rules.
func (p *approvals) rules() []string { return p.a.rt.Rules() }

func (p *approvals) removeRule(rule string) {
	var rs []string
	for _, r := range p.rules() {
		if r != rule {
			rs = append(rs, r)
		}
	}
	p.a.st.SetSetting(host.SettingRules, strings.Join(rs, "\n"))
}

// describe is an action as a card reads it.
func describe(call agent.Part) string { return host.Describe(call) }

// decide answers a waiting action; always also allows it from now on.
func (p *approvals) decide(id string, ok, always bool) { p.a.rt.Decide(id, ok, always) }

// in lists a chat's waiting actions, oldest first.
func (p *approvals) in(chatID string) []waiting { return p.a.rt.Waiting(chatID) }

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
			a.approvals.decide(callID, true, false)
		}
		if smallButton(c.Key("always-"+callID), L("Always Allow"), secondary).Tooltip(L("Allow this exact command (or this plugin tool) from now on")).Clicked() {
			a.approvals.decide(callID, true, true)
		}
		if smallButton(c.Key("deny-"+callID), L("Deny"), secondary).Clicked() {
			a.approvals.decide(callID, false, false)
		}
	})
}

// approvalBar is the chat's waiting actions that have no card of their own,
// as a script's calls, above the composer.
func (a *app) approvalBar(c *ui.Context, ch store.Chat) {
	var ws []waiting
	v := a.views[ch.ID]
	for _, w := range a.approvals.in(ch.ID) {
		carded := false
		if v != nil {
			for _, r := range v.rows {
				carded = carded || (r.kind == rowTool && r.call.CallID == w.ID)
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
				ui.Column(c.Key("wait-" + w.ID)).Gap(6).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, iconShield).FontSize(iconSize).TextColor(p.orange).Shrink(0)
						ui.Text(c, L("%s wants to run %s", a.name(w.Bot), describe(w.Call))).FontSize(13).Grow(1).MinWidth(0).Selectable()
					})
					if w.Reason != "" {
						ui.Text(c, L("Auto-review: %s", w.Reason)).FontSize(12).TextColor(t.TextMuted).Padding(0, 0, 0, 22)
					}
					ui.Row(c).Padding(0, 0, 0, 22).Children(func() { a.approvalButtons(c, ch.ID, w.ID) })
				})
			}
		})
	})
}
