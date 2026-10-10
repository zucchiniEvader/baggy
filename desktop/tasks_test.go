//go:build darwin || linux

package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/terminal"
)

func TestRunningTasks(t *testing.T) {
	h := newHarness(t, script(func(bot string, msgs []agent.Message) agent.Message {
		if msgs[len(msgs)-1].Role == agent.RoleTool {
			return say("Started.")
		}
		return agent.Message{StopReason: agent.StopToolUse, Parts: []agent.Part{{Kind: agent.PartToolCall, CallID: "c1", Name: "shell",
			Args: json.RawMessage(`{"command":"echo serving; sleep 60","description":"Dev server","background":true}`)}}}
	}))
	h.a.team.Terminals = terminal.NewManager()
	defer h.a.team.Terminals.StopAll()
	h.send("start the server")
	h.pump("the reply", func() bool { return h.tt.HasText("Started.") && h.idle() })
	time.Sleep(taskDelay)
	h.tt.Frame()
	if err := h.tt.Click("Tasks (1)"); err != nil {
		t.Fatalf("%v: %q", err, h.tt.Texts())
	}
	h.tt.Frame()
	if !h.tt.HasText("Dev server") || !h.tt.HasText("serving") {
		t.Fatalf("task: %q", h.tt.Texts())
	}
	h.tt.Click("Stop")
	h.pump("the stop", func() bool { return len(h.a.team.Terminals.Running("")) == 0 })
	h.tt.Frame()
	if h.tt.HasText("Tasks (1)") {
		t.Fatal("task still listed")
	}
}
