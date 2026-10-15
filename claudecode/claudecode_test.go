package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// The test binary stands in for claude when FAKE_CLAUDE is set: it speaks
// the stream-json protocol as captured from Claude Code 2.1.
func TestMain(m *testing.M) {
	if mode := os.Getenv("FAKE_CLAUDE"); mode != "" {
		fakeClaude(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeClaude(mode string) {
	args := strings.Join(os.Args[1:], " ")
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(nil, 1<<20)
	out := json.NewEncoder(os.Stdout)
	read := func() map[string]any {
		if !in.Scan() {
			os.Exit(0)
		}
		var v map[string]any
		json.Unmarshal(in.Bytes(), &v)
		return v
	}
	if strings.Contains(args, "--resume gone") {
		fmt.Fprintln(os.Stderr, "No conversation found with session ID: gone")
		out.Encode(map[string]any{"type": "result", "subtype": "error_during_execution", "is_error": true, "num_turns": 0, "session_id": "gone"})
		os.Exit(1)
	}
	if !strings.Contains(args, "--permission-prompt-tool stdio") || !strings.Contains(args, "--permission-mode acceptEdits") {
		fmt.Fprintln(os.Stderr, "missing flags: "+args)
		os.Exit(2)
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		fmt.Fprintln(os.Stderr, "an API key leaked in")
		os.Exit(2)
	}
	init := read()
	out.Encode(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": init["request_id"],
		"response": map[string]any{"account": map[string]any{"subscriptionType": "Claude Pro"}}}})
	user := read()
	task := user["message"].(map[string]any)["content"].(string)
	session := "s-new"
	if i := strings.Index(args, "--resume "); i >= 0 {
		session = strings.Fields(args[i+len("--resume "):])[0]
	}
	out.Encode(map[string]any{"type": "system", "subtype": "init", "session_id": session, "apiKeySource": "none"})
	cwd, _ := os.Getwd()
	switch mode {
	case "hang":
		out.Encode(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": map[string]any{"command": "sleep 100"}}}}})
		time.Sleep(time.Minute)
		return
	case "crash":
		fmt.Fprintln(os.Stderr, "Error: something broke")
		os.Exit(3)
	}
	out.Encode(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{
		map[string]any{"type": "text", "text": "Looking."},
		map[string]any{"type": "tool_use", "id": "t1", "name": "Edit", "input": map[string]any{"file_path": cwd + "/main.go"}},
	}}})
	out.Encode(map[string]any{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "ok"}}}})
	out.Encode(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{
		map[string]any{"type": "tool_use", "id": "t2", "name": "Bash", "input": map[string]any{"command": "rm -rf build"}},
	}}})
	out.Encode(map[string]any{"type": "control_request", "request_id": "q1", "request": map[string]any{"subtype": "can_use_tool", "tool_name": "Bash", "input": map[string]any{"command": "rm -rf build"}, "tool_use_id": "t2"}})
	var answer map[string]any
	for answer == nil {
		v := read()
		if v["type"] == "control_response" {
			answer = v["response"].(map[string]any)["response"].(map[string]any)
		}
	}
	failed := answer["behavior"] != "allow"
	out.Encode(map[string]any{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "t2", "content": fmt.Sprint(answer["message"]), "is_error": failed}}}})
	out.Encode(map[string]any{"type": "control_request", "request_id": "q2", "request": map[string]any{"subtype": "can_use_tool", "tool_name": "AskUserQuestion", "input": map[string]any{}}})
	for {
		v := read()
		if v["type"] == "control_response" {
			msg := v["response"].(map[string]any)["response"].(map[string]any)["message"]
			out.Encode(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": fmt.Sprint(msg)}}}})
			break
		}
	}
	out.Encode(map[string]any{"type": "result", "subtype": "success", "is_error": false, "num_turns": 3, "session_id": session,
		"result": fmt.Sprintf("Did %q; rm allowed: %v", task, !failed)})
	read() // until stdin closes
}

func fake(t *testing.T, mode string) *Manager {
	t.Setenv("FAKE_CLAUDE", mode)
	t.Setenv("ANTHROPIC_API_KEY", "sk-should-not-pass")
	m := NewManager()
	m.Command = os.Args[0]
	return m
}

func TestRunApprovesAndReports(t *testing.T) {
	m := fake(t, "ok")
	dir := t.TempDir()
	var mu sync.Mutex
	var asked []Request
	ended := make(chan *Run, 1)
	r, err := m.Start(Options{Chat: "c", Bot: "b", Dir: dir, Task: "fix the bug",
		Approve: func(ctx context.Context, req Request) (bool, string) {
			mu.Lock()
			asked = append(asked, req)
			mu.Unlock()
			return false, "not that"
		},
		OnEnd: func(r *Run) { ended <- r }})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not end")
	}
	done, how := r.Done()
	if !done || how != "finished" {
		t.Fatalf("done %v, %q; stderr %s", done, how, r.stderr)
	}
	if r.Session() != "s-new" || r.Account() != "Claude Pro" {
		t.Errorf("session %q account %q", r.Session(), r.Account())
	}
	if got := r.Result(); got != `Did "fix the bug"; rm allowed: false` {
		t.Errorf("result %q", got)
	}
	if len(asked) != 1 || asked[0].Tool != "Bash" || Describe(asked[0]) != "Bash: rm -rf build" {
		t.Errorf("asked %+v (AskUserQuestion is answered without asking)", asked)
	}
	if !strings.Contains(r.Text(), "Nobody can answer") {
		t.Errorf("a question should be refused with a note: %q", r.Text())
	}
	if p := r.Progress(5); p != "✓ Edit main.go\n✗ Bash rm -rf build" {
		t.Errorf("progress %q", p)
	}
}

