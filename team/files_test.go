package team

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spike/gobot/agent"
)

func TestSendFilesPlacesThemInTheWorkspace(t *testing.T) {
	var seen []agent.Message
	f := setup(t, script(func(bot string, msgs []agent.Message) agent.Message {
		seen = msgs
		return say("got it")
	}))
	b := f.bot(t, "Chef")
	dm, _ := f.st.DMOf(b.ID)
	var pic bytes.Buffer
	png.Encode(&pic, image.NewRGBA(image.Rect(0, 0, 3000, 1500)))
	err := f.team.SendFiles(dm.ID, "look", []Attachment{{Name: "notes.txt", Data: []byte("hello")}, {Name: "../wide.png", Data: pic.Bytes()}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.team.Wait()

	es, _ := f.st.Messages(dm.ID, 0)
	files := es[0].Meta.Files
	if len(files) != 2 || files[1].Name != "wide.png" || !files[1].Image || files[0].Image {
		t.Fatalf("files %+v", files)
	}
	if got := es[0].Msg.Parts[0].Text; got != "look" {
		t.Fatalf("stored text %q", got)
	}
	data, err := os.ReadFile(filepath.Join(f.st.Workspace(b.ID), files[0].Rel()))
	if err != nil || string(data) != "hello" {
		t.Fatalf("workspace copy %q %v", data, err)
	}
	user := seen[0]
	text := user.Parts[0].Text
	if !strings.Contains(text, "[Attached file: "+files[0].Rel()+" (5 bytes, text/plain)]") ||
		!strings.Contains(text, files[1].Rel()) || !strings.Contains(text, "scaled down from 3000×1500 to 2000×1000") {
		t.Fatalf("model read %q", text)
	}
	if len(user.Parts) != 2 || user.Parts[1].Kind != agent.PartImage {
		t.Fatalf("parts %+v", user.Parts)
	}
}
