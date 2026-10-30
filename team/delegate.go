package team

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/tool"
)

// MaxDelegates is how many tasks one delegate call hands out at most.
const MaxDelegates = 6

// DelegateTimeout is how long a delegated task may work.
var DelegateTimeout = 30 * time.Minute

// Delegation is one delegate call: tasks a bot handed to workers of their
// own, which run side by side in the background, each with a fresh context,
// and report together when the last ends.
type Delegation struct {
	ID      string
	Chat    string // where it was started, and where the report goes
	Bot     string // the bot that delegated
	Started time.Time
	Tasks   []*Task

	cancel context.CancelFunc
	done   chan struct{}
}

// Task is one worker of a delegation.
type Task struct {
	Title string
	Brief string
	Bot   string // whose role, model and folder the worker takes

	mu      sync.Mutex
	steps   []string
	text    string // what it said last
	state   string // "working", "done", "failed", "stopped"
	report  string
	took    time.Duration
	started time.Time
}

// Status is the task's state and report (empty while it works).
func (k *Task) Status() (state, report string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.state, k.report
}

// Steps are the task's last n tool calls, one line each.
func (k *Task) Steps(n int) []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]string(nil), k.steps[max(0, len(k.steps)-n):]...)
}

// Text is what the task's worker said last.
func (k *Task) Text() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.text
}

// Done reports whether every task has ended.
func (d *Delegation) Done() bool {
	select {
	case <-d.done:
		return true
	default:
		return false
	}
}

// Stop ends the delegation's tasks under way.
func (d *Delegation) Stop() { d.cancel() }

// Delegations are a chat's delegations under way ("" for every chat).
func (t *Team) Delegations(chatID string) []*Delegation {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []*Delegation
	for _, d := range t.delegations {
		if (chatID == "" || d.Chat == chatID) && !d.Done() {
			out = append(out, d)
		}
	}
	return out
}

// StopDelegations ends every delegation under way (quitting).
func (t *Team) StopDelegations() {
	for _, d := range t.Delegations("") {
		d.Stop()
	}
}

func (t *Team) delegation(id string) *Delegation {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, d := range t.delegations {
		if d.ID == id {
			return d
		}
	}
	return nil
}

// delegateTools are delegate, delegate_status and delegate_stop.
func (t *Team) delegateTools(b store.Bot, chat store.Chat, r *run) []tool.Tool {
	find := func(id string) (*Delegation, tool.Result, bool) {
		var d *Delegation
		if id != "" {
			d = t.delegation(id)
		} else {
			t.mu.Lock()
			for i := len(t.delegations) - 1; i >= 0 && d == nil; i-- {
				if x := t.delegations[i]; x.Chat == chat.ID && x.Bot == b.ID {
					d = x
				}
			}
			t.mu.Unlock()
		}
		if d == nil || d.Chat != chat.ID {
			return nil, errResult("no delegation %s in this chat", id), false
		}
		return d, tool.Result{}, true
	}
	return []tool.Tool{
		Func{N: "delegate", D: fmt.Sprintf("Hand tasks to workers that run side by side in the background, each with a fresh context of its own and your tools (files, search, commands), so the reading and trying stays out of your context and only their reports come back. "+
			"Use it for work that is large, reads a lot, or splits into parts that do not depend on each other (research several things, go through many files, compare options). "+
			"Each worker knows only its brief: write it in full, as for a capable colleague new to the request (the goal, where to look, what to leave alone, what the report should hold). "+
			"bot runs a task as that teammate, with its description, model and folder. The call returns at once; you hear all the reports together when the last task ends, so do not poll. At most %d tasks.", MaxDelegates),
			S: fmt.Sprintf(`{"type":"object","properties":{"tasks":{"type":"array","minItems":1,"maxItems":%d,"items":{"type":"object","properties":{"title":{"type":"string","description":"a few words, shown to the user"},"brief":{"type":"string","description":"the task in full"},"bot":{"type":"string","description":"a teammate's id or name to do it as; yourself when left out"}},"required":["title","brief"],"additionalProperties":false}}},"required":["tasks"],"additionalProperties":false}`, MaxDelegates),
			F: func(ctx context.Context, raw json.RawMessage) tool.Result {
				var a struct {
					Tasks []struct{ Title, Brief, Bot string }
				}
				json.Unmarshal(raw, &a)
				d := &Delegation{ID: store.NewID("dlg"), Chat: chat.ID, Bot: b.ID, Started: t.now(), done: make(chan struct{})}
				var roles []store.Bot
				for _, x := range a.Tasks {
					role := b
					if strings.TrimSpace(x.Bot) != "" {
						var err error
						if role, err = t.findBot(x.Bot); err != nil {
							return errResult("%s: %v", x.Title, err)
						}
					}
					roles = append(roles, role)
					d.Tasks = append(d.Tasks, &Task{Title: strings.TrimSpace(x.Title), Brief: x.Brief, Bot: role.ID, state: "working"})
				}
				t.mu.Lock()
				hops := r.hops
				t.mu.Unlock()
				t.startDelegation(d, b, chat, roles, hops)
				var names []string
				for _, k := range d.Tasks {
					names = append(names, k.Title)
				}
				return tool.Result{Text: fmt.Sprintf("Delegated %d task(s) (delegation %s): %s. You will hear the reports when the last one ends: go on, or end your turn.", len(d.Tasks), d.ID, strings.Join(names, "; "))}
			}},
		Func{N: "delegate_status", D: "See how delegated tasks are going: each task's state and recent steps, or the reports once they have ended. wait_seconds (at most 60) waits for them to end first.",
			S: `{"type":"object","properties":{"delegation_id":{"type":"string","description":"your latest in this chat when left out"},"wait_seconds":{"type":"integer","minimum":0,"maximum":60}},"additionalProperties":false}`,
			F: func(ctx context.Context, raw json.RawMessage) tool.Result {
				var a struct {
					ID   string `json:"delegation_id"`
					Wait int    `json:"wait_seconds"`
				}
				json.Unmarshal(raw, &a)
				d, res, ok := find(a.ID)
				if !ok {
					return res
				}
				if a.Wait > 0 {
					select {
					case <-d.done:
					case <-ctx.Done():
					case <-time.After(time.Duration(min(a.Wait, 60)) * time.Second):
					}
				}
				if d.Done() {
					return tool.Result{Text: DelegationReport(d)}
				}
				return tool.Result{Text: delegationStatus(d, t.now())}
			}},
		Func{N: "delegate_stop", D: "Stop delegated tasks still at work; those done keep their reports.",
			S: `{"type":"object","properties":{"delegation_id":{"type":"string","description":"your latest in this chat when left out"}},"additionalProperties":false}`,
			F: func(ctx context.Context, raw json.RawMessage) tool.Result {
				var a struct {
					ID string `json:"delegation_id"`
				}
				json.Unmarshal(raw, &a)
				d, res, ok := find(a.ID)
				if !ok {
					return res
				}
				if d.Done() {
					return tool.Result{Text: fmt.Sprintf("Delegation %s has already ended.", d.ID)}
				}
				d.Stop()
				<-d.done
				return tool.Result{Text: DelegationReport(d)}
			}},
	}
}