func TestResumeGoneStartsOver(t *testing.T) {
	m := fake(t, "ok")
	r, err := m.Start(Options{Chat: "c", Bot: "b", Dir: t.TempDir(), Task: "go on", Resume: "gone"})
	if err != nil {
		t.Fatal(err)
	}
	r.Wait(context.Background(), 10*time.Second)
	if _, how := r.Done(); how != "finished" || !r.Fresh() || r.Session() != "s-new" {
		t.Fatalf("how %q fresh %v session %q", how, r.Fresh(), r.Session())
	}
	r2, _ := m.Start(Options{Chat: "c", Bot: "b", Dir: t.TempDir(), Task: "more", Resume: "s-old"})
	r2.Wait(context.Background(), 10*time.Second)
	if r2.Session() != "s-old" || r2.Fresh() {
		t.Errorf("a resume should keep its session: %q", r2.Session())
	}
}

func TestStopLimitsAndFailure(t *testing.T) {
	m := fake(t, "hang")
	dir := t.TempDir()
	stopped := make(chan bool, 1)
	r, err := m.Start(Options{Chat: "c", Bot: "b", Dir: dir, Task: "long", OnEnd: func(r *Run) { stopped <- r.Stopped() }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(Options{Chat: "c", Bot: "b", Dir: dir, Task: "again"}); err == nil || !strings.Contains(err.Error(), "still working on run "+r.ID) {
		t.Errorf("a second run of the same bot in the chat: %v", err)
	}
	r2, err := m.Start(Options{Chat: "c2", Bot: "b", Dir: dir, Task: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(Options{Chat: "c3", Bot: "b", Dir: dir, Task: "third"}); err == nil {
		t.Error("a third run at once should be refused")
	}
	for len(r.Steps()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	start := time.Now()
	r.Stop("by the test")
	if !<-stopped || time.Since(start) > 4*time.Second {
		t.Errorf("stop took %v", time.Since(start))
	}
	if _, how := r.Done(); how != "stopped: by the test" {
		t.Errorf("how %q", how)
	}
	if got := m.Latest("c", "b"); got != r {
		t.Error("Latest should find the run")
	}
	m.StopAll()
	if len(m.Running("")) != 0 || !r2.Stopped() {
		t.Error("StopAll should end every run")
	}

	t.Setenv("FAKE_CLAUDE", "crash")
	r3, _ := m.Start(Options{Chat: "c", Bot: "b", Dir: dir, Task: "x"})
	r3.Wait(context.Background(), 10*time.Second)
	if _, how := r3.Done(); !strings.HasPrefix(how, "failed: ") || !strings.Contains(how, "something broke") {
		t.Errorf("a crash: %q", how)
	}
	if _, err := m.Start(Options{Chat: "c", Bot: "b", Dir: dir + "/nope", Task: "x"}); err == nil {
		t.Error("a missing folder should be refused")
	}
}

func TestFindAndEnv(t *testing.T) {
	dir := t.TempDir()
	if _, err := Find([]string{"PATH=" + dir}); err == nil {
		t.Error("no claude there")
	}
	os.WriteFile(dir+"/claude", []byte("#!/bin/sh\n"), 0o755)
	if p, err := Find([]string{"PATH=/nowhere:" + dir}); err != nil || p != dir+"/claude" {
		t.Errorf("Find = %q, %v", p, err)
	}
	m := NewManager()
	m.Env = func() []string { return []string{"PATH=/bin", "ANTHROPIC_API_KEY=x", "CLAUDECODE=1", "HOME=/h"} }
	if got := strings.Join(m.env(), " "); got != "PATH=/bin HOME=/h" {
		t.Errorf("env %q", got)
	}
}

// TestRealClaude runs the claude on this computer once, on its cheapest
// model: GOBOT_CLAUDE_E2E=1 go test ./claudecode -run Real
func TestRealClaude(t *testing.T) {
	if os.Getenv("GOBOT_CLAUDE_E2E") == "" {
		t.Skip("set GOBOT_CLAUDE_E2E=1 to run the real claude")
	}
	m := NewManager()
	dir := t.TempDir()
	os.WriteFile(dir+"/a.txt", []byte("hi\n"), 0o644)
	var asked []string
	r, err := m.Start(Options{Chat: "c", Bot: "b", Dir: dir, Task: "Create b.txt containing the word hello. Then delete a.txt with the Bash tool (rm a.txt). Reply with one short line.",
		Approve: func(ctx context.Context, req Request) (bool, string) {
			asked = append(asked, Describe(req))
			return true, ""
		}})
	if err != nil {
		t.Fatal(err)
	}
	r.Wait(context.Background(), 3*time.Minute)
	_, how := r.Done()
	t.Logf("how %q account %q session %q asked %q\n%s\nresult: %s", how, r.Account(), r.Session(), asked, r.Progress(10), r.Result())
	if how != "finished" || r.Session() == "" {
		t.Fatal("the run should finish with a session")
	}
	if b, _ := os.ReadFile(dir + "/b.txt"); !strings.Contains(string(b), "hello") {
		t.Error("b.txt was not written (acceptEdits)")
	}
	if _, err := os.Stat(dir + "/a.txt"); err == nil {
		t.Error("a.txt should be gone")
	}
}
