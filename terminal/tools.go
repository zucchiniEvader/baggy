package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zucchiniEvader/baggy/tool"
)

// maxOutput is how much of a read the model gets: its end.
const maxOutput = 50 << 10

// Tools are shell, shell_input and shell_output for a bot in a chat.
func (m *Manager) Tools(chat, bot, dir string) []tool.Tool {
	return []tool.Tool{shellTool{m, chat, bot, dir}, inputTool{m, chat}, outputTool{m, chat}}
}

func clip(s string) string {
	s = strings.TrimRight(s, "\n")
	if len(s) > maxOutput {
		return "[… output cut, its end follows …]\n" + s[len(s)-maxOutput:]
	}
	return s
}

func reply(s *Session, output, note string, ended bool) tool.Result {
	var b strings.Builder
	if out := clip(output); out != "" {
		b.WriteString(out + "\n")
	}
	if !ended {
		fmt.Fprintf(&b, "[session %s] ", s.ID)
	}
	b.WriteString(note)
	isErr := ended && note != "The command exited."
	return tool.Result{Text: strings.TrimSpace(b.String()), IsError: isErr}
}

type shellTool struct {
	m              *Manager
	chat, bot, dir string
}

func (shellTool) Name() string { return "shell" }
func (shellTool) Description() string {
	return "Run a shell command (bash) in the working directory, in a terminal of its own. The call returns when the command exits, " +
		"or while it still runs: at a question it asks (a password, [Y/n]), or after 20 s without output, with a session id to answer with " +
		"shell_input or read on with shell_output. Set background for what should keep running (a server, a watcher, a long build): " +
		"the call returns after 2 s and the command runs on; you hear when it ends. Never use & or nohup for that. " +
		"timeout (seconds) kills the command when it runs that long."
}
func (shellTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
"command":{"type":"string"},
"description":{"type":"string","description":"what the command does, in a few words"},
"timeout":{"type":"integer","minimum":1},
"background":{"type":"boolean"}
},"required":["command"],"additionalProperties":false}`)
}

func (t shellTool) Execute(ctx context.Context, args json.RawMessage) tool.Result {
	var a struct {
		Command, Description string
		Timeout              int
		Background           bool
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Result{Text: err.Error(), IsError: true}
	}
	s, err := t.m.Start(t.chat, t.bot, t.dir, a.Command, a.Description, a.Background)
	if err != nil {
		return tool.Result{Text: err.Error(), IsError: true}
	}
	if a.Timeout > 0 {
		kill := time.AfterFunc(time.Duration(a.Timeout)*time.Second, func() { s.Stop(fmt.Sprintf("timed out after %d s", a.Timeout)) })
		go func() { <-s.done; kill.Stop() }()
	}
	if a.Background {
		out, note, ended := s.Wait(ctx, BackgroundSettle)
		if ctx.Err() != nil {
			s.Stop("the call was cancelled")
			return tool.Result{Text: "Stopped.", IsError: true}
		}
		if !ended {
			note = fmt.Sprintf("The command runs in the background (session %s). Read it with shell_output, stop it with shell_input text \\u0003.", s.ID)
		}
		return reply(s, out, note, ended)
	}
	out, note, ended := s.Wait(ctx, 0)
	if ctx.Err() != nil {
		if !s.Background() { // sent there meanwhile: it runs on
			s.Stop("the call was cancelled")
			return tool.Result{Text: clip(out) + "\nStopped.", IsError: true}
		}
		return reply(s, out, "The user sent the command to the background; go on with your work.", false)
	}
	return reply(s, out, note, ended)
}

type inputTool struct {
	m    *Manager
	chat string
}

func (inputTool) Name() string { return "shell_input" }
func (inputTool) Description() string {
	return "Type into a command that shell left running, then wait for what it does next, as shell does. Enter follows the text unless enter is false; " +
		"\\u0003 is Ctrl-C, \\u001b[B the Down arrow."
}
func (inputTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
"session_id":{"type":"string"},"text":{"type":"string"},"enter":{"type":"boolean"}
},"required":["session_id","text"],"additionalProperties":false}`)
}

func (t inputTool) Execute(ctx context.Context, args json.RawMessage) tool.Result {
	var a struct {
		SessionID string `json:"session_id"`
		Text      string
		Enter     *bool
	}
	json.Unmarshal(args, &a)
	s, ok := t.m.Get(a.SessionID)
	if !ok || s.Chat != t.chat {
		return tool.Result{Text: "no session " + a.SessionID + " in this chat", IsError: true}
	}
	enter := a.Enter == nil || *a.Enter
	if strings.Contains(Keys(a.Text), "\x03") {
		enter = false
	}
	if err := s.Write(a.Text, enter); err != nil {
		return tool.Result{Text: err.Error(), IsError: true}
	}
	out, note, ended := s.Wait(ctx, 0)
	if s.Background() && !ended && strings.HasPrefix(note, "No output") {
		note = fmt.Sprintf("The command runs on in the background (session %s).", s.ID)
	}
	return reply(s, out, note, ended)
}

type outputTool struct {
	m    *Manager
	chat string
}

func (outputTool) Name() string { return "shell_output" }
func (outputTool) Description() string {
	return "Read what a command shell left running printed since you last read it, waiting up to wait_seconds (default 5, at most 60) for more or for its end."
}
func (outputTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
"session_id":{"type":"string"},"wait_seconds":{"type":"integer","minimum":0,"maximum":60}
},"required":["session_id"],"additionalProperties":false}`)
}

func (t outputTool) Execute(ctx context.Context, args json.RawMessage) tool.Result {
	var a struct {
		SessionID string `json:"session_id"`
		Wait      *int   `json:"wait_seconds"`
	}
	json.Unmarshal(args, &a)
	s, ok := t.m.Get(a.SessionID)
	if !ok || s.Chat != t.chat {
		return tool.Result{Text: "no session " + a.SessionID + " in this chat", IsError: true}
	}
	wait := 5
	if a.Wait != nil {
		wait = *a.Wait
	}
	if done, how := s.Done(); done {
		return reply(s, s.unread(), "The command "+how+".", true)
	}
	out, note, ended := s.Wait(ctx, time.Duration(max(wait, 0))*time.Second+time.Millisecond)
	if !ended && strings.HasPrefix(note, "The command is still running") && s.Background() {
		note = fmt.Sprintf("The command runs on in the background (session %s).", s.ID)
	}
	return reply(s, out, note, ended)
}
