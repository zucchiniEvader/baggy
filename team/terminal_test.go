//go:build darwin || linux

package team

import (
	"strings"
	"testing"
	"time"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/terminal"
)

func TestBackgroundCommandWakesItsBot(t *testing.T) {
	woke := make(chan string, 1)
	f := setup(t, script(func(bot string, msgs []agent.Message) agent.Message {
		l := last(msgs)
		switch {
		case strings.HasPrefix(l, "[Your background command"):
			woke <- l
			return say("The build finished.")
		case msgs[len(msgs)-1].Role == agent.RoleTool:
			return say("Started it.")
		}
		return call("shell", map[string]any{"command": "sleep 1; echo built ok", "background": true})
	}))
	f.team.Terminals = terminal.NewManager()
	defer f.team.Terminals.StopAll()
	f.team.WakeOnExit()
	a := f.bot(t, "A")
	dm, _ := f.st.DMOf(a.ID)
	f.team.Send(dm.ID, "build it", nil, nil)
	select {
	case note := <-woke:
		if !strings.Contains(note, "built ok") || !strings.Contains(note, "exited") {
			t.Fatalf("note %q", note)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("not woken")
	}
	f.team.Wait()
	got := f.texts(t, dm.ID)
	if got[len(got)-1] != "A: The build finished." {
		t.Fatalf("transcript %q", got)
	}
	var sys store.Entry
	es, _ := f.st.Messages(dm.ID, 0)
	for _, e := range es {
		if e.Author == store.System {
			sys = e
		}
	}
	if sys.Seq == 0 {
		t.Fatal("no system note stored")
	}
}
