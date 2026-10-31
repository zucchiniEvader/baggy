package host

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/claudecode"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/team"
	"github.com/zucchiniEvader/baggy/tool"
)

// Declined is the result a bot reads for an action the user did not allow.
const Declined = "The user declined to run this command."

// Commands modes, the pref.commands setting: what happens before a shell
// command or a plugin call that does not only read.
const (
	ModeRun    = "run"    // it runs
	ModeAsk    = "ask"    // it waits for Allow or Deny
	ModeReview = "review" // the bot's model reviews it; only what it doubts waits
)

// SettingRules holds Always Allow's rules, one a line.
const SettingRules = "allow_rules"

type approvals struct {
	mu      sync.Mutex
	waiting map[string]*waiting // by call id
}

type waiting struct {
	Waiting
	answer chan bool
}

// Waiting is an action on hold.
type Waiting struct {
	ID     string     `json:"id"` // the call's
	Chat   string     `json:"chat"`
	Bot    string     `json:"bot"`
	Call   agent.Part `json:"call"`
	Reason string     `json:"reason,omitempty"` // why a review asked
	Since  time.Time  `json:"since"`
}

// Mode is the Commands mode the preferences hold.
func (h *Host) Mode() string {
	m := h.Store.Setting("pref.commands")
	if m == "" {
		m = ModeRun
		if h.Store.Setting("pref.approve_shell") == "1" { // the switch it replaces
			m = ModeAsk
		}
	}
	return m
}

// gated reports whether an action goes through approval: a shell command,
// Claude Code's, or a plugin tool its server does not mark read-only.
func (h *Host) gated(call agent.Part) bool {
	if call.Name == "shell" || strings.HasPrefix(call.Name, team.ClaudeCodeCall) {
		return true
	}
	if !strings.Contains(call.Name, "__") || h.Team.Plugins == nil {
		return false
	}
	t, ok := h.Team.Plugins.Lookup(call.Name)
	return !ok || !t.Annotations.ReadOnlyHint
}

// Rule is what Always Allow saves for an action: the exact command (of the
// bot or of Claude Code), or the tool.
func Rule(call agent.Part) string {
	if call.Name == "shell" {
		return "shell:" + CommandOf(call)
	}
	if call.Name == team.ClaudeCodeCall+"Bash" {
		return team.ClaudeCodeCall + "Bash:" + CommandOf(call)
	}
	return "tool:" + call.Name
}

// Rules are Always Allow's rules.
func (h *Host) Rules() []string {
	var out []string
	for _, r := range strings.Split(h.Store.Setting(SettingRules), "\n") {
		if r != "" {
			out = append(out, r)
		}
	}
	return out
}

func (h *Host) allowed(call agent.Part) bool {
	want := Rule(call)
	for _, r := range h.Rules() {
		if r == want {
			return true
		}
	}
	return false
}

func (h *Host) alwaysAllow(call agent.Part) {
	rs := append(h.Rules(), Rule(call))
	sort.Strings(rs)
	h.Store.SetSetting(SettingRules, strings.Join(rs, "\n"))
}

// Describe is an action as a review and a card read it.
func Describe(call agent.Part) string {
	if call.Name == "shell" {
		return "the shell command: " + CommandOf(call)
	}
	if tool, ok := strings.CutPrefix(call.Name, team.ClaudeCodeCall); ok {
		return "in Claude Code, " + claudecode.Describe(claudecode.Request{Tool: tool, Input: call.Args})
	}
	return fmt.Sprintf("the plugin tool %s with %s", call.Name, oneLine(string(call.Args), 600))
}

// CommandOf is a shell call's command on one line.
func CommandOf(call agent.Part) string {
	var a struct{ Command string }
	json.Unmarshal(call.Args, &a)
	return oneLine(a.Command, 400)
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// approve is team.Team's Approve hook; it runs on a turn's goroutine and
// waits for Decide while the action is held.
func (h *Host) approve(ctx context.Context, b store.Bot, chat store.Chat, call agent.Part) *tool.Result {
	mode := h.Mode()
	if mode == ModeRun || !h.gated(call) || h.allowed(call) {
		return nil
	}
	reason := ""
	if mode == ModeReview {
		if m, err := h.Team.Model(b); err == nil {
			ok, why := team.Review(ctx, m, b, h.Team.LastRequest(chat.ID), Describe(call), h.Store.WorkDir(b))
			if ok {
				return nil
			}
			reason = why
		}
	}
	w := &waiting{Waiting: Waiting{ID: call.CallID, Chat: chat.ID, Bot: b.ID, Call: call, Reason: reason, Since: time.Now()}, answer: make(chan bool, 1)}
	p := &h.approvals
	p.mu.Lock()
	p.waiting[w.ID] = w
	p.mu.Unlock()
	h.emit(team.Event{Kind: team.EventApproval, ChatID: chat.ID, BotID: b.ID, Agent: agent.Event{Call: &call}, Waiting: true})
	defer func() {
		p.mu.Lock()
		delete(p.waiting, w.ID)
		p.mu.Unlock()
		h.emit(team.Event{Kind: team.EventApproval, ChatID: chat.ID, BotID: b.ID, Agent: agent.Event{Call: &call}})
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

// Decide answers a waiting action; always also allows it from now on.
func (h *Host) Decide(id string, ok, always bool) {
	p := &h.approvals
	p.mu.Lock()
	w := p.waiting[id]
	p.mu.Unlock()
	if w == nil {
		return
	}
	if ok && always {
		h.alwaysAllow(w.Call)
	}
	select {
	case w.answer <- ok:
	default:
	}
}

// Waiting lists a chat's waiting actions ("" for every chat's), oldest first.
func (h *Host) Waiting(chatID string) []Waiting {
	p := &h.approvals
	p.mu.Lock()
	defer p.mu.Unlock()
	out := []Waiting{}
	for _, w := range p.waiting {
		if chatID == "" || w.Chat == chatID {
			out = append(out, w.Waiting)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Since.Before(out[j].Since) })
	return out
}

// DenyAll denies what a chat has waiting.
func (h *Host) DenyAll(chatID string) {
	for _, w := range h.Waiting(chatID) {
		h.Decide(w.ID, false, false)
	}
}
