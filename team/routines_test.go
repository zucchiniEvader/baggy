package team

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/tool"
)

func clock(at time.Time) func() time.Time { return func() time.Time { return at } }

func TestDueRoutineRunsOnceAndPassLeavesTheMarker(t *testing.T) {
	var seen []string
	f := setup(t, script(func(bot string, msgs []agent.Message) agent.Message {
		seen = append(seen, last(msgs))
		return say("PASS")
	}))
	now := time.Date(2026, 10, 9, 9, 0, 30, 0, time.UTC)
	f.team.Now = clock(now)
	a := f.bot(t, "A")
	r, _ := f.st.SaveRoutine(store.Routine{BotID: a.ID, Name: "Brief", Prompt: "Check the news", Schedule: "0 9 * * *", Timezone: "UTC", Enabled: true,
		Created: now.Add(-time.Hour).UnixMilli(), EnabledAt: now.Add(-time.Hour).UnixMilli()})
	f.team.TickRoutines()
	f.team.Wait()
	f.team.TickRoutines() // the same due time: taken already
	f.team.Wait()
	if len(seen) != 1 || !strings.Contains(seen[0], `Routine "Brief" is running`) || !strings.Contains(seen[0], "Task: Check the news") {
		t.Fatalf("turns saw %q", seen)
	}
	dm, _ := f.st.DMOf(a.ID)
	es, _ := f.st.Messages(dm.ID, 0)
	if len(es) != 1 || es[0].Meta.Routine != r.ID || es[0].Author != store.System {
		t.Fatalf("entries %+v", es)
	}
	got, _ := f.st.Routine(r.ID)
	if got.LastOutcome != store.OutcomePass || got.LastRunAt == 0 {
		t.Fatalf("routine %+v", got)
	}
}

func TestMissedRunsCoalesceOrSkip(t *testing.T) {
	runs := 0
	f := setup(t, script(func(string, []agent.Message) agent.Message { runs++; return say("done") }))
	now := time.Date(2026, 10, 9, 12, 0, 30, 0, time.UTC)
	f.team.Now = clock(now)
	a, b := f.bot(t, "A"), f.bot(t, "B")
	start := now.Add(-5 * time.Hour).UnixMilli() // five hourly runs missed
	f.st.SaveRoutine(store.Routine{BotID: a.ID, Name: "c", Prompt: "p", Schedule: "every 1h", Enabled: true, Created: start, EnabledAt: start})
	skip, _ := f.st.SaveRoutine(store.Routine{BotID: b.ID, Name: "s", Prompt: "p", Schedule: "0 * * * *", Timezone: "UTC", Missed: store.Skip, Enabled: true, Created: start, EnabledAt: start})
	f.team.TickRoutines()
	f.team.Wait()
	if runs != 2 { // A's five missed runs coalesce into one; B's 12:00 is 30 s late, within a minute, so it runs
		t.Fatalf("runs %d", runs)
	}
	got, _ := f.st.Routine(skip.ID)
	if got.LastScheduledAt != time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("skip took %v", time.UnixMilli(got.LastScheduledAt))
	}

	// Two minutes late with skip: no run.
	runs = 0
	f.team.Now = clock(now.Add(62 * time.Minute))
	f.team.TickRoutines()
	f.team.Wait()
	dmB, _ := f.st.DMOf(b.ID)
	esB, _ := f.st.Messages(dmB.ID, 0)
	if n := len(esB); n != 2 { // the 12:00 run and its reply only
		t.Fatalf("skip ran late: %d entries", n)
	}
}

