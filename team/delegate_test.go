package team

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/tool"
)

// stepFunc is a model from a function of the system prompt and messages;
// a reply keeps its stop reason (a tool call's).
type stepFunc func(system string, msgs []agent.Message) agent.Message

func (m stepFunc) Call(ctx context.Context, system string, msgs []agent.Message, _ []tool.Tool, _ func(agent.Event)) agent.Message {
	out := m(system, msgs)
	out.Role = agent.RoleAssistant
	if out.StopReason == "" {
		out.StopReason = agent.StopEnd
	}
	return out
}

func TestDelegateReportsTogether(t *testing.T) {
	var mu sync.Mutex
	var briefs []string // each worker: who it is, its brief, how many messages it read
	f := setup(t, stepFunc(func(system string, msgs []agent.Message) agent.Message {
		if strings.Contains(system, "handed you") {
			mu.Lock()
			briefs = append(briefs, whoRe.FindStringSubmatch(system)[1]+":"+last(msgs)+":"+string(rune('0'+len(msgs))))
			mu.Unlock()
			return say("found " + last(msgs))
		}
		l := last(msgs)
		switch {
		case msgs[len(msgs)-1].Role == agent.RoleTool:
			return say("on it")
		case strings.HasPrefix(l, "[Delegation"):
			return say("summary: " + strings.ReplaceAll(l, "\n", " "))
		}
		return call("delegate", map[string]any{"tasks": []map[string]string{
			{"title": "Prices", "brief": "look up prices"},
			{"title": "Reviews", "brief": "read reviews", "bot": "B"},
		}})
	}))
	a := f.bot(t, "A")
	f.bot(t, "B")
	dm, _ := f.st.DMOf(a.ID)
	f.team.Send(dm.ID, "compare the two", nil, nil)
	f.team.Wait()
	got := f.texts(t, dm.ID)
	end := got[len(got)-1]
	if !strings.HasPrefix(end, "A: summary: [Delegation") || !strings.Contains(end, "2 of 2 task(s) done") ||
		!strings.Contains(end, "found look up prices") || !strings.Contains(end, "found read reviews") {
		t.Fatalf("transcript: %q", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(briefs) != 2 || !contains(briefs, "A:look up prices:1") || !contains(briefs, "B:read reviews:1") {
		t.Fatalf("workers saw %q", briefs)
	}
	if len(f.team.Delegations("")) != 0 {
		t.Fatal("a delegation still under way")
	}
}

func TestMessageBotAnswersBack(t *testing.T) {
	f := setup(t, stepFunc(func(system string, msgs []agent.Message) agent.Message {
		bot, l := whoRe.FindStringSubmatch(system)[1], last(msgs)
		switch {
		case bot == "B":
			return say("42")
		case msgs[len(msgs)-1].Role == agent.RoleTool:
			return say("asked B")
		case strings.HasPrefix(l, "[Reply from B"):
			return say("B says " + l[strings.LastIndexByte(l, '\n')+1:])
		}
		return call("message_bot", map[string]string{"bot": "B", "text": "the answer?"})
	}))
	a, b := f.bot(t, "A"), f.bot(t, "B")
	dm, _ := f.st.DMOf(a.ID)
	f.team.Send(dm.ID, "ask B", nil, nil)
	f.team.Wait()
	got := f.texts(t, dm.ID)
	if got[len(got)-1] != "A: B says 42" {
		t.Fatalf("A's chat: %q", got)
	}
	dmB, _ := f.st.DMOf(b.ID)
	if gotB := strings.Join(f.texts(t, dmB.ID), " | "); gotB != "A: the answer? | B: 42" {
		t.Fatalf("B's chat: %s", gotB)
	}
}

func TestRoutingPicksWhoAnswers(t *testing.T) {
	var mu sync.Mutex
	pick := `{"bots": ["C"]}`
	routed := 0
	f := setup(t, stepFunc(func(system string, msgs []agent.Message) agent.Message {
		if system == routeSystem {
			mu.Lock()
			defer mu.Unlock()
			routed++
			return say(pick)
		}
		return replyOnce(whoRe.FindStringSubmatch(system)[1], msgs)
	}))
	f.team.Routing = true
	a, b, c := f.bot(t, "A"), f.bot(t, "B"), f.bot(t, "C")
	g, _ := f.st.CreateGroup("Team", "", []string{a.ID, b.ID, c.ID})
	f.team.Send(g.ID, "plan", nil, nil)
	f.team.Wait()
	// Only C is offered a turn; in round 2 the others heard C, and the
	// router picks C again, which has nothing new: the room ends.
	if got := strings.Join(f.texts(t, g.ID), " | "); got != "user: plan | C: C on plan" {
		t.Fatalf("routed: %s", got)
	}
	// A mention goes first whatever the router says; then the rest are routed.
	f.team.Send(g.ID, "you first", []string{a.ID}, nil)
	f.team.Wait()
	if got := strings.Join(f.texts(t, g.ID), " | "); !strings.HasSuffix(got, "user: you first | A: A on you first") {
		t.Fatalf("mentioned: %s", got)
	}
	// A router that does not answer offers everyone, as without routing.
	mu.Lock()
	pick = "not json"
	mu.Unlock()
	g2, _ := f.st.CreateGroup("Two", "", []string{a.ID, b.ID, c.ID})
	f.team.Send(g2.ID, "plan", nil, nil)
	f.team.Wait()
	if got := strings.Join(f.texts(t, g2.ID), " | "); got != "user: plan | A: A on plan | C: C on plan" {
		t.Fatalf("fallback: %s", got)
	}
	if routed == 0 {
		t.Fatal("the router was never asked")
	}
}
