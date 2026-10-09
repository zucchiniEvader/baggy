package team

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/tool"
)

// script answers as the bot the system prompt names.
type script func(bot string, msgs []agent.Message) agent.Message

var whoRe = regexp.MustCompile(`You are (\S+) \(id`)

func (s script) Call(ctx context.Context, system string, msgs []agent.Message, tools []tool.Tool, emit func(agent.Event)) agent.Message {
	if ctx.Err() != nil {
		return agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopAborted}
	}
	m := s(whoRe.FindStringSubmatch(system)[1], msgs)
	m.Role = agent.RoleAssistant
	if m.StopReason == "" {
		m.StopReason = agent.StopEnd
	}
	return m
}

func say(s string) agent.Message {
	return agent.Message{Parts: []agent.Part{{Kind: agent.PartText, Text: s}}}
}

func call(name string, args any) agent.Message {
	raw, _ := json.Marshal(args)
	return agent.Message{StopReason: agent.StopToolUse, Parts: []agent.Part{{Kind: agent.PartToolCall, CallID: store.NewID("call"), Name: name, Args: raw}}}
}

func last(msgs []agent.Message) string {
	if len(msgs) == 0 {
		return ""
	}
	m := msgs[len(msgs)-1]
	var b strings.Builder
	for _, p := range m.Parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

type fixture struct {
	st   *store.Store
	team *Team
	mu   sync.Mutex
	evs  []Event
}

func setup(t *testing.T, s agent.Model) *fixture {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := &fixture{st: st}
	f.team = &Team{Store: st, Model: func(store.Bot) (agent.Model, error) { return s, nil },
		Emit: func(e Event) { f.mu.Lock(); f.evs = append(f.evs, e); f.mu.Unlock() }}
	return f
}

func (f *fixture) bot(t *testing.T, name string) store.Bot {
	b, err := f.st.CreateBot(store.Bot{Name: name, Description: name + " helps."})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (f *fixture) texts(t *testing.T, chatID string) []string {
	es, err := f.st.Messages(chatID, 0)
	if err != nil {
		t.Fatal(err)
	}
	bots, _ := f.st.Bots()
	names := botNames(bots)
	var out []string
	for _, e := range es {
		who := "user"
		if e.Author != store.User {
			who = names[e.Author].Name
		}
		if e.Msg.Role == agent.RoleTool {
			continue
		}
		out = append(out, who+": "+textOf(e.Msg))
	}
	return out
}

func TestDMReplyPersistsAndSteers(t *testing.T) {
	release, entered := make(chan struct{}), make(chan struct{}, 1)
	var calls int
	f := setup(t, script(func(bot string, msgs []agent.Message) agent.Message {
		calls++
		if calls == 1 {
			entered <- struct{}{}
			<-release // the user sends again meanwhile
		}
		return say("re: " + last(msgs))
	}))
	chef := f.bot(t, "Chef")
	dm, _ := f.st.DMOf(chef.ID)
	if err := f.team.Send(dm.ID, "one", nil, nil); err != nil {
		t.Fatal(err)
	}
	<-entered
	f.team.Send(dm.ID, "two", nil, nil) // steering: taken at the next call
	close(release)
	f.team.Wait()
	got := strings.Join(f.texts(t, dm.ID), " | ")
	if got != "user: one | Chef: re: one | user: two | Chef: re: two" {
		t.Fatalf("transcript: %s", got)
	}
	// A new team over the same store sees the same transcript.
	es, _ := f.st.Messages(dm.ID, 0)
	bots, _ := f.st.Bots()
	if p := Project(es, chef.ID, bots); len(p) != 4 || p[1].Role != agent.RoleAssistant {
		t.Fatalf("projection: %+v", p)
	}
}

// lastUser is the newest user message that is not a turn cue.
func lastUser(msgs []agent.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if t := last(msgs[i : i+1]); msgs[i].Role == agent.RoleUser && !strings.HasPrefix(t, "[") {
			return t
		}
	}
	return ""
}

// replyOnce answers the user once; B always passes.
func replyOnce(bot string, msgs []agent.Message) agent.Message {
	for _, m := range msgs {
		if m.Role == agent.RoleAssistant {
			return say("PASS")
		}
	}
	if bot == "B" {
		return say("PASS")
	}
	return say(bot + " on " + lastUser(msgs))
}

func TestGroupRoomPassesAndEnds(t *testing.T) {
	f := setup(t, script(replyOnce))
	a, b, c := f.bot(t, "A"), f.bot(t, "B"), f.bot(t, "C")
	g, _ := f.st.CreateGroup("Team", "", []string{a.ID, b.ID, c.ID})
	f.team.Send(g.ID, "plan", []string{c.ID}, nil)
	f.team.Wait()
	got := strings.Join(f.texts(t, g.ID), " | ")
	// C was mentioned, so it goes first; B passed; round 2 is all passes.
	if got != "user: plan | C: C on plan | A: A on plan" {
		t.Fatalf("transcript: %s", got)
	}
	passes := 0
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.evs {
		if e.Kind == EventPass {
			passes++
		}
	}
	if passes != 2 { // B in round 1; in round 2 only C heard something new (A's reply)
		t.Fatalf("%d passes", passes)
	}
}

func TestGroupStopsAfterFourRounds(t *testing.T) {
	f := setup(t, script(func(bot string, msgs []agent.Message) agent.Message { return say(bot + " again") }))
	a, b := f.bot(t, "A"), f.bot(t, "B")
	g, _ := f.st.CreateGroup("Chatty", "", []string{a.ID, b.ID})
	f.team.Send(g.ID, "go", nil, nil)
	f.team.Wait()
	if n := len(f.texts(t, g.ID)); n != 1+2*MaxRounds {
		t.Fatalf("%d messages", n)
	}
}

func TestMessageBotHopLimit(t *testing.T) {
	var mu sync.Mutex
	refused := ""
	f := setup(t, nil)
	f.team.Model = func(store.Bot) (agent.Model, error) {
		return script(func(bot string, msgs []agent.Message) agent.Message {
			l := last(msgs)
			if msgs[len(msgs)-1].Role == agent.RoleTool {
				if strings.HasPrefix(l, "refused") {
					mu.Lock()
					refused = l
					mu.Unlock()
				}
				return say("ok")
			}
			other := map[string]string{"A": "B", "B": "A"}[bot]
			return call("message_bot", map[string]string{"bot": other, "text": "ping from " + bot})
		}), nil
	}
	a, b := f.bot(t, "A"), f.bot(t, "B")
	dm, _ := f.st.DMOf(a.ID)
	f.team.Send(dm.ID, "start", nil, nil)
	f.team.Wait()
	dmB, _ := f.st.DMOf(b.ID)
	esA, _ := f.st.Messages(dm.ID, 0)
	esB, _ := f.st.Messages(dmB.ID, 0)
	delivered := 0
	for _, e := range append(esA, esB...) {
		if e.Meta.From != "" {
			delivered++
		}
	}
	if delivered != MaxHops || !strings.Contains(refused, "8 bot-to-bot") {
		t.Fatalf("delivered %d, refused %q", delivered, refused)
	}
	// B reads A's message as one from A.
	bots, _ := f.st.Bots()
	if p := Project(esB, b.ID, bots); !strings.HasPrefix(last(p[:1]), "[Message from A (id "+a.ID) {
		t.Fatalf("projection: %q", last(p[:1]))
	}
}

func TestCreateBotAndHandOff(t *testing.T) {
	f := setup(t, script(func(bot string, msgs []agent.Message) agent.Message {
		if msgs[len(msgs)-1].Role == agent.RoleTool {
			return say("done")
		}
		if lastUser(msgs) == "hire someone" && bot == "A" {
			return call("create_bot", map[string]any{"name": "Scout", "description": "Finds things.", "join": true})
		}
		if lastUser(msgs) == "now" && bot == "A" {
			return call("hand_off", map[string]string{"bot": "Scout"})
		}
		return say("PASS")
	}))
	a := f.bot(t, "A")
	g, _ := f.st.CreateGroup("G", "", []string{a.ID})
	f.team.Send(g.ID, "hire someone", nil, nil)
	f.team.Wait()
	g, _ = f.st.Chat(g.ID)
	if len(g.Members) != 2 {
		t.Fatalf("members %v", g.Members)
	}
	scout, _ := f.st.Bot(g.Members[1])
	if scout.Name != "Scout" || scout.Description != "Finds things." {
		t.Fatalf("bot %+v", scout)
	}
	f.team.Send(g.ID, "now", nil, nil)
	f.team.Wait()
	if g, _ = f.st.Chat(g.ID); g.Owner != scout.ID {
		t.Fatalf("owner %s", g.Owner)
	}
}

func TestStopCancels(t *testing.T) {
	f := setup(t, nil)
	f.team.Model = func(store.Bot) (agent.Model, error) {
		return blocking{}, nil
	}
	a := f.bot(t, "A")
	dm, _ := f.st.DMOf(a.ID)
	f.team.Send(dm.ID, "x", nil, nil)
	for !f.team.Busy(dm.ID) {
		time.Sleep(time.Millisecond)
	}
	f.team.Stop(dm.ID)
	f.team.Wait()
	es, _ := f.st.Messages(dm.ID, 0)
	if len(es) != 2 || es[1].Msg.StopReason != agent.StopAborted {
		t.Fatalf("entries %+v", es)
	}
}

type blocking struct{}

func (blocking) Call(ctx context.Context, _ string, _ []agent.Message, _ []tool.Tool, _ func(agent.Event)) agent.Message {
	<-ctx.Done()
	return agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopAborted}
}

