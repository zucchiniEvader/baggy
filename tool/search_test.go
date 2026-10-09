package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runTool(t *testing.T, tl Tool, args any) Result {
	t.Helper()
	raw, _ := json.Marshal(args)
	v, err := ValidateArgs(tl.Name(), tl.Schema(), raw)
	if err != nil {
		t.Fatal(err)
	}
	return tl.Execute(context.Background(), v)
}

func tree(t *testing.T) string {
	dir := t.TempDir()
	for p, s := range map[string]string{
		"a.go":              "package a\n\nfunc Hello() {}\n",
		"sub/b.go":          "package sub\n// hello there\n",
		"sub/c.md":          "# Hello\n",
		".git/x.go":         "hello\n",
		"node_modules/m.js": "hello\n",
		"bin.dat":           "hel\x00lo",
	} {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte(s), 0o644)
	}
	return dir
}

func TestEditFile(t *testing.T) {
	dir := tree(t)
	e := EditFile{Dir: dir}
	if r := runTool(t, e, map[string]any{"path": "a.go", "old": "Hello", "new": "Hi"}); r.IsError {
		t.Fatal(r.Text)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.go")); !strings.Contains(string(b), "func Hi()") {
		t.Fatalf("not edited: %s", b)
	}
	os.WriteFile(filepath.Join(dir, "d.txt"), []byte("x x"), 0o644)
	if r := runTool(t, e, map[string]any{"path": "d.txt", "old": "x", "new": "y"}); !r.IsError || !strings.Contains(r.Text, "2 times") {
		t.Fatalf("ambiguous: %+v", r)
	}
	if r := runTool(t, e, map[string]any{"path": "d.txt", "old": "x", "new": "y", "all": true}); r.IsError {
		t.Fatal(r.Text)
	}
	if r := runTool(t, e, map[string]any{"path": "d.txt", "old": "zzz", "new": "y"}); !r.IsError {
		t.Fatal("missing passage accepted")
	}
	os.WriteFile(filepath.Join(dir, "w.txt"), []byte("one\r\ntwo\r\n"), 0o644)
	if r := runTool(t, e, map[string]any{"path": "w.txt", "old": "one\ntwo", "new": "1\n2"}); r.IsError {
		t.Fatal(r.Text)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "w.txt")); string(b) != "1\r\n2\r\n" {
		t.Fatalf("CRLF edit: %q", b)
	}
}

func TestFindAndGrep(t *testing.T) {
	dir := tree(t)
	r := runTool(t, Find{Dir: dir}, map[string]any{"pattern": "*.go"})
	if lines := strings.Split(r.Text, "\n"); len(lines) != 2 || strings.Contains(r.Text, ".git") {
		t.Fatalf("find *.go: %q", r.Text)
	}
	if r := runTool(t, Find{Dir: dir}, map[string]any{"pattern": "sub/*.{md,go}"}); strings.Count(r.Text, "\n") != 1 {
		t.Fatalf("find braces: %q", r.Text)
	}
	r = runTool(t, Grep{Dir: dir}, map[string]any{"pattern": "(?i)hello"})
	if !strings.Contains(r.Text, "a.go:3: func Hello() {}") || !strings.Contains(r.Text, "sub/b.go:2:") || strings.Contains(r.Text, "node_modules") || strings.Contains(r.Text, "bin.dat") {
		t.Fatalf("grep: %q", r.Text)
	}
	if r := runTool(t, Grep{Dir: dir}, map[string]any{"pattern": "hello", "glob": "*.md"}); !strings.Contains(r.Text, "no matches") {
		t.Fatalf("grep glob: %q", r.Text)
	}
	if r := runTool(t, Grep{Dir: dir}, map[string]any{"pattern": "Hello", "path": "sub/c.md"}); r.Text != "c.md:1: # Hello" {
		t.Fatalf("grep one file: %q", r.Text)
	}
}

func TestSeveralEditsLsAndGitignore(t *testing.T) {
	dir := tree(t)
	os.WriteFile(filepath.Join(dir, "e.txt"), []byte("alpha beta gamma"), 0o644)
	if r := runTool(t, EditFile{Dir: dir}, map[string]any{"path": "e.txt", "edits": []map[string]any{{"old": "alpha", "new": "A"}, {"old": "gamma", "new": "G"}}}); r.IsError {
		t.Fatal(r.Text)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "e.txt")); string(b) != "A beta G" {
		t.Fatalf("%q", b)
	}
	if r := runTool(t, EditFile{Dir: dir}, map[string]any{"path": "e.txt", "edits": []map[string]any{{"old": "beta", "new": "B"}, {"old": "nope", "new": "x"}}}); !r.IsError || !strings.Contains(r.Text, "edit 2") {
		t.Fatalf("%+v", r)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "e.txt")); string(b) != "A beta G" {
		t.Fatal("a failed edit changed the file")
	}
	r := runTool(t, Ls{Dir: dir}, map[string]any{})
	if !strings.HasPrefix(r.Text, ".git/") || !strings.Contains(r.Text, "sub/") || !strings.Contains(r.Text, "a.go  ") {
		t.Fatalf("ls: %q", r.Text)
	}
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("# build output\n*.md\n/build/\n!keep.md\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "build"), 0o755)
	os.WriteFile(filepath.Join(dir, "build", "x.go"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(dir, "keep.md"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(dir, "sub", ".gitignore"), []byte("b.go\n"), 0o644)
	r = runTool(t, Find{Dir: dir}, map[string]any{"pattern": "**"})
	for _, gone := range []string{"sub/c.md", "build/x.go", "sub/b.go"} {
		if strings.Contains(r.Text, gone) {
			t.Fatalf("find showed %s: %q", gone, r.Text)
		}
	}
	if !strings.Contains(r.Text, "keep.md") || !strings.Contains(r.Text, "a.go") {
		t.Fatalf("find: %q", r.Text)
	}
	if r := runTool(t, Grep{Dir: dir}, map[string]any{"pattern": "(?i)hello"}); strings.Contains(r.Text, "build/") || !strings.Contains(r.Text, "keep.md") {
		t.Fatalf("grep: %q", r.Text)
	}
}
