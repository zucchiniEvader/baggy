package tool

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestShellReportsOutputAsItComes(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	ctx := WithUpdate(context.Background(), func(r Result) { mu.Lock(); seen = append(seen, r.Text); mu.Unlock() })
	r := Shell{Dir: t.TempDir()}.Execute(ctx, json.RawMessage(`{"command":"echo one; sleep 0.6; echo two"}`))
	if r.IsError || !strings.Contains(r.Text, "two") {
		t.Fatalf("result %+v", r)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 || strings.TrimSpace(seen[0]) != "one" {
		t.Fatalf("updates %q", seen)
	}
	Update(context.Background(), Result{Text: "nobody listens"}) // no WithUpdate: nothing
}
