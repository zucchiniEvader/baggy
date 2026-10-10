package main

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/store"
)

// BAGGY_SITE=../site go test -run TestSiteScreenshots writes the landing
// page's screenshots, light and dark, at twice their size.
func TestSiteScreenshots(t *testing.T) {
	dir := os.Getenv("BAGGY_SITE")
	if dir == "" {
		t.Skip("set BAGGY_SITE to the site folder")
	}
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("ok") }))
	chef := h.a.bots[0]
	scout, _ := h.st.CreateBot(store.Bot{Name: "Scout", Description: "Finds things on the web and in files.", Icon: "search", Color: "blue"})
	quill, _ := h.st.CreateBot(store.Bot{Name: "Quill", Description: "Writes and edits.", Icon: "pencil", Color: "purple"})
	g, _ := h.st.CreateGroup("Launch week", "Ship the beta on Friday", []string{chef.ID, scout.ID, quill.ID})
	add := func(author string, m agent.Message) { h.st.Append(g.ID, author, "", store.Meta{}, m) }
	text := func(role agent.Role, s string) agent.Message {
		m := agent.Message{Role: role, Parts: []agent.Part{{Kind: agent.PartText, Text: s}}}
		if role == agent.RoleAssistant {
			m.StopReason = agent.StopEnd
		}
		return m
	}
	add(store.User, text(agent.RoleUser, "We ship the beta on Friday. @Scout what is still open?"))
	add(scout.ID, agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopToolUse, Parts: []agent.Part{{Kind: agent.PartText, Text: "Checking the tracker."},
		{Kind: agent.PartToolCall, CallID: "c1", Name: "shell", Args: json.RawMessage(`{"command":"gh issue list --label beta"}`)}}})
	add(scout.ID, agent.Message{Role: agent.RoleTool, Parts: []agent.Part{{Kind: agent.PartToolResult, CallID: "c1", Text: "#12 crash on start\n#14 typo in onboarding\n#15 slow first sync"}}})
	add(scout.ID, text(agent.RoleAssistant, "Three issues are open:\n\n1. **#12** crash on start (blocks the beta)\n2. **#14** a typo in onboarding\n3. **#15** slow first sync"))
	add(chef.ID, text(agent.RoleAssistant, "Plan: I take **#12** today, #15 tomorrow. @Quill, fix the typo and draft the release notes?"))
	add(quill.ID, text(agent.RoleAssistant, "On it. The notes will be in `RELEASE.md` by tonight."))
	scoutDM, _ := h.st.DMOf(scout.ID)
	h.st.Append(scoutDM.ID, store.User, "", store.Meta{}, text(agent.RoleUser, "Every weekday at 8, a short brief of the news."))
	h.st.Append(scoutDM.ID, scout.ID, "t", store.Meta{}, text(agent.RoleAssistant, "Done: a routine runs weekdays at 8:00."))
	h.st.SetPinned(scoutDM.ID, true)
	h.a.reload()
	h.a.selectChat(g.ID)
	h.tt.SetSize(1200, 760)
	h.tt.SetScale(2)
	for name, dark := range map[string]bool{"light": false, "dark": true} {
		h.tt.SetDark(dark)
		h.tt.Frame()
		time.Sleep(250 * time.Millisecond)
		h.tt.Frame()
		f, err := os.Create(filepath.Join(dir, "screenshot-"+name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		png.Encode(f, h.tt.Image())
		f.Close()
	}
}
