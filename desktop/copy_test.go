package main

import (
	"testing"

	"github.com/zucchiniEvader/baggy/agent"
)

func TestPlainText(t *testing.T) {
	src := "# Plan\n\nShip **#12** first, then `#15`. See [the board](https://x.test/b) or <https://x.test>.\n\n1. Fix the crash\n2. Write notes\n   - in `RELEASE.md`\n\n- [x] tests\n- [ ] docs\n\n> Quoted\n\n```go\nfmt.Println(\"hi\")\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |"
	want := "Plan\n\nShip #12 first, then #15. See the board (https://x.test/b) or https://x.test.\n\n1. Fix the crash\n2. Write notes\n   • in RELEASE.md\n\n☑ tests\n☐ docs\n\n  Quoted\n\nfmt.Println(\"hi\")\n\na\tb\n1\t2"
	if got := plainText(src); got != want {
		t.Fatalf("got\n%s\n\nwant\n%s", got, want)
	}
}

func TestCopyAReply(t *testing.T) {
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("Use **bold** and `code`.\n\nSecond line.") }))
	var got []string
	h.a.copyText = func(s string) { got = append(got, s) }
	h.send("hi")
	h.pump("the reply", func() bool { return h.tt.HasText("Second line.") && h.idle() })
	r, _ := h.tt.Find("Second line.")
	h.tt.Move(r.X+2, r.Y+2)
	h.tt.Frame()
	h.tt.Frame()
	if err := h.tt.Click("Copy Text"); err != nil {
		t.Fatalf("%v: %q", err, h.tt.Texts())
	}
	h.tt.Frame()
	if len(got) != 1 || got[0] != "Use bold and code.\n\nSecond line." {
		t.Fatalf("copied %q", got)
	}
	if h.a.copied == "" { // the button shows a check for a moment
		t.Fatal("not marked copied")
	}
	if err := h.tt.Click("Copy as Markdown"); err != nil {
		t.Fatal(err)
	}
	if got[1] != "Use **bold** and `code`.\n\nSecond line." {
		t.Fatalf("markdown %q", got[1])
	}
	if err := h.tt.RightClick("Second line."); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem("Copy Text"); err != nil {
		t.Fatalf("%v: %q", err, h.tt.Menu())
	}
	h.tt.Frame()
	if len(got) != 3 || got[2] != got[0] {
		t.Fatalf("from the menu %q", got)
	}
}