func TestProjectNamesSpeakersMentionsAndReplies(t *testing.T) {
	bots := []store.Bot{{ID: "bot-a", Name: "A"}, {ID: "bot-b", Name: "B"}}
	es := []store.Entry{
		{Author: store.User, Meta: store.Meta{Mentions: []string{"bot-b"}}, Msg: agent.Message{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: "hey @B look"}}}},
		{Author: "bot-b", Msg: agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopToolUse, Parts: []agent.Part{{Kind: agent.PartToolCall, Name: "x"}}}},
		{Author: "bot-b", Msg: agent.Message{Role: agent.RoleTool, Parts: []agent.Part{{Kind: agent.PartToolResult, Text: "r"}}}},
		{Author: "bot-b", Msg: agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopEnd, Parts: []agent.Part{{Kind: agent.PartText, Text: "found it"}}}},
		{Author: store.User, Meta: store.Meta{ReplyTo: &store.Quote{Author: "bot-a", Text: "mine"}}, Msg: agent.Message{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: "thanks"}}}},
	}
	p := Project(es, "bot-a", bots)
	if len(p) != 3 || last(p[:1]) != "hey @B (id bot-b) look" || last(p[1:2]) != "[B]: found it" ||
		last(p[2:]) != "[Replying to your message: \"mine\"]\nthanks" {
		t.Fatalf("projection: %+v", p)
	}
	if es[0].Msg.Parts[0].Text != "hey @B look" {
		t.Fatal("projection changed the entry")
	}
}