// startDelegation runs a delegation's tasks side by side; when the last
// ends, the report goes to the chat as a note that wakes the bot (in a
// group, the bot first). A delegation stopped by its bot reports nothing:
// delegate_stop returned it.
func (t *Team) startDelegation(d *Delegation, b store.Bot, chat store.Chat, roles []store.Bot, hops int) {
	ctx, cancel := context.WithTimeout(context.Background(), DelegateTimeout)
	d.cancel = cancel
	t.mu.Lock()
	t.delegations = append(t.delegations, d)
	t.mu.Unlock()
	t.emit(Event{Kind: EventDelegation, ChatID: chat.ID, BotID: b.ID})
	t.active.Add(1) // Wait waits for the report too
	var wg sync.WaitGroup
	for i, k := range d.Tasks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t.work1(ctx, d, k, b, roles[i], chat)
			t.emit(Event{Kind: EventDelegation, ChatID: chat.ID, BotID: b.ID})
		}()
	}
	go func() {
		defer t.active.Done()
		wg.Wait()
		stopped := ctx.Err() == context.Canceled
		cancel()
		close(d.done)
		t.emit(Event{Kind: EventDelegation, ChatID: chat.ID, BotID: b.ID})
		t.forgetDelegations()
		if stopped {
			return
		}
		msg := agent.Message{ID: store.NewID("msg"), Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: DelegationReport(d)}}}
		meta := store.Meta{Hops: hops}
		if chat.Kind == store.Group {
			meta.Mentions = []string{b.ID}
		}
		t.deliver(chat.ID, store.System, meta, msg)
	}()
}

// forgetDelegations keeps the ended ones of the last hour, for
// delegate_status.
func (t *Team) forgetDelegations() {
	t.mu.Lock()
	defer t.mu.Unlock()
	kept := t.delegations[:0]
	for _, d := range t.delegations {
		if !d.Done() || t.now().Sub(d.Started) < time.Hour {
			kept = append(kept, d)
		}
	}
	t.delegations = kept
}

