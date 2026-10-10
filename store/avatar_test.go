package store

import (
	"os"
	"testing"
)

func TestBotAvatarAndColor(t *testing.T) {
	s := open(t)
	b, _ := s.CreateBot(Bot{Name: "Scout", Color: "teal"})
	first, err := s.SaveAvatar([]byte("png1"))
	if err != nil {
		t.Fatal(err)
	}
	b.Avatar = first
	if err := s.UpdateBot(b); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Bot(b.ID); got.Avatar != first || got.Color != "teal" {
		t.Fatalf("bot %+v", got)
	}
	second, _ := s.SaveAvatar([]byte("png2"))
	b.Avatar = second
	s.UpdateBot(b)
	if _, err := os.Stat(s.AvatarPath(first)); !os.IsNotExist(err) {
		t.Fatal("the old picture is kept")
	}
	s.DeleteBot(b.ID)
	if _, err := os.Stat(s.AvatarPath(second)); !os.IsNotExist(err) {
		t.Fatal("a deleted bot's picture is kept")
	}
}

func TestBotWorkdir(t *testing.T) {
	s := open(t)
	home, _ := os.UserHomeDir()
	b, _ := s.CreateBot(Bot{Name: "Scout"})
	if s.WorkDir(b) != s.Workspace(b.ID) {
		t.Fatal("the default is the workspace")
	}
	dir := t.TempDir()
	b.Workdir = dir
	if err := s.UpdateBot(b); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Bot(b.ID); s.WorkDir(got) != dir {
		t.Fatalf("workdir %q", s.WorkDir(got))
	}
	if s.WorkDir(Bot{Workdir: "~"}) != home {
		t.Fatal("~")
	}
	for _, bad := range []string{"relative/path", dir + "/missing"} {
		b.Workdir = bad
		if s.UpdateBot(b) == nil {
			t.Fatalf("%q taken", bad)
		}
	}
	if _, err := s.CreateBot(Bot{Name: "X", Workdir: "nope"}); err == nil {
		t.Fatal("created with a relative folder")
	}
}
