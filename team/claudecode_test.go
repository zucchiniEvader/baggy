//go:build darwin || linux

package team

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/claudecode"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/tool"
)

// fakeClaude speaks Claude Code's stream-json: one Bash request to approve,
// then a result naming the session and the answer.
const fakeClaude = `#!/bin/sh
read init
read user
session=s-new
case "$*" in *"--resume "*) session=$(echo "$*" | sed 's/.*--resume \([^ ]*\).*/\1/');; esac
echo '{"type":"system","subtype":"init","session_id":"'$session'","apiKeySource":"none"}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"make"}}]}}'
echo '{"type":"control_request","request_id":"q1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"make"}}}'
read answer
case "$answer" in *'"allow"'*) ok=allowed;; *) ok=denied;; esac
sleep ${FAKE_SLEEP:-0}
echo '{"type":"result","subtype":"success","is_error":false,"num_turns":2,"session_id":"'$session'","result":"make was '$ok' in session '$session'"}'
cat >/dev/null
`

func TestClaudeCodeHandsOffAndReports(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	os.WriteFile(bin, []byte(fakeClaude), 0o755)
	reports := make(chan string, 4)
	var mu sync.Mutex
	var names []string
	f := setup(t, script(func(bot string, msgs []agent.Message) agent.Message {
		l := last(msgs)
		switch {
		case strings.HasPrefix(l, "[Claude Code"):
			reports <- l
			return say("Checked it.")
		case msgs[len(msgs)-1].Role == agent.RoleTool:
			reports <- l
			return say("Handed over.")
		}
		return call("claude_code", map[string]any{"task": "build it"})
	}))
	f.team.ClaudeCode = claudecode.NewManager()
	f.team.ClaudeCode.Command = bin
	f.team.ClaudeCode.Env = func() []string { return append(os.Environ(), "FAKE_SLEEP=1") }
	f.team.Approve = func(ctx context.Context, b store.Bot, chat store.Chat, call agent.Part) *tool.Result {
		if strings.HasPrefix(call.Name, ClaudeCodeCall) {
			mu.Lock()
			names = append(names, call.Name+" "+string(call.Args))
			mu.Unlock()
		}
		return nil
	}
	old := ClaudeCodeSettle
	ClaudeCodeSettle = 100 * time.Millisecond
	defer func() { ClaudeCodeSettle = old }()

	b := f.bot(t, "Eng")
	b.ClaudeCode, b.Workdir = true, dir
	if err := f.st.UpdateBot(b); err != nil {
		t.Fatal(err)
	}
	dm, _ := f.st.DMOf(b.ID)
	wait := func() string {
		select {
		case r := <-reports:
			return r
		case <-time.After(15 * time.Second):
			t.Fatal("no report")
		}
		return ""
	}

	f.team.Send(dm.ID, "please build", nil, nil)
	if got := wait(); !strings.Contains(got, "working on it (run c1, a new Claude Code conversation)") {
		t.Fatalf("the call should return while it works: %q", got)
	}
	got := wait()
	if !strings.HasPrefix(got, "[Claude Code finished run c1") || !strings.Contains(got, "make was allowed in session s-new") {
		t.Fatalf("the bot should be woken with the report: %q", got)
	}
	f.team.Wait()
	mu.Lock()
	if len(names) != 1 || names[0] != `claude_code:Bash {"command":"make"}` {
		t.Errorf("Approve saw %q", names)
	}
	mu.Unlock()
	if s := f.st.ClaudeSession(dm.ID, b.ID, dir); s != "s-new" {
		t.Errorf("session kept: %q", s)
	}

	// Quick enough to end within the settle: the call itself reports, once,
	// and the next task continues the session.
	ClaudeCodeSettle = 10 * time.Second
	f.team.ClaudeCode.Env = os.Environ
	f.team.Send(dm.ID, "again", nil, nil)
	if got := wait(); !strings.HasPrefix(got, "[Claude Code finished run c2") || !strings.Contains(got, "in session s-new") {
		t.Fatalf("the call should carry the report: %q", got)
	}
	f.team.Wait()
	select {
	case extra := <-reports:
		t.Fatalf("told twice: %q", extra)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestClaudeCodeOnlyForItsBots(t *testing.T) {
	var mu sync.Mutex
	var system string
	var names []string
	f := setup(t, modelFunc(func(sys string, msgs []agent.Message, tools []tool.Tool) agent.Message {
		mu.Lock()
		defer mu.Unlock()
		system, names = sys, nil
		for _, tl := range tools {
			names = append(names, tl.Name())
		}
		return say("ok")
	}))
	f.team.ClaudeCode = claudecode.NewManager()
	b := f.bot(t, "Plain")
	dm, _ := f.st.DMOf(b.ID)
	f.team.Send(dm.ID, "hi", nil, nil)
	f.team.Wait()
	mu.Lock()
	if strings.Contains(strings.Join(names, " "), "claude_code") || strings.Contains(system, "## Claude Code") {
		t.Errorf("a bot without Claude Code got %v", names)
	}
	mu.Unlock()
	b.ClaudeCode = true
	f.st.UpdateBot(b)
	f.team.Send(dm.ID, "hi", nil, nil)
	f.team.Wait()
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(strings.Join(names, " "), "claude_code claude_code_status claude_code_stop") || !strings.Contains(system, "## Claude Code") {
		t.Errorf("tools %v", names)
	}
}
