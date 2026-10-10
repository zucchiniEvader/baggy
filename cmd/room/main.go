// Command room runs one group exchange against a real API (provider from the
// environment, as cmd/smoke): Chef, Scout and Critic in a scratch store.
//
//	GOBOT_API=chat GOBOT_BASE_URL=https://api.deepseek.com GOBOT_API_KEY=... go run ./cmd/room deepseek-chat
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/provider/connect"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/team"
)

func main() {
	cfg := connect.FromEnv("claude-sonnet-5-5")
	if len(os.Args) > 1 {
		cfg.Model = os.Args[1]
	}
	dir, _ := os.MkdirTemp("", "gobot-room-")
	defer os.RemoveAll(dir)
	st, err := store.Open(dir)
	if err != nil {
		panic(err)
	}
	defer st.Close()
	st.Seed()
	chef := must(st.Bots())[0]
	scout := must(st.CreateBot(store.Bot{Name: "Scout", Description: "You research facts and give short, sourced answers."}))
	critic := must(st.CreateBot(store.Bot{Name: "Critic", Description: "You point out risks and gaps in plans, briefly. You stay quiet when you have nothing to add."}))
	g := must(st.CreateGroup("Trip", "Plan a weekend trip", []string{chef.ID, scout.ID, critic.ID}))

	names := map[string]string{chef.ID: "Chef", scout.ID: "Scout", critic.ID: "Critic", store.User: "user"}
	tm := &team.Team{Store: st, Model: team.Models(st, cfg), Emit: func(e team.Event) {
		switch e.Kind {
		case team.EventPass:
			fmt.Printf("  (%s passed)\n", names[e.BotID])
		case team.EventEntry:
			if e.Entry.Msg.Role == agent.RoleAssistant {
				var t strings.Builder
				for _, p := range e.Entry.Msg.Parts {
					if p.Kind == agent.PartText {
						t.WriteString(p.Text)
					}
				}
				fmt.Printf("%s: %s\n\n", names[e.Entry.Author], strings.TrimSpace(t.String()))
			}
		case team.EventError:
			fmt.Println("error:", e.Err)
		}
	}}
	text := "@Scout what is the usual weather in Kyoto in late October? Then Chef, propose a two-day plan in three lines."
	fmt.Println("user:", text)
	tm.Send(g.ID, text, []string{scout.ID}, nil)
	tm.Wait()
	es := must(st.Messages(g.ID, 0))
	fmt.Printf("stored %d messages\n", len(es))
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
