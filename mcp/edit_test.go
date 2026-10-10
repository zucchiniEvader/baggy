package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAndDeleteServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	os.WriteFile(path, []byte(`{"globalShortcut": "x", "mcpServers": {"github": {"command": "npx", "args": ["gh"], "alwaysAllow": ["read"]}}}`), 0o644)

	// Renamed and changed: the key it does not know stays.
	if err := SaveServer(path, "github", Server{Name: "GitHub", Command: "npx", Args: []string{"-y", "gh-mcp"}, Env: map[string]string{"TOKEN": "t"}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	ss, err := Parse(b)
	if err != nil || len(ss) != 1 || ss[0].Name != "GitHub" || ss[0].Args[1] != "gh-mcp" || ss[0].Env["TOKEN"] != "t" {
		t.Fatalf("servers %+v %v", ss, err)
	}
	if !strings.Contains(string(b), `"alwaysAllow"`) || !strings.Contains(string(b), `"globalShortcut"`) {
		t.Fatalf("lost keys: %s", b)
	}

	// A new remote one; a clash and an empty one are refused.
	if err := SaveServer(path, "", Server{Name: "Docs", Type: "http", URL: "https://example.com/mcp", Disabled: true}); err != nil {
		t.Fatal(err)
	}
	if SaveServer(path, "", Server{Name: "github", Command: "x"}) == nil {
		t.Fatal("a second GitHub")
	}
	if SaveServer(path, "", Server{Name: "Empty"}) == nil {
		t.Fatal("a server with nothing to run")
	}
	b, _ = os.ReadFile(path)
	ss, _ = Parse(b)
	if len(ss) != 2 || ss[0].Name != "Docs" || !ss[0].Disabled || ss[0].URL == "" {
		t.Fatalf("servers %+v", ss)
	}

	if err := DeleteServer(path, "GitHub"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	var f map[string]map[string]json.RawMessage
	json.Unmarshal(b, &f)
	if len(f["mcpServers"]) != 1 {
		t.Fatalf("after delete: %s", b)
	}

	// A missing file is made; VS Code's "servers" is kept.
	fresh := filepath.Join(t.TempDir(), "sub", "mcp.json")
	if err := SaveServer(fresh, "", Server{Name: "a", Command: "a"}); err != nil {
		t.Fatal(err)
	}
	vs := filepath.Join(t.TempDir(), "mcp.json")
	os.WriteFile(vs, []byte(`{"servers": {}}`), 0o644)
	SaveServer(vs, "", Server{Name: "b", Command: "b"})
	if b, _ := os.ReadFile(vs); !strings.Contains(string(b), `"servers"`) || strings.Contains(string(b), "mcpServers") {
		t.Fatalf("vs code file: %s", b)
	}
}

func TestWorkDir(t *testing.T) {
	home, _ := os.UserHomeDir()
	if workDir("") != home || workDir("~/x") != filepath.Join(home, "x") || workDir("/tmp") != "/tmp" {
		t.Fatal(workDir(""), workDir("~/x"))
	}
}