// work1 runs one task: a fresh agent over its brief with the role's tools,
// its calls through Approve in the chat it came from.
func (t *Team) work1(ctx context.Context, d *Delegation, k *Task, b, role store.Bot, chat store.Chat) {
	k.mu.Lock()
	k.started = t.now()
	k.mu.Unlock()
	finish := func(state, report string) {
		k.mu.Lock()
		k.state, k.report, k.took = state, report, t.now().Sub(k.started)
		k.mu.Unlock()
	}
	model, err := t.Model(role)
	if err != nil {
		finish("failed", err.Error())
		return
	}
	ws := t.Store.WorkDir(role)
	var tools []tool.Tool
	if t.Tools != nil {
		tools = t.Tools(role, ws)
	}
	hooks := agent.Hooks{}
	if t.Approve != nil {
		hooks.BeforeToolCall = func(call agent.Part) *tool.Result { return t.Approve(ctx, role, chat, call) }
	}
	out := agent.Run(ctx, model, []agent.Message{textMessage(k.Brief)}, agent.Options{
		System: workerPrompt(b, role, ws, t.now()),
		Tools:  tools,
		Hooks:  hooks,
		Retry:  t.Retry,
		Emit: func(e agent.Event) {
			switch e.Kind {
			case agent.EventToolStart:
				k.mu.Lock()
				k.steps = append(k.steps, e.Call.Name+" "+oneLine(argsLine(e.Call.Args), 100))
				k.mu.Unlock()
				t.emit(Event{Kind: EventDelegation, ChatID: chat.ID, BotID: b.ID})
			case agent.EventMessageEnd:
				if e.Message.Role == agent.RoleAssistant {
					if s := strings.TrimSpace(textOf(*e.Message)); s != "" {
						k.mu.Lock()
						k.text = s
						k.mu.Unlock()
					}
				}
			}
		},
	})
	var reply agent.Message
	if len(out) > 0 {
		reply = out[len(out)-1]
	}
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		finish("failed", fmt.Sprintf("it ran out of time (%s)", DelegateTimeout))
	case ctx.Err() != nil:
		finish("stopped", "")
	case reply.StopReason == agent.StopError:
		finish("failed", errorOf(reply))
	default:
		finish("done", strings.TrimSpace(textOf(reply)))
	}
}

// argsLine is a call's arguments in a line: the first string, as a
// command or a path.
func argsLine(raw json.RawMessage) string {
	var a map[string]any
	if json.Unmarshal(raw, &a) != nil {
		return string(raw)
	}
	for _, k := range []string{"command", "path", "pattern", "query", "url"} {
		if s, ok := a[k].(string); ok {
			return s
		}
	}
	return string(raw)
}

func errorOf(m agent.Message) string {
	var b strings.Builder
	for _, p := range m.Parts {
		if p.Kind == agent.PartText && p.IsError {
			b.WriteString(p.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

func workerPrompt(b, role store.Bot, ws string, now time.Time) string {
	var s strings.Builder
	fmt.Fprintf(&s, "You are %s (id %s), working on one task that %s handed you. ", role.Name, role.ID, b.Name)
	if role.ID == b.ID {
		s.WriteString("You are a worker of your own, with a fresh context: you know only the task below. ")
	}
	fmt.Fprintf(&s, "Your tools run in %s. Today is %s.\n\n", ws, now.Format("Monday, 2 January 2006"))
	if d := strings.TrimSpace(role.Description); d != "" {
		s.WriteString("## Who you are\n\n" + d + "\n\n")
	}
	s.WriteString("## How to work\n\nNo one is there to answer questions: decide what is reasonable and say what you assumed. Do the task, check what you can, and stay inside it. " +
		"End with your report, which is all " + b.Name + " sees of your work: what you found or did, where (files, paths, links), what you could not do and why, and what is left. Keep it short and complete; leave out the steps that led nowhere.")
	return s.String()
}

// DelegationReport is what a bot reads when its delegation ends: a first
// line that stands alone (the desktop shows only it), then each task's
// report.
func DelegationReport(d *Delegation) string {
	var b strings.Builder
	n, ok := len(d.Tasks), 0
	for _, k := range d.Tasks {
		if st, _ := k.Status(); st == "done" {
			ok++
		}
	}
	fmt.Fprintf(&b, "[Delegation %s ended: %d of %d task(s) done.]\n", d.ID, ok, n)
	for i, k := range d.Tasks {
		st, report := k.Status()
		k.mu.Lock()
		took := k.took.Round(time.Second)
		k.mu.Unlock()
		fmt.Fprintf(&b, "\n## %d. %s (%s, %s)\n\n", i+1, k.Title, st, took)
		switch {
		case st == "stopped":
			b.WriteString("Stopped before it finished.")
			if s := k.Steps(5); len(s) > 0 {
				b.WriteString(" Its last steps:\n" + strings.Join(s, "\n"))
			}
		case st == "failed":
			b.WriteString("It failed: " + report)
		default:
			b.WriteString(clipReport(report))
		}
		b.WriteString("\n")
	}
	if ok > 0 {
		b.WriteString("\nCheck what matters in the reports before you rely on them.")
	}
	return b.String()
}

func delegationStatus(d *Delegation, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Delegation %s is working (%s).", d.ID, now.Sub(d.Started).Round(time.Second))
	for i, k := range d.Tasks {
		st, _ := k.Status()
		fmt.Fprintf(&b, "\n\n%d. %s: %s", i+1, k.Title, st)
		if s := k.Steps(8); len(s) > 0 {
			b.WriteString("\n" + strings.Join(s, "\n"))
		}
	}
	return b.String()
}

const delegatePrompt = "## Delegating\n\nA task that is large, reads a lot, or splits into independent parts goes to workers with delegate rather than through your own context: they work side by side, each fresh, and only their reports come back. Keep the plan, the decisions and the reports here; when the reports come, check what matters and put them together for whoever asked. Small things you do yourself."
