package team

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/claudecode"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/tool"
)

// ClaudeCodeSettle is how long claude_code waits before it returns, so a
// task that fails at once (not signed in, no folder) reads as a failure.
var ClaudeCodeSettle = 3 * time.Second

// ClaudeCodeCall is the name a request of Claude Code's to use a tool takes
// before Approve: "claude_code:" and the tool's name, its input the call's
// arguments.
const ClaudeCodeCall = "claude_code:"

// maxReport is how much of Claude Code's report a bot reads.
const maxReport = 12 << 10

// claudeCodeTools are claude_code, claude_code_status and claude_code_stop
// for a bot that hands its coding tasks to Claude Code.
func (t *Team) claudeCodeTools(b store.Bot, chat store.Chat, dir string) []tool.Tool {
	if t.ClaudeCode == nil || !b.ClaudeCode {
		return nil
	}
	m := t.ClaudeCode
	find := func(id string) (*claudecode.Run, tool.Result, bool) {
		var r *claudecode.Run
		if id != "" {
			r, _ = m.Get(id)
		} else {
			r = m.Latest(chat.ID, b.ID)
		}
		if r == nil || r.Chat != chat.ID {
			return nil, errResult("no Claude Code run %s in this chat", id), false
		}
		return r, tool.Result{}, true
	}
	return []tool.Tool{
		Func{N: "claude_code", D: "Hand a coding task to Claude Code, the user's coding agent on this computer, which works on its own in your folder (" + dir + "): it reads the code, edits files and runs commands. " +
			"Write the task in full: the goal, the files or area, the constraints, and how to check it. It runs in the background and the call returns at once; you hear its report when it ends, so do not poll. " +
			"A later task continues the same Claude Code conversation, so a follow-up can say \"now also…\"; new_session starts a fresh one for unrelated work. One task at a time per chat.",
			S: `{"type":"object","properties":{"task":{"type":"string"},"new_session":{"type":"boolean","description":"start a fresh Claude Code conversation instead of continuing the last one"}},"required":["task"],"additionalProperties":false}`,
			F: func(ctx context.Context, raw json.RawMessage) tool.Result {
				var a struct {
					Task       string
					NewSession bool `json:"new_session"`
				}
				json.Unmarshal(raw, &a)
				resume := ""
				if !a.NewSession {
					resume = t.Store.ClaudeSession(chat.ID, b.ID, dir)
				}
				// told says whether this call reported the end, so the bot
				// is not told twice.
				told := make(chan bool, 1)
				r, err := m.Start(claudecode.Options{Chat: chat.ID, Bot: b.ID, Dir: dir, Task: a.Task, Resume: resume,
					Approve: t.claudeCodeApprove(b, chat),
					OnEnd:   func(r *claudecode.Run) { t.claudeCodeEnded(r, <-told) }})
				if err != nil {
					return errResult("%v", err)
				}
				done := r.Wait(ctx, ClaudeCodeSettle)
				told <- done
				if done {
					report := ClaudeCodeReport(r)
					if r.Stopped() || !strings.HasPrefix(report, "[Claude Code finished") {
						return tool.Result{Text: report, IsError: true}
					}
					return tool.Result{Text: report}
				}
				how := "a new Claude Code conversation"
				if resume != "" {
					how = "the same Claude Code conversation as before"
				}
				return tool.Result{Text: fmt.Sprintf("Claude Code is working on it (run %s, %s). You will hear its report when it ends: go on, or end your turn.", r.ID, how)}
			}},
		Func{N: "claude_code_status", D: "See how a Claude Code task is going: its recent steps and what it said last, or its report once it has ended. wait_seconds (at most 60) waits for it to end first.",
			S: `{"type":"object","properties":{"run_id":{"type":"string","description":"the run; your latest in this chat when left out"},"wait_seconds":{"type":"integer","minimum":0,"maximum":60}},"additionalProperties":false}`,
			F: func(ctx context.Context, raw json.RawMessage) tool.Result {
				var a struct {
					RunID string `json:"run_id"`
					Wait  int    `json:"wait_seconds"`
				}
				json.Unmarshal(raw, &a)
				r, res, ok := find(a.RunID)
				if !ok {
					return res
				}
				if a.Wait > 0 {
					r.Wait(ctx, time.Duration(min(a.Wait, 60))*time.Second)
				}
				return tool.Result{Text: claudeCodeStatus(r)}
			}},
		Func{N: "claude_code_stop", D: "Stop a Claude Code task.",
			S: `{"type":"object","properties":{"run_id":{"type":"string","description":"the run; your latest in this chat when left out"}},"additionalProperties":false}`,
			F: func(ctx context.Context, raw json.RawMessage) tool.Result {
				var a struct {
					RunID string `json:"run_id"`
				}
				json.Unmarshal(raw, &a)
				r, res, ok := find(a.RunID)
				if !ok {
					return res
				}
				if done, how := r.Done(); done {
					return tool.Result{Text: fmt.Sprintf("Run %s has already ended (%s).", r.ID, how)}
				}
				r.Stop(b.Name + " stopped it")
				return tool.Result{Text: fmt.Sprintf("Stopped run %s. Its last steps:\n%s", r.ID, r.Progress(10))}
			}},
	}
}

