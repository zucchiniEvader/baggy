//go:build darwin || linux

package terminal

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spike/gobot/tool"
)

func call(t *testing.T, tl tool.Tool, args any) tool.Result {
	t.Helper()
	b, _ := json.Marshal(args)
	return tl.Execute(context.Background(), b)
}

func tools(t *testing.T) (*Manager, tool.Tool, tool.Tool, tool.Tool) {
	m := NewManager()
	t.Cleanup(m.StopAll)
	ts := m.Tools("chat", "bot", t.TempDir())
	return m, ts[0], ts[1], ts[2]
}

func sessionOf(t *testing.T, r tool.Result) string {
	t.Helper()
	i := strings.Index(r.Text, "[session ")
	if i < 0 {
		t.Fatalf("no session in %q", r.Text)
	}
	return strings.TrimSuffix(strings.Fields(r.Text[i+len("[session "):])[0], "]")
}

func TestCommandExitsWithItsOutput(t *testing.T) {
	_, sh, _, _ := tools(t)
	r := call(t, sh, map[string]any{"command": "echo hello; printf 'a\\rb\\n'; tput setaf 1 2>/dev/null; echo red"})
	if r.IsError || !strings.Contains(r.Text, "hello\nb\n") || strings.Contains(r.Text, "\x1b") || !strings.HasSuffix(r.Text, "The command exited.") {
		t.Fatalf("%+v", r)
	}
	if r := call(t, sh, map[string]any{"command": "exit 3"}); !r.IsError || !strings.Contains(r.Text, "exit code 3") {
		t.Fatalf("%+v", r)
	}
}

func TestQuestionAnsweredWithInput(t *testing.T) {
	_, sh, in, _ := tools(t)
	start := time.Now()
	r := call(t, sh, map[string]any{"command": `printf 'Proceed? [y/N] '; read a; echo "got $a"`})
	if strings.Contains(r.Text, "exited") || !strings.Contains(r.Text, "waits at a question") || time.Since(start) > 10*time.Second {
		t.Fatalf("question: %+v", r)
	}
	id := sessionOf(t, r)
	r = call(t, in, map[string]any{"session_id": id, "text": "y"})
	if !strings.Contains(r.Text, "got y") || strings.Count(r.Text, "y") > 2 || !strings.Contains(r.Text, "exited") {
		t.Fatalf("answer: %+v", r) // the typed y does not echo back
	}
}

func TestBackgroundCommandRunsOnAndWakes(t *testing.T) {
	m, sh, in, out := tools(t)
	woke := make(chan *Session, 1)
	m.OnExit = func(s *Session) { woke <- s }
	r := call(t, sh, map[string]any{"command": "echo listening on 8080; sleep 1; echo tick; sleep 1; echo done", "background": true})
	if !strings.Contains(r.Text, "listening on 8080") || !strings.Contains(r.Text, "runs in the background") {
		t.Fatalf("background: %+v", r)
	}
	id := sessionOf(t, r)
	r = call(t, out, map[string]any{"session_id": id, "wait_seconds": 10})
	if !strings.Contains(r.Text, "tick") && !strings.Contains(r.Text, "done") {
		t.Fatalf("output: %+v", r)
	}
	select {
	case s := <-woke:
		if s.ID != id {
			t.Fatal(s.ID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no wake")
	}
	r = call(t, sh, map[string]any{"command": "sleep 100", "background": true})
	id = sessionOf(t, r)
	m.StopChat("chat") // Stop leaves background commands running
	if s, _ := m.Get(id); func() bool { d, _ := s.Done(); return d }() {
		t.Fatal("Stop ended a background command")
	}
	r = call(t, in, map[string]any{"session_id": id, "text": `\u0003`})
	if !strings.Contains(r.Text, "exit") && !strings.Contains(r.Text, "failed") {
		t.Fatalf("ctrl-c: %+v", r)
	}
}

func TestStopCancelsTheCall(t *testing.T) {
	m := NewManager()
	defer m.StopAll()
	sh := m.Tools("c", "b", t.TempDir())[0]
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	b, _ := json.Marshal(map[string]any{"command": "sleep 100"})
	r := sh.Execute(ctx, b)
	if !r.IsError || len(m.Running("c")) != 0 {
		t.Fatalf("%+v running %d", r, len(m.Running("c")))
	}
}

func TestSanitize(t *testing.T) {
	in := "\x1b]0;title\x07\x1b[1;31mred\x1b[0m\r\nprogress 10%\rprogress 99%\nab\bc\x00\n"
	if got := Sanitize(in); got != "red\nprogress 99%\nac\n" {
		t.Fatalf("%q", got)
	}
}
