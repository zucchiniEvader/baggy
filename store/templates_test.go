package store

import (
	"strings"
	"testing"
)

func TestTemplates(t *testing.T) {
	seen := map[string]bool{}
	for _, tp := range Templates {
		if seen[tp.ID] || tp.Name == "" || tp.Summary == "" || tp.Description == "" || len(tp.Suggestions) == 0 {
			t.Errorf("template %+v", tp)
		}
		seen[tp.ID] = true
	}
	for _, g := range GroupTemplates {
		names := map[string]string{}
		for _, id := range g.Members {
			if !seen[id] {
				t.Errorf("%s: no template %q", g.ID, id)
			}
			names[id] = "X"
		}
		if d := Named(g.Description, names); strings.Contains(d, "{") {
			t.Errorf("%s: unnamed member in %q", g.ID, d)
		}
	}
	if b := (Template{Workdir: "~/no-such-folder-here"}).Bot(); b.Workdir != "" {
		t.Errorf("a missing folder was kept: %q", b.Workdir)
	}
}
