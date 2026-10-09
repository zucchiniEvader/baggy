// Command desktop is gobot's window: a chat with the agent, drawn by MyGo's
// native UI.
//
//	ANTHROPIC_API_KEY=... go tool mygo dev
//
// GOBOT_MODEL picks the model (default claude-sonnet-5-5), ANTHROPIC_BASE_URL
// points at another server speaking the Messages API.
package main

import (
	"log"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/provider"
	"github.com/spike/gobot/provider/anthropic"
	"github.com/spike/gobot/tool"
)

const system = "You are a personal assistant on the user's computer. You can run shell commands and read and write files in the working directory. Be brief."

func main() {
	dir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	model := os.Getenv("GOBOT_MODEL")
	if model == "" {
		model = "claude-sonnet-5-5"
	}
	key := os.Getenv("ANTHROPIC_API_KEY")
	p := anthropic.New("anthropic", os.Getenv("ANTHROPIC_BASE_URL"), key, model)
	tools := []tool.Tool{tool.Shell{Dir: dir}, tool.ReadFile{Dir: dir}, tool.WriteFile{Dir: dir}}

	app := newChat(provider.Model{Provider: p}, system, tools, nil)
	if key == "" {
		app.notice("ANTHROPIC_API_KEY is not set; replies will fail.")
	}
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:   "gobot",
			Width:   760,
			Height:  640,
			Content: ui.View(app.view),
		})
		app.post = win.Update
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
