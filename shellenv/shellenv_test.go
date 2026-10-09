package shellenv

import (
	"strings"
	"testing"
)

func TestEnvHasPath(t *testing.T) {
	path := ""
	for _, kv := range Env() {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			path = v
		}
	}
	if path == "" {
		t.Fatal("no PATH")
	}
	got := With([]string{"A=1", "B=2"}, "B=3", "C=4")
	if strings.Join(got, ",") != "A=1,B=3,C=4" {
		t.Fatal(got)
	}
}
