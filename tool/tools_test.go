package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func run(t *testing.T, tl Tool, ctx context.Context, args string) Result {
	t.Helper()
	a, err := ValidateArgs(tl.Name(), tl.Schema(), json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return tl.Execute(ctx, a)
}

func TestShell(t *testing.T) {
	dir := t.TempDir()
	sh := Shell{Dir: dir}
	ctx := context.Background()

	if r := run(t, sh, ctx, `{"command":"echo hi; echo err >&2; pwd"}`); r.IsError || !strings.Contains(r.Text, "hi\nerr\n") || !strings.Contains(r.Text, filepath.Base(dir)) {
		t.Errorf("output: %+v", r)
	}
	if r := run(t, sh, ctx, `{"command":"echo boom; exit 3"}`); !r.IsError || !strings.Contains(r.Text, "boom") || !strings.Contains(r.Text, "exit status 3") {
		t.Errorf("exit: %+v", r)
	}
	if r := run(t, sh, ctx, `{"command":"true"}`); r.IsError || r.Text != "(no output)" {
		t.Errorf("empty: %+v", r)
	}
	if r := run(t, sh, ctx, `{"command":"seq 1 5000"}`); !strings.HasPrefix(r.Text, "[output truncated") || !strings.HasSuffix(r.Text, "5000\n") || strings.Contains(r.Text, "\n1\n") {
		t.Errorf("tail truncation: %.80q", r.Text)
	}
}

func TestShellTimeoutAndCancelKillChildren(t *testing.T) {
	sh := Shell{}
	start := time.Now()
	r := run(t, sh, context.Background(), `{"command":"sleep 30 & sleep 30","timeout":1}`)
	if !r.IsError || !strings.Contains(r.Text, "timed out") || time.Since(start) > 10*time.Second {
		t.Errorf("timeout: %+v after %s", r, time.Since(start))
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	start = time.Now()
	r = run(t, sh, ctx, `{"command":"sleep 30 & sleep 30"}`)
	if !r.IsError || !strings.Contains(r.Text, "cancelled") || time.Since(start) > 10*time.Second {
		t.Errorf("cancel: %+v after %s", r, time.Since(start))
	}
}

func TestFiles(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	w, r := WriteFile{Dir: dir}, ReadFile{Dir: dir}

	if res := run(t, w, ctx, `{"path":"a/b.txt","content":"one\ntwo\nthree"}`); res.IsError || !strings.Contains(res.Text, "13 bytes") {
		t.Fatalf("write: %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a/b.txt")); string(b) != "one\ntwo\nthree" {
		t.Fatalf("file: %q", b)
	}
	if res := run(t, r, ctx, `{"path":"a/b.txt"}`); res.Text != "one\ntwo\nthree" {
		t.Errorf("read: %+v", res)
	}
	res := run(t, r, ctx, `{"path":"a/b.txt","offset":2,"limit":1}`)
	if res.IsError || !strings.HasPrefix(res.Text, "two\n") || !strings.Contains(res.Text, "lines 2-2 of 3; continue with offset 3") {
		t.Errorf("range: %+v", res)
	}
	if res := run(t, r, ctx, `{"path":"a/b.txt","offset":9}`); !res.IsError {
		t.Errorf("offset past end: %+v", res)
	}
	if res := run(t, r, ctx, `{"path":"missing"}`); !res.IsError {
		t.Errorf("missing: %+v", res)
	}

	var big strings.Builder
	for i := 1; i <= 3000; i++ {
		fmt.Fprintf(&big, "line %d\n", i)
	}
	os.WriteFile(filepath.Join(dir, "big"), []byte(big.String()), 0o644)
	if res := run(t, r, ctx, `{"path":"big"}`); !strings.Contains(res.Text, "lines 1-2000 of 3001; continue with offset 2001") {
		t.Errorf("big: %q", res.Text[len(res.Text)-120:])
	}
	os.WriteFile(filepath.Join(dir, "bin"), []byte("a\x00b"), 0o644)
	if res := run(t, r, ctx, `{"path":"bin"}`); !res.IsError {
		t.Errorf("binary: %+v", res)
	}
}
