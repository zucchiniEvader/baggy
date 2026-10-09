package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/tool"
)

// GOBOT_RENDER=<folder> go test -run TestRender writes a PNG of a sample chat.
func TestRender(t *testing.T) {
	dir := os.Getenv("GOBOT_RENDER")
	if dir == "" {
		t.Skip("set GOBOT_RENDER to a folder")
	}
	c := newChat(nil, "", nil, func(fn func()) { fn() })
	c.rows = []row{
		{kind: rowUser, text: "Write notes/a.txt with the numbers 1 to 3, then count its lines."},
		{kind: rowAssistant, thinking: "I will write the file first.", text: "Writing it now, then counting."},
		{kind: rowTool, call: agent.Part{Name: "write_file", Args: []byte(`{"path":"notes/a.txt","content":"1\n2\n3\n"}`)}, result: &tool.Result{Text: "wrote 6 bytes to notes/a.txt"}},
		{kind: rowTool, call: agent.Part{Name: "shell", Args: []byte(`{"command":"wc -l notes/a.txt"}`)}, result: &tool.Result{Text: "exit status 1", IsError: true}, failed: true},
		{kind: rowTool, call: agent.Part{Name: "shell", Args: []byte(`{"command":"sleep 5"}`)}},
		{kind: rowAssistant, text: "The file has 3 lines.", stopped: true},
		{kind: rowNotice, text: "ANTHROPIC_API_KEY is not set; replies will fail."},
	}
	c.running, c.queued = true, 1
	for name, dark := range map[string]bool{"light": false, "dark": true} {
		tt := ui.NewTester(c.view, 760, 640)
		tt.SetDark(dark)
		tt.Frame()
		f, err := os.Create(filepath.Join(dir, "chat-"+name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
