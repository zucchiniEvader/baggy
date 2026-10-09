package team

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/memory"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/tool"
)

// summarizing answers a summary request with "SUMMARY" and a flush with a
// memory_update, then DONE; anything else with reply.
type summarizing struct {
	reply   func(msgs []agent.Message) agent.Message
	systems []string
}

func (s *summarizing) Call(ctx context.Context, system string, msgs []agent.Message, tools []tool.Tool, emit func(agent.Event)) agent.Message {
	s.systems = append(s.systems, system)
	l := last(msgs)
	switch {
	case system == summarySystem:
		return agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopEnd, Parts: []agent.Part{{Kind: agent.PartText, Text: "SUMMARY"}}}
	case strings.HasPrefix(l, "[Context is about to be compacted"):
		return agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopToolUse, Parts: []agent.Part{
			{Kind: agent.PartToolCall, CallID: "m1", Name: "memory_update", Args: []byte(`{"op":"append","text":"the user likes tea"}`)},
			{Kind: agent.PartToolCall, CallID: "m2", Name: "shell", Args: []byte(`{}`)},
		}}
	case msgs[len(msgs)-1].Role == agent.RoleTool:
		return agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopEnd, Parts: []agent.Part{{Kind: agent.PartText, Text: "DONE"}}}
	}
	m := s.reply(msgs)
	m.Role = agent.RoleAssistant
	m.StopReason = agent.StopEnd
	return m
}

func TestCompactionSummarizesAndKeeps(t *testing.T) {
	model := &summarizing{reply: func(msgs []agent.Message) agent.Message { return say("ok " + strings.Repeat("x", 400)) }}
	f := setup(t, model)
	f.team.Memory = true
	f.team.Compaction = &Compaction{Window: func(string) int { return 4000 }, Threshold: 0.5, Keep: 0.1}
	chef := f.bot(t, "Chef")
	dm, _ := f.st.DMOf(chef.ID)
	for i := 0; i < 8; i++ {
		f.team.Send(dm.ID, strings.Repeat("words ", 120), nil, nil)
		f.team.Wait()
	}
	es, _ := f.st.Messages(dm.ID, 0)
	var comp *store.Entry
	for i := range es {
		if es[i].Msg.Kind == KindCompaction {
			comp = &es[i]
		}
	}
	if comp == nil || comp.Meta.KeepID == "" || !strings.Contains(textOf(comp.Msg), "SUMMARY") {
		t.Fatalf("no compaction among %d entries", len(es))
	}
	bots, _ := f.st.Bots()
	p := Project(es, chef.ID, bots)
	if p[0].Kind != KindCompaction || p[1].ID != comp.Meta.KeepID || len(p) >= len(es) {
		t.Fatalf("projection: first %q, %d of %d", p[0].Kind, len(p), len(es))
	}
	// The flush saved a fact, and only memory tools ran during it.
	mem := memory.New(f.st.Workspace(chef.ID)).Read()
	if !strings.Contains(mem, "the user likes tea") {
		t.Fatalf("MEMORY.md: %q", mem)
	}
	// The memory is in the next turn's system prompt.
	f.team.Send(dm.ID, "hi", nil, nil)
	f.team.Wait()
	if s := model.systems[len(model.systems)-1]; !strings.Contains(s, "the user likes tea") || !strings.Contains(s, "## Your memory") {
		t.Fatalf("system prompt lacks memory:\n%s", s)
	}
}

func TestCutPointKeepsCallsWithResults(t *testing.T) {
	big := strings.Repeat("y", 400)
	msgs := []agent.Message{
		{ID: "u1", Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: big}}},
		{Role: agent.RoleAssistant, Parts: []agent.Part{{Kind: agent.PartToolCall, Name: "x"}}},
		{Role: agent.RoleTool, Parts: []agent.Part{{Kind: agent.PartToolResult, Text: big}}},
		{ID: "u2", Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: big}}},
		{Role: agent.RoleAssistant, Parts: []agent.Part{{Kind: agent.PartText, Text: big}}},
	}
	if k := cutPoint(msgs, 150); k != 3 {
		t.Fatalf("cut at %d", k)
	}
	if k := cutPoint(msgs[:1], 10); k != 0 {
		t.Fatalf("cut a lone message at %d", k)
	}
}

func TestRecallBriefAndDiary(t *testing.T) {
	now := time.Now()
	f := setup(t, script(func(bot string, msgs []agent.Message) agent.Message {
		if strings.Contains(lastUser(msgs), "hotel") {
			return say("Booked the Kyoto hotel in Gion.")
		}
		return say("noted")
	}))
	f.team.Memory = true
	f.team.Now = func() time.Time { return now }
	chef := f.bot(t, "Chef")
	scout := f.bot(t, "Scout")
	dm, _ := f.st.DMOf(chef.ID)
	f.team.Send(dm.ID, "book the hotel", nil, nil)
	f.team.Wait()
	g, _ := f.st.CreateGroup("Trip", "", []string{chef.ID, scout.ID})

	brief := f.team.brief(chef, g)
	if !strings.Contains(brief, "today") || !strings.Contains(brief, "your chat with the user") || !strings.Contains(brief, "Booked the Kyoto hotel") {
		t.Fatalf("brief: %q", brief)
	}
	if f.team.brief(chef, dm) != "" {
		t.Fatal("the brief names the chat it is for")
	}
	got := f.team.recall(chef, "kyoto hotel", 0)
	if !strings.Contains(got, "you: Booked the Kyoto hotel") || !strings.Contains(got, "memory/log/") {
		t.Fatalf("recall: %s", got)
	}
	if !strings.Contains(f.team.recall(chef, "kyoto", 1), "Kyoto") {
		t.Fatal("recall with days")
	}
}
