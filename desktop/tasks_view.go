package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/terminal"
)

// taskDelay is how long a command runs before it counts as a task, so a
// quick ls never shows.
const taskDelay = 2 * time.Second

// sessionOf is the running session of a shell card: the chat's newest one
// running its command.
func (a *app) sessionOf(chatID string, call agent.Part) *terminal.Session {
	if a.team.Terminals == nil || call.Name != "shell" {
		return nil
	}
	var args struct{ Command string }
	json.Unmarshal(call.Args, &args)
	running := a.team.Terminals.Running(chatID)
	for i := len(running) - 1; i >= 0; i-- {
		if running[i].Command == args.Command {
			return running[i]
		}
	}
	return nil
}

// tasks are a chat's commands that have run a while.
func (a *app) tasks(chatID string) []*terminal.Session {
	if a.team.Terminals == nil {
		return nil
	}
	var out []*terminal.Session
	for _, s := range a.team.Terminals.Running(chatID) {
		if time.Since(s.Started) >= taskDelay {
			out = append(out, s)
		}
	}
	return out
}

func elapsed(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// tasksButton opens the chat's running tasks while it has any.
func (a *app) tasksButton(c *ui.Context, ch store.Chat) {
	ts := a.tasks(ch.ID)
	if len(ts) == 0 {
		a.tasksOpen = false
		return
	}
	btn := ui.Button(c.Key("tasks"), L("Tasks (%d)", len(ts))).Tooltip(L("Commands running in this chat"))
	if btn.Clicked() {
		a.tasksOpen = !a.tasksOpen
	}
	t := c.Theme()
	ui.Popover(c, btn, &a.tasksOpen, func() {
		ui.Column(c).Width(420).Gap(10).Children(func() {
			for _, s := range ts {
				s := s
				ui.Column(c.Key("task-" + s.ID)).Gap(4).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						title := s.Description
						if title == "" {
							title = s.Command
						}
						ui.Text(c, oneLine(title, 60)).Bold().Grow(1).SingleLine()
						who := ""
						if ch.Kind == store.Group {
							who = a.name(s.Bot) + " · "
						}
						ui.Text(c, who+elapsed(time.Since(s.Started))).FontSize(11).TextColor(t.TextMuted)
					})
					ui.Column(c).Padding(6, 8).Radius(6).Background(t.Surface).Children(func() {
						ui.Text(c, oneLine(s.Command, 120)).Font(monoFont).FontSize(11).Selectable()
					})
					if last := s.Last(10); last != "" {
						ui.Column(c).Padding(6, 8).Radius(6).Background(t.Surface).Children(func() {
							ui.Text(c, last).Font(monoFont).FontSize(11).Selectable()
						})
					}
					ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
						if !s.Background() {
							if ui.Button(c.Key("bg-"+s.ID), L("Run in Background")).Clicked() {
								s.SendToBackground()
							}
						}
						if ui.Button(c.Key("stop-"+s.ID), L("Stop")).Clicked() {
							go s.Stop(L("stopped from Running tasks"))
						}
					})
				})
			}
		})
	})
}
