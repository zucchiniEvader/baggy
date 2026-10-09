package store

import (
	"errors"
	"os"
	"testing"

	"github.com/spike/gobot/agent"
)

func open(t *testing.T) *Store {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestSeedReopenAndMessages(t *testing.T) {
	dir := t.TempDir()
	s := must(Open(dir))
	if !must(s.Seed()) {
		t.Fatal("no Chef on first run")
	}
	chef := must(s.Bots())[0]
	if chef.Name != "Chef" {
		t.Fatalf("bad bot %+v", chef)
	}
	if _, err := os.Stat(s.Workspace(chef.ID)); err != nil {
		t.Fatal("no workspace:", err)
	}
	dm := must(s.DMOf(chef.ID))
	msg := agent.Message{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: "hi"}}}
	e := must(s.Append(dm.ID, User, "", Meta{Mentions: []string{chef.ID}}, msg))
	if e.Seq != 1 {
		t.Fatalf("seq %d", e.Seq)
	}
	must(s.Append(dm.ID, chef.ID, "t1", Meta{}, agent.Message{Role: agent.RoleAssistant, Parts: []agent.Part{{Kind: agent.PartText, Text: "hello"}}}))
	s.Close()

	s = must(Open(dir))
	defer s.Close()
	if must(s.Seed()) {
		t.Fatal("seeded twice")
	}
	es := must(s.Messages(dm.ID, 0))
	if len(es) != 2 || es[0].Msg.Parts[0].Text != "hi" || es[0].Meta.Mentions[0] != chef.ID || es[1].Author != chef.ID || es[1].Turn != "t1" {
		t.Fatalf("bad messages after reopen: %+v", es)
	}
	if must(s.Chats())[0].Updated < es[1].Created {
		t.Fatal("chat not touched")
	}
	mustDo(s.DeleteTurn(dm.ID, "t1"))
	if len(must(s.Messages(dm.ID, 0))) != 1 {
		t.Fatal("turn not deleted")
	}
	if got := must(s.Since(chef.ID, 0, 10)); len(got) != 0 {
		t.Fatalf("since: %+v", got)
	}
}

func TestGroupsOwnersAndBotDeletion(t *testing.T) {
	s := open(t)
	a := must(s.CreateBot(Bot{Name: "A"}))
	b := must(s.CreateBot(Bot{Name: "B"}))
	c := must(s.CreateBot(Bot{Name: "C"}))
	g := must(s.CreateGroup("Team", "ship it", []string{a.ID, b.ID}))
	if g.Owner != a.ID {
		t.Fatal("owner is not the first member")
	}
	mustDo(s.AddMember(g.ID, c.ID))
	mustDo(s.SetOwner(g.ID, b.ID))
	if err := s.SetOwner(g.ID, "bot-nobody"); err == nil {
		t.Fatal("non-member owner accepted")
	}
	dm := must(s.DMOf(a.ID))
	if err := s.AddMember(dm.ID, b.ID); err == nil {
		t.Fatal("DM gained a member")
	}
	mustDo(s.DeleteBot(b.ID)) // the owner
	g = must(s.Chat(g.ID))
	if len(g.Members) != 2 || g.Owner != a.ID || g.Members[1] != c.ID {
		t.Fatalf("after deleting the owner: %+v", g)
	}
	if _, err := s.DMOf(b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("DM survived its bot")
	}
	mustDo(s.RemoveMember(g.ID, a.ID))
	mustDo(s.DeleteBot(c.ID))
	if _, err := s.Chat(g.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("empty group kept")
	}
	if names := must(s.BotsNamed("a")); len(names) != 1 {
		t.Fatalf("BotsNamed: %+v", names)
	}
	if err := s.DeleteChat(must(s.DMOf(a.ID)).ID); err == nil {
		t.Fatal("DM deleted on its own")
	}
}

func TestProvidersAndSettings(t *testing.T) {
	s := open(t)
	p := must(s.SaveProvider(Provider{Name: "DeepSeek", API: "chat", BaseURL: "https://api.deepseek.com", APIKey: "k", Models: []string{"deepseek-chat"}, Options: map[string]bool{"replay_reasoning": true}}))
	p.Models = append(p.Models, "deepseek-reasoner")
	must(s.SaveProvider(p))
	got := must(s.Provider(p.ID))
	if len(got.Models) != 2 || !got.Options["replay_reasoning"] || got.APIKey != "k" {
		t.Fatalf("bad provider %+v", got)
	}
	mustDo(s.DeleteProvider(p.ID))
	if _, err := s.Provider(p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("provider not deleted")
	}
	if s.Setting("x") != "" {
		t.Fatal("unset setting not empty")
	}
	mustDo(s.SetSetting("x", "1"))
	mustDo(s.SetSetting("x", "2"))
	if s.Setting("x") != "2" {
		t.Fatal("setting not replaced")
	}
}

func mustDo(err error) {
	if err != nil {
		panic(err)
	}
}

func TestRoutines(t *testing.T) {
	s := open(t)
	b := must(s.CreateBot(Bot{Name: "A"}))
	r := must(s.SaveRoutine(Routine{BotID: b.ID, Name: "Brief", Prompt: "Summarize the news", Schedule: "0 8 * * *", Enabled: true}))
	if r.ID == "" || r.Missed != Coalesce || r.EnabledAt == 0 {
		t.Fatalf("routine %+v", r)
	}
	r.LastOutcome = OutcomePass
	must(s.SaveRoutine(r))
	if got := must(s.Routines(b.ID)); len(got) != 1 || got[0].LastOutcome != OutcomePass {
		t.Fatalf("routines %+v", got)
	}
	if _, err := s.SaveRoutine(Routine{BotID: b.ID, Name: "x", Prompt: "y", Schedule: "z", Missed: "later"}); err == nil {
		t.Fatal("bad policy accepted")
	}
	mustDo(s.DeleteBot(b.ID))
	if got := must(s.Routines("")); len(got) != 0 {
		t.Fatal("routines outlived their bot")
	}
	if s.LastUserMessage() != 0 {
		t.Fatal("no messages yet")
	}
}

func TestLooksAndUnread(t *testing.T) {
	s := open(t)
	b := must(s.CreateBot(Bot{Name: "A", Emoji: "🦊", Color: "#ff8800"}))
	if got := must(s.Bot(b.ID)); got.Emoji != "🦊" || got.Color != "#ff8800" {
		t.Fatalf("look %+v", got)
	}
	dm := must(s.DMOf(b.ID))
	must(s.Append(dm.ID, User, "", Meta{}, agent.Message{Role: agent.RoleUser}))
	must(s.Append(dm.ID, b.ID, "t", Meta{}, agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopToolUse}))
	must(s.Append(dm.ID, b.ID, "t", Meta{}, agent.Message{Role: agent.RoleTool}))
	must(s.Append(dm.ID, b.ID, "t", Meta{}, agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopEnd}))
	if n := s.Unread(dm.ID); n != 1 {
		t.Fatalf("unread %d", n)
	}
	mustDo(s.MarkRead(dm.ID))
	if n := s.Unread(dm.ID); n != 0 || must(s.Chat(dm.ID)).ReadSeq != 4 {
		t.Fatalf("after read %d", n)
	}
}