func TestSystemPromptGroup(t *testing.T) {
	bots := []store.Bot{{ID: "bot-a", Name: "A", Description: "Does A."}, {ID: "bot-b", Name: "B"}, {ID: "bot-c", Name: "C", Description: "Outside."}}
	chat := store.Chat{Kind: store.Group, Title: "G", Description: "launch", Owner: "bot-b", Members: []string{"bot-a", "bot-b"}}
	s := SystemPrompt(bots[0], chat, bots, "/w")
	for _, want := range []string{"Does A.", "You are A (id bot-a)", "- A (id bot-a) — you", "- B (id bot-b) — owner", "for: launch", "PASS", "- C (id bot-c): Outside."} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
}

func TestInterruptReadsSteeringAtOnce(t *testing.T) {
	entered := make(chan struct{}, 1)
	f := setup(t, nil)
	f.team.Model = func(store.Bot) (agent.Model, error) {
		return ctxScript(func(ctx context.Context, msgs []agent.Message) agent.Message {
			if last(msgs) == "long task" {
				entered <- struct{}{}
				<-ctx.Done()
				return agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopAborted, Parts: []agent.Part{{Kind: agent.PartText, Text: "working on it"}}}
			}
			return agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopEnd, Parts: []agent.Part{{Kind: agent.PartText, Text: "re: " + last(msgs)}}}
		}), nil
	}
	a := f.bot(t, "A")
	dm, _ := f.st.DMOf(a.ID)
	f.team.Send(dm.ID, "long task", nil, nil)
	<-entered
	f.team.Send(dm.ID, "never mind", nil, nil)
	if !f.team.Interrupt(dm.ID) {
		t.Fatal("nothing to interrupt")
	}
	f.team.Wait()
	if got := strings.Join(f.texts(t, dm.ID), " | "); got != "user: long task | A: working on it | user: never mind | A: re: never mind" {
		t.Fatalf("transcript: %s", got)
	}
}

type ctxScript func(ctx context.Context, msgs []agent.Message) agent.Message

func (f ctxScript) Call(ctx context.Context, _ string, msgs []agent.Message, _ []tool.Tool, _ func(agent.Event)) agent.Message {
	return f(ctx, msgs)
}