// claudeCodeApprove puts Claude Code's requests to use a tool through
// Approve, as a call of the bot's named ClaudeCodeCall plus the tool.
func (t *Team) claudeCodeApprove(b store.Bot, chat store.Chat) func(context.Context, claudecode.Request) (bool, string) {
	if t.Approve == nil {
		return nil
	}
	return func(ctx context.Context, req claudecode.Request) (bool, string) {
		call := agent.Part{Kind: agent.PartToolCall, CallID: "cc-" + req.ID, Name: ClaudeCodeCall + req.Tool, Args: req.Input}
		if res := t.Approve(ctx, b, chat, call); res != nil {
			return false, res.Text
		}
		return true, ""
	}
}

// claudeCodeEnded keeps the run's session for the next task, and, when it
// ended by itself after claude_code returned (told is false), tells the bot
// in the chat it started in.
func (t *Team) claudeCodeEnded(r *claudecode.Run, told bool) {
	if s := r.Session(); s != "" {
		t.Store.SetClaudeSession(r.Chat, r.Bot, r.Dir, s)
	}
	if told || r.Stopped() {
		return
	}
	msg := agent.Message{ID: store.NewID("msg"), Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: ClaudeCodeReport(r)}}}
	t.deliver(r.Chat, store.System, store.Meta{}, msg)
}

// ClaudeCodeReport is what a bot reads when its Claude Code task ends: a
// first line that stands alone (the desktop shows only it), then the rest.
func ClaudeCodeReport(r *claudecode.Run) string {
	_, how := r.Done()
	took := r.Took().Round(time.Second)
	steps := len(r.Steps())
	if how != "finished" {
		text := fmt.Sprintf("[Claude Code run %s %s, after %s and %d steps.]", r.ID, how, took, steps)
		if p := r.Progress(8); p != "" {
			text += "\nIts last steps:\n" + p
		}
		return text
	}
	text := fmt.Sprintf("[Claude Code finished run %s in %s, %d steps.]\n", r.ID, took, steps)
	if r.Fresh() {
		text += "The earlier conversation was gone, so it started a new one. "
	}
	return text + "Check its work (the diff, the build, the tests) before you report it done. Its report:\n\n" + clipReport(r.Result())
}

func claudeCodeStatus(r *claudecode.Run) string {
	if done, _ := r.Done(); done {
		return ClaudeCodeReport(r)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Run %s is working (%s, %d steps).", r.ID, r.Took().Round(time.Second), len(r.Steps()))
	if p := r.Progress(15); p != "" {
		b.WriteString(" Recent steps:\n" + p)
	}
	if s := r.Text(); s != "" {
		b.WriteString("\nIt said last: " + clipReport(s))
	}
	return b.String()
}

func clipReport(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxReport {
		return s[:maxReport] + "\n[… cut …]"
	}
	if s == "" {
		return "(no report)"
	}
	return s
}

const claudeCodePrompt = "## Claude Code\n\nCode changes are Claude Code's work: hand each coding task to it with claude_code rather than editing code yourself, and keep your own tools for looking, checking and small things. Write the task in full, as for a capable colleague new to the request. It works in the background; when you hear its report, check the work (the diff, the build, the tests) and send it back with claude_code when something is wrong, before you tell anyone it is done."