func TestAwayPausesRoutines(t *testing.T) {
	f := setup(t, script(func(string, []agent.Message) agent.Message { return say("x") }))
	a := f.bot(t, "A")
	dm, _ := f.st.DMOf(a.ID)
	f.team.Send(dm.ID, "hi", nil, nil)
	f.team.Wait()
	now := time.Now().Add(8 * 24 * time.Hour)
	f.team.Now = clock(now)
	r, _ := f.st.SaveRoutine(store.Routine{BotID: a.ID, Name: "n", Prompt: "p", Schedule: "every 1h", Enabled: true, Created: now.Add(-2 * time.Hour).UnixMilli(), EnabledAt: now.Add(-2 * time.Hour).UnixMilli()})
	f.team.TickRoutines()
	f.team.Wait()
	got, _ := f.st.Routine(r.ID)
	es, _ := f.st.Messages(dm.ID, 0)
	if got.Enabled || got.PausedReason != "away" || es[len(es)-1].Msg.Kind != KindNotice {
		t.Fatalf("routine %+v last %+v", got, es[len(es)-1])
	}
	bots, _ := f.st.Bots()
	for _, m := range Project(es, a.ID, bots) {
		if strings.Contains(textOf(m), "Paused the routine") {
			t.Fatal("the bot reads the notice")
		}
	}
}

func TestChecks(t *testing.T) {
	var seen []string
	f := setup(t, script(func(bot string, msgs []agent.Message) agent.Message {
		seen = append(seen, last(msgs))
		return say("Two new issues.")
	}))
	found := ""
	f.team.Checks = func(ctx context.Context, r store.Routine) (string, error) { return found, nil }
	now := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	f.team.Now = clock(now)
	a := f.bot(t, "A")
	r, _ := f.st.SaveRoutine(store.Routine{BotID: a.ID, Name: "Issues", Prompt: "Report new issues", Schedule: "every 1h", Check: "return []", Enabled: true,
		Created: now.Add(-61 * time.Minute).UnixMilli(), EnabledAt: now.Add(-61 * time.Minute).UnixMilli()})
	f.team.TickRoutines()
	f.team.Wait()
	if got, _ := f.st.Routine(r.ID); len(seen) != 0 || got.LastCheck != "quiet" {
		t.Fatalf("quiet check ran a turn: %q %+v", seen, got)
	}
	found = "#12 crash\n#14 typo"
	f.team.Now = clock(now.Add(time.Hour))
	f.team.TickRoutines()
	f.team.Wait()
	if len(seen) != 1 || !strings.Contains(seen[0], "check found") || !strings.Contains(seen[0], "#14 typo") {
		t.Fatalf("ready check: %q", seen)
	}
}

func TestRoutinesTool(t *testing.T) {
	var system string
	f := setup(t, nil)
	f.team.Model = func(store.Bot) (agent.Model, error) {
		return sysScript(func(sys string, msgs []agent.Message) agent.Message {
			system = sys
			if msgs[len(msgs)-1].Role == agent.RoleTool {
				return say(msgs[len(msgs)-1].Parts[0].Text)
			}
			if last(msgs) == "bad" {
				return call("routines", map[string]any{"action": "create", "name": "x", "prompt": "y", "schedule": "every 1m"})
			}
			return call("routines", map[string]any{"action": "create", "name": "Brief", "prompt": "Summarize my calendar", "schedule": "0 8 * * 1-5", "timezone": "Asia/Shanghai"})
		}), nil
	}
	a := f.bot(t, "A")
	dm, _ := f.st.DMOf(a.ID)
	f.team.Send(dm.ID, "every weekday at 8, brief me", nil, nil)
	f.team.Wait()
	rs, _ := f.st.Routines(a.ID)
	if len(rs) != 1 || rs[0].Timezone != "Asia/Shanghai" {
		t.Fatalf("routines %+v", rs)
	}
	f.team.Send(dm.ID, "bad", nil, nil)
	f.team.Wait()
	if !strings.Contains(system, `"Brief" (id `) || !strings.Contains(system, "Weekdays at 8:00 (Asia/Shanghai)") {
		t.Fatalf("system prompt:\n%s", system)
	}
	if got := f.texts(t, dm.ID); !strings.Contains(got[len(got)-1], "too often") {
		t.Fatalf("bad schedule: %q", got)
	}
}

type sysScript func(system string, msgs []agent.Message) agent.Message

func (f sysScript) Call(_ context.Context, system string, msgs []agent.Message, _ []tool.Tool, _ func(agent.Event)) agent.Message {
	m := f(system, msgs)
	m.Role = agent.RoleAssistant
	if m.StopReason == "" {
		m.StopReason = agent.StopEnd
	}
	return m
}
