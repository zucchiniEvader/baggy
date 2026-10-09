package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type host struct {
	running, peak atomic.Int32
	slow          time.Duration
}

func (h *host) Tools() []ToolInfo {
	return []ToolInfo{
		{Name: "github__list_issues", Description: "List the open issues", ReadOnly: true},
		{Name: "github__create_issue", Description: "Open an issue"},
		{Name: "read_file", Description: "Read a file", ReadOnly: true},
	}
}

func (h *host) Call(ctx context.Context, name string, args json.RawMessage) (any, error) {
	n := h.running.Add(1)
	defer h.running.Add(-1)
	for {
		p := h.peak.Load()
		if n <= p || h.peak.CompareAndSwap(p, n) {
			break
		}
	}
	select {
	case <-time.After(h.slow):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	switch name {
	case "github__list_issues":
		return map[string]any{"structuredContent": map[string]any{"issues": []map[string]any{{"id": 1, "title": "crash"}, {"id": 2, "title": "typo"}, {"id": 3, "title": "slow"}}}}, nil
	case "read_file":
		var a struct{ Path string }
		json.Unmarshal(args, &a)
		if a.Path == "missing" {
			return nil, errors.New("no such file")
		}
		return "contents of " + a.Path, nil
	}
	return nil, errors.New("refused")
}

type mem struct {
	mu sync.Mutex
	m  map[string]json.RawMessage
}

func (s *mem) Load(k string) (json.RawMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[k]
	return v, ok
}
func (s *mem) Save(k string, v json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
	return nil
}

func TestScriptCallsInParallelAndOnlyItsOutputComesBack(t *testing.T) {
	h := &host{slow: 20 * time.Millisecond}
	r := Run(context.Background(), `
const { structuredContent } = await tools.github__list_issues({ state: "open" });
const texts = await Promise.all(structuredContent.issues.map(i => tools.read_file({ path: "issue-" + i.id })));
console.log("read", texts.length);
return texts.filter(t => !t.endsWith("2"));
`, h, Options{})
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if r.Calls != 4 || h.peak.Load() < 2 || !strings.HasPrefix(r.Output, "read 3\n[") || strings.Contains(r.Output, "issue-2") {
		t.Fatalf("calls %d peak %d output:\n%s", r.Calls, h.peak.Load(), r.Output)
	}
}

func TestErrorsUnknownToolsAndCatch(t *testing.T) {
	h := &host{}
	r := Run(context.Background(), `try { await tools.read_file({path: "missing"}) } catch (e) { print("caught:", e.message) }
await tools.github_list_issues({})`, h, Options{})
	if !strings.Contains(r.Output, "caught: no such file") || r.Err == nil || !strings.Contains(r.Err.Error(), "github__list_issues") {
		t.Fatalf("output %q err %v", r.Output, r.Err)
	}
	if r := Run(context.Background(), `throw new Error("boom")`, h, Options{}); r.Err == nil || r.Err.Error() != "boom" {
		t.Fatalf("throw: %v", r.Err)
	}
	if r := Run(context.Background(), `await new Promise(() => {})`, h, Options{}); r.Err == nil || !strings.Contains(r.Err.Error(), "never comes") {
		t.Fatalf("hang: %v", r.Err)
	}
	if r := Run(context.Background(), `return describeTool("read_file").readOnly && searchTools("issue").length`, h, Options{}); r.Output != "2" {
		t.Fatalf("describe/search: %q %v", r.Output, r.Err)
	}
}

func TestTimeoutBudgetStoreAndOptions(t *testing.T) {
	h := &host{slow: time.Hour}
	start := time.Now()
	r := Run(context.Background(), "// @options: {\"timeout_ms\": 100}\nawait tools.read_file({path: \"a\"})", h, Options{})
	if r.Err == nil || !strings.Contains(r.Err.Error(), "out of time") || time.Since(start) > 5*time.Second {
		t.Fatalf("timeout: %v after %v", r.Err, time.Since(start))
	}
	r = Run(context.Background(), `while (true) {}`, h, Options{Timeout: 100 * time.Millisecond})
	if r.Err == nil {
		t.Fatal("a busy loop ran on")
	}
	r = Run(context.Background(), `for (let i = 0; i < 5000; i++) print("line " + i)`, h, Options{MaxOutputTokens: 100})
	if !r.Truncated || !strings.Contains(r.Output, "line 0") || !strings.Contains(r.Output, "line 4999") || len(r.Output) > 600 {
		t.Fatalf("budget: %d %q", len(r.Output), r.Output[:80])
	}
	s := &mem{m: map[string]json.RawMessage{}}
	Run(context.Background(), `store("seen", [1, 2])`, h, Options{Store: s})
	if r := Run(context.Background(), `return load("seen").length + (load("nope") === undefined ? 10 : 0)`, h, Options{Store: s}); r.Output != "12" {
		t.Fatalf("store: %q %v", r.Output, r.Err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	if r := Run(ctx, `await tools.read_file({path: "a"})`, h, Options{}); r.Err == nil || r.Err.Error() != "stopped" {
		t.Fatalf("stop: %v", r.Err)
	}
}
