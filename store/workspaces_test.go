package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUseWorkspacesMovesThemOver(t *testing.T) {
	s := open(t)
	a, _ := s.CreateBot(Bot{Name: "A"})
	b, _ := s.CreateBot(Bot{Name: "B"})
	old := s.Workspaces()
	os.WriteFile(filepath.Join(s.Workspace(a.ID), "MEMORY.md"), []byte("- a fact"), 0o644)
	root := filepath.Join(t.TempDir(), ".gobot", "workspaces")
	os.MkdirAll(filepath.Join(root, b.ID), 0o755) // B has one there already
	os.WriteFile(filepath.Join(root, b.ID, "keep"), []byte("new"), 0o644)
	if err := s.UseWorkspaces(root); err != nil {
		t.Fatal(err)
	}
	if s.Workspace(a.ID) != filepath.Join(root, a.ID) {
		t.Fatalf("workspace %q", s.Workspace(a.ID))
	}
	if data, err := os.ReadFile(filepath.Join(root, a.ID, "MEMORY.md")); err != nil || string(data) != "- a fact" {
		t.Fatalf("moved memory %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(old, a.ID)); !os.IsNotExist(err) {
		t.Fatal("the old workspace stays")
	}
	if _, err := os.Stat(filepath.Join(old, b.ID)); err != nil {
		t.Fatal("B's old workspace was lost though the new one was kept")
	}
	if _, err := os.Stat(filepath.Join(root, b.ID, "keep")); err != nil {
		t.Fatal("B's new workspace was overwritten")
	}
	c, _ := s.CreateBot(Bot{Name: "C"})
	if _, err := os.Stat(filepath.Join(root, c.ID)); err != nil {
		t.Fatal("a new bot's workspace is not in the new folder")
	}
	if err := s.UseWorkspaces(root); err != nil { // again: nothing to do
		t.Fatal(err)
	}
}

func TestOpenRenamesTheOldDatabase(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	s.CreateBot(Bot{Name: "Kept"})
	s.Close()
	os.Rename(filepath.Join(dir, "baggy.db"), filepath.Join(dir, "gobot.db"))
	os.Remove(filepath.Join(dir, "baggy.db-wal"))
	os.Remove(filepath.Join(dir, "baggy.db-shm"))
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if bs, _ := s.Bots(); len(bs) != 1 || bs[0].Name != "Kept" {
		t.Fatalf("bots %+v", bs)
	}
	if _, err := os.Stat(filepath.Join(dir, "gobot.db")); !os.IsNotExist(err) {
		t.Fatal("the old file stays")
	}
}
