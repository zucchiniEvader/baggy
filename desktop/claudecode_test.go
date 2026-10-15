//go:build darwin || linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/claudecode"
	"github.com/zucchiniEvader/baggy/team"
)

// fakeClaude asks to run make, then reports whether it could.
const fakeClaude = `#!/bin/sh
read init
read user
echo '{"type":"system","subtype":"init","session_id":"s1","apiKeySource":"none"}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"make"}}]}}'
echo '{"type":"control_request","request_id":"q1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"make"}}}'
read answer
case "$answer" in *'"allow"'*) ok=allowed;; *) ok=denied;; esac
echo '{"type":"result","subtype":"success","is_error":false,"num_turns":2,"session_id":"s1","result":"make was '$ok'"}'
cat >/dev/null
`

func TestClaudeCodeApproval(t *testing.T) {
	h := newHarness(t, script(func(bot string, msgs []agent.Message) agent.Message {
		if l := msgs[len(msgs)-1]; l.Role == agent.RoleTool {
			return say("handed: " + l.Parts[0].Text)
		}
		return agent.Message{StopReason: agent.StopToolUse, Parts: []agent.Part{{Kind: agent.PartToolCall, CallID: "c1", Name: "claude_code", Args: []byte(`{"task":"build it"}`)}}}
	}))
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	os.WriteFile(bin, []byte(fakeClaude), 0o755)
	h.a.team.ClaudeCode = claudecode.NewManager()
	h.a.team.ClaudeCode.Command = bin
	defer h.a.team.ClaudeCode.StopAll()
	old := team.ClaudeCodeSettle
	team.ClaudeCodeSettle = 10 * time.Second
	defer func() { team.ClaudeCodeSettle = old }()
	b := h.a.byID[h.a.chat(h.a.sel).Owner]
	b.ClaudeCode, b.Workdir = true, dir
	if err := h.st.UpdateBot(b); err != nil {
		t.Fatal(err)
	}
	h.a.edited("")
	h.a.prefs.commands = ModeAsk
	h.a.applyPrefs()

	h.send("build")
	h.pump("Claude Code's request", func() bool { return h.tt.HasText("in Claude Code, Bash: make") && h.tt.HasText("Allow") })
	if err := h.tt.Click("Allow"); err != nil {
		t.Fatal(err)
	}
	h.pump("the report", func() bool { return h.tt.HasText("make was allowed") && h.idle() })
	for _, s := range h.tt.Texts() {
		if strings.Contains(s, "handed: [Claude Code finished run c1") {
			return
		}
	}
	t.Fatalf("texts %q", h.tt.Texts())
}

func TestApprovalRulesForClaudeCode(t *testing.T) {
	bash := agent.Part{Name: team.ClaudeCodeCall + "Bash", Args: []byte(`{"command":"go test ./..."}`)}
	fetch := agent.Part{Name: team.ClaudeCodeCall + "WebFetch", Args: []byte(`{"url":"https://example.com"}`)}
	if rule(bash) != "claude_code:Bash:go test ./..." || rule(fetch) != "tool:claude_code:WebFetch" {
		t.Errorf("rules %q %q", rule(bash), rule(fetch))
	}
	if describe(bash) != "in Claude Code, Bash: go test ./..." {
		t.Errorf("describe %q", describe(bash))
	}
}
