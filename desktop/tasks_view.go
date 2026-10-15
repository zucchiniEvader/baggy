package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/claudecode"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/terminal"
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

// claudeRuns are a chat's Claude Code tasks at work.
func (a *app) claudeRuns(chatID string) []*claudecode.Run {
	if a.team.ClaudeCode == nil {
		return nil
	}
	return a.team.ClaudeCode.Running(chatID)
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
	runs := a.claudeRuns(ch.ID)
	n := len(ts) + len(runs)
	if n == 0 {
		a.tasksOpen = false
		return
	}
	btn := ui.ButtonBase(c.Key("tasks")).Height(compactHeight).Padding(0, 8).Gap(5).Radius(rowRadius).AlignItems(ui.Center).Label(L("Tasks (%d)", n)).Tooltip(L("Commands and Claude Code tasks running in this chat")).Shrink(0)
	styleButton(btn, c.Theme(), ghost, false)
	btn.Children(func() {
		ui.Icon(c, iconTerminal).FontSize(iconSize)
		ui.Text(c, fmt.Sprint(n)).Font(monoFont).FontSize(11).Bold()
	})
	if btn.Clicked() {
		a.tasksOpen = !a.tasksOpen
	}
	t := c.Theme()
	p := pal(c)
	ui.PopoverBase(c, btn, &a.tasksOpen, func(panel ui.Element) {
		panel.Margin(6, 0, 0, 0).Padding(12).Radius(cardRadius).Background(p.surface).Border(1, p.border)
		ui.Column(c).Width(420).Gap(14).Children(func() {
			for _, r := range runs {
				a.claudeTask(c, ch, r)
			}
			for _, s := range ts {
				s := s
				ui.Column(c.Key("task-" + s.ID)).Gap(4).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						title := s.Description
						if title == "" {
							title = s.Command
						}
						ui.Icon(c, iconTerminal).FontSize(iconSize).TextColor(t.TextMuted).Shrink(0)
						ui.Text(c, oneLine(title, 60)).FontSize(13).FontWeight(600).Grow(1).SingleLine().MinWidth(0)
						who := ""
						if ch.Kind == store.Group {
							who = a.name(s.Bot) + " · "
						}
						ui.Text(c, who+elapsed(time.Since(s.Started))).FontSize(11).TextColor(t.TextMuted)
					})
					codeBlock(c, oneLine(s.Command, 120))
					if last := s.Last(10); last != "" {
						codeBlock(c, last)
					}
					ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
						if !s.Background() {
							if smallButton(c.Key("bg-"+s.ID), L("Run in Background"), secondary).Clicked() {
								s.SendToBackground()
							}
						}
						if smallButton(c.Key("stop-"+s.ID), L("Stop"), danger).Clicked() {
							go s.Stop(L("stopped from Running tasks"))
						}
					})
				})
			}
		})
	})
}

// claudeTask is a Claude Code task in Running tasks: what it was given, its
// recent steps, what it said last, and Stop.
func (a *app) claudeTask(c *ui.Context, ch store.Chat, r *claudecode.Run) {
	t := c.Theme()
	ui.Column(c.Key("cc-" + r.ID)).Gap(4).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconCode).FontSize(iconSize).TextColor(t.TextMuted).Shrink(0)
			ui.Text(c, L("Claude Code")).FontSize(13).FontWeight(600).Shrink(0)
			ui.Text(c, oneLine(r.Task, 80)).FontSize(13).TextColor(t.TextMuted).Grow(1).SingleLine().MinWidth(0)
			who := ""
			if ch.Kind == store.Group {
				who = a.name(r.Bot) + " · "
			}
			ui.Text(c, who+elapsed(r.Took())).FontSize(11).TextColor(t.TextMuted).Shrink(0)
		})
		if p := r.Progress(8); p != "" {
			codeBlock(c, p)
		}
		if s := r.Text(); s != "" {
			ui.Text(c, oneLine(s, 200)).FontSize(12).TextColor(t.TextMuted).MaxLines(2)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if smallButton(c.Key("cc-stop-"+r.ID), L("Stop"), danger).Clicked() {
				go r.Stop("stopped from Running tasks")
			}
		})
	})
}
