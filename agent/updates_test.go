package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/spike/gobot/tool"
)

// chatty reports partial results as it works.
type chatty struct{}

func (chatty) Name() string            { return "echo" }
func (chatty) Description() string     { return "works a while" }
func (chatty) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (chatty) Execute(ctx context.Context, _ json.RawMessage) tool.Result {
	for _, s := range []string{"a", "ab", "abc"} { // quicker than updateEvery: only the newest goes
		tool.Update(ctx, tool.Result{Text: s})
	}
	time.Sleep(updateEvery + 100*time.Millisecond)
	tool.Update(ctx, tool.Result{Text: "abcd"})
	return tool.Result{Text: "abcde"} // ends before abcd's turn: it never shows
}

func TestToolUpdates(t *testing.T) {
	var mu sync.Mutex
	var got []string
	ended := false
	m := &seen{replies: []Message{ask("c1"), say("done")}}
	Run(context.Background(), m, []Message{userText("go")}, Options{
		Tools: []tool.Tool{chatty{}},
		Emit: func(e Event) {
			mu.Lock()
			defer mu.Unlock()
			switch e.Kind {
			case EventToolUpdate:
				if ended || e.Call.CallID != "c1" {
					t.Errorf("an update after the end, or of another call: %+v", e)
				}
				got = append(got, e.Result.Text)
			case EventToolEnd:
				ended = true
			}
		},
	})
	time.Sleep(updateEvery)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0] != "a" || got[1] != "abc" {
		t.Fatalf("updates %q", got)
	}
}
