// Command desktop is gobot's window: a chat with the agent, drawn by MyGo's
// native UI.
//
//	ANTHROPIC_API_KEY=... go tool mygo dev
//
// The provider comes from the environment (connect.FromEnv): GOBOT_API picks
// the protocol (anthropic, chat or responses), GOBOT_MODEL the model (default
// claude-sonnet-5-5), GOBOT_BASE_URL and GOBOT_API_KEY the server.
package main

import (
	"log"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/provider"
	"github.com/spike/gobot/provider/connect"
	"github.com/spike/gobot/tool"
)

const system = "You are a personal assistant on the user's computer. You can run shell commands and read and write files in the working directory. Be brief."

func main() {
	dir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	cfg := connect.FromEnv("claude-sonnet-5-5")
	p, err := connect.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	tools := []tool.Tool{tool.Shell{Dir: dir}, tool.ReadFile{Dir: dir}, tool.WriteFile{Dir: dir}}

	app := newChat(provider.Model{Provider: p}, system, tools, nil)
	if cfg.APIKey == "" {
		app.notice("No API key (GOBOT_API_KEY, or ANTHROPIC_API_KEY for the Anthropic API); replies will fail.")
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
