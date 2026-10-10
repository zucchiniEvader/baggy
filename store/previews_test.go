package store

import (
	"testing"

	"github.com/spike/gobot/agent"
)

func TestPreviewsAndPins(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b, _ := s.CreateBot(Bot{Name: "Chef"})
	dm, _ := s.DMOf(b.ID)
	text := func(role agent.Role, s string) agent.Message {
		return agent.Message{Role: role, Parts: []agent.Part{{Kind: agent.PartText, Text: s}}}
	}
	s.Append(dm.ID, User, "", Meta{}, text(agent.RoleUser, "plan\n  dinner"))
	s.Append(dm.ID, b.ID, "t", Meta{}, text(agent.RoleAssistant, "Pasta,   then cake."))
	s.Append(dm.ID, b.ID, "t", Meta{}, agent.Message{Role: agent.RoleTool, Parts: []agent.Part{{Kind: agent.PartToolResult, Text: "ls output"}}})
	s.Append(dm.ID, b.ID, "t2", Meta{}, agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopError, Parts: []agent.Part{{Kind: agent.PartText, Text: "rate limited", IsError: true}}})
	s.Append(dm.ID, System, "", Meta{}, text(agent.RoleUser, "[a note]"))
	ps, err := s.Previews()
	if err != nil {
		t.Fatal(err)
	}
	if p := ps[dm.ID]; p.Author != b.ID || p.Text != "Pasta, then cake." {
		t.Fatalf("preview %+v", p)
	}
	s.Append(dm.ID, User, "", Meta{Files: []File{{Name: "a.pdf"}, {Name: "b.png"}}}, agent.Message{Role: agent.RoleUser})
	if ps, _ = s.Previews(); ps[dm.ID].Text != "a.pdf, b.png" || ps[dm.ID].Author != User {
		t.Fatalf("files preview %+v", ps[dm.ID])
	}

	if err := s.SetPinned(dm.ID, true); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Chat(dm.ID); c.Pinned == 0 {
		t.Fatal("not pinned")
	}
	s.SetPinned(dm.ID, false)
	if c, _ := s.Chat(dm.ID); c.Pinned != 0 {
		t.Fatal("still pinned")
	}
	if s.SetPinned("chat-none", true) != ErrNotFound {
		t.Fatal("pinned a chat that is not there")
	}
}
