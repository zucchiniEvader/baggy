package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixed(t *testing.T) *Memory {
	m := New(t.TempDir())
	m.Now = func() time.Time { return time.Date(2026, 10, 9, 9, 5, 0, 0, time.Local) }
	return m
}

func TestUpdateOps(t *testing.T) {
	m := fixed(t)
	if _, err := m.Update(Append, "the user prefers short replies", "", "your chat with the user"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Update(Append, "the user prefers short replies", "", "x"); err == nil {
		t.Fatal("duplicate accepted")
	}
	m.Update(Append, "standup is at 9", "", `group "Team"`)
	if _, err := m.Update(Supersede, "standup is at 10", "standup is at 9", `group "Team"`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Update(Replace, "very short replies", "short replies", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Update(Remove, "", "nowhere", "x"); err != ErrNotUnique {
		t.Fatalf("remove of a missing passage: %v", err)
	}
	got := m.Read()
	want := "- 2026-10-09 · from your chat with the user · the user prefers very short replies\n" +
		"- ~~2026-10-09 · from group \"Team\" · standup is at 9~~ (superseded 2026-10-09)\n" +
		"- 2026-10-09 · from group \"Team\" · standup is at 10\n"
	if got != want {
		t.Fatalf("MEMORY.md:\n%s\nwant:\n%s", got, want)
	}
	m.Update(Remove, "", "- ~~2026-10-09 · from group \"Team\" · standup is at 9~~ (superseded 2026-10-09)\n", "x")
	if strings.Contains(m.Read(), "~~") || strings.Count(m.Read(), "\n") != 2 {
		t.Fatalf("after remove:\n%s", m.Read())
	}
}

func TestPromptBudgetAndTopics(t *testing.T) {
	m := fixed(t)
	var b strings.Builder
	for i := 0; i < LoadLines+5; i++ {
		b.WriteString("- fact\n")
	}
	os.WriteFile(m.Index(), []byte(b.String()), 0o644)
	os.MkdirAll(m.Topics(), 0o755)
	os.WriteFile(filepath.Join(m.Topics(), "trips.md"), []byte("Kyoto"), 0o644)
	p := m.Prompt()
	if strings.Count(p, "- fact") != LoadLines || !strings.Contains(p, "5 more lines") || !strings.Contains(p, "memory/trips.md") {
		t.Fatalf("prompt:\n%s", p[len(p)-300:])
	}
}

func TestLogAndSearch(t *testing.T) {
	m := fixed(t)
	m.Log("in your chat with the user · said \"Booked the Kyoto hotel\"")
	m.Update(Append, "the hotel is in Gion", "", "x")
	hits := m.Search("kyoto hotel", time.Time{})
	if len(hits) != 1 || !strings.Contains(hits[0].Where, "memory/log/2026-10-09.md") || !strings.HasPrefix(hits[0].Text, "- 09:05 ·") {
		t.Fatalf("hits %+v", hits)
	}
	if hits := m.Search("hotel", time.Date(2026, 10, 9, 0, 0, 0, 0, time.Local)); len(hits) != 1 {
		t.Fatalf("since: %+v", hits) // the diary only: MEMORY.md has no time
	}
}

func TestScrub(t *testing.T) {
	in := "key sk-0000fake0000fake0000fake0000fake and ghp_abcdefghijklmnopqrstuvwxyz123 and password: hunter22 and AKIAABCDEFGHIJKLMNOP"
	out := Scrub(in)
	for _, leak := range []string{"sk-0000", "ghp_", "hunter22", "AKIA"} {
		if strings.Contains(out, leak) {
			t.Fatalf("%s leaked: %s", leak, out)
		}
	}
	if !strings.Contains(out, "password: «redacted 8 chars»") {
		t.Fatalf("scrubbed: %s", out)
	}
	m := fixed(t)
	m.Update(Append, "the token: abcdef123456", "", "x")
	if strings.Contains(m.Read(), "abcdef123456") {
		t.Fatal("memory kept a credential")
	}
}

func TestSaveRefusesAChangedFile(t *testing.T) {
	m := fixed(t)
	m.Update(Append, "one", "", "x")
	was := m.Read()
	m.Update(Append, "two", "", "x")
	if err := m.Save(was, "edited"); err != ErrChanged {
		t.Fatalf("save over a change: %v", err)
	}
	if err := m.Save(m.Read(), "edited\n"); err != nil || m.Read() != "edited\n" {
		t.Fatalf("save: %v %q", err, m.Read())
	}
	if l, _, cut := m.Loaded(); l != 1 || cut != 0 {
		t.Fatalf("loaded %d cut %d", l, cut)
	}
}
