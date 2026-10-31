package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/host"
	"github.com/zucchiniEvader/baggy/store"
)

// taskDelay is how long a command runs before it counts as a task, so a
// quick ls never shows.
const taskDelay = 2 * time.Second

// sessionOf is the running session of a shell card: the chat's newest one
// running its command.
func (a *app) sessionOf(chatID string, call agent.Part) *host.Shell {
	if call.Name != "shell" {
		return nil
	}
	var args struct{ Command string }
	json.Unmarshal(call.Args, &args)
	running := a.rt.Tasks(chatID).Shells
	for i := len(running) - 1; i >= 0; i-- {
		if running[i].Command == args.Command {
			return &running[i]
		}
	}
	return nil
}

// tasks are a chat's commands that have run a while.
func (a *app) tasks(all host.Tasks) []host.Shell {
	var out []host.Shell
	for _, s := range all.Shells {
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
	all := a.rt.Tasks(ch.ID)
	ts := a.tasks(all)
	runs := all.Claude
	ds := all.Delegations
	n := len(ts) + len(runs)
	for _, d := range ds {
		n += len(d.Tasks)
	}
	if n == 0 {
		a.tasksOpen = false
		return
	}
	btn := ui.ButtonBase(c.Key("tasks")).Height(compactHeight).Padding(0, 8).Gap(5).Radius(rowRadius).AlignItems(ui.Center).Label(L("Tasks (%d)", n)).Tooltip(L("Commands, delegated tasks and Claude Code running in this chat")).Shrink(0)
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
			for _, d := range ds {
				a.delegationTask(c, ch, d)
			}
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
					if s.Last != "" {
						codeBlock(c, s.Last)
					}
					ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
						if !s.Background {
							if smallButton(c.Key("bg-"+s.ID), L("Run in Background"), secondary).Clicked() {
								a.rt.BackgroundShell(s.ID)
							}
						}
						if smallButton(c.Key("stop-"+s.ID), L("Stop"), danger).Clicked() {
							go a.rt.StopShell(s.ID, L("stopped from Running tasks"))
						}
					})
				})
			}
		})
	})
}

// delegationTask is a delegation in Running tasks: who handed it out, each
// task with its state and last steps, and Stop.
func (a *app) delegationTask(c *ui.Context, ch store.Chat, d host.Delegation) {
	t := c.Theme()
	p := pal(c)
	ui.Column(c.Key("dlg-" + d.ID)).Gap(6).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconUsers).FontSize(iconSize).TextColor(t.TextMuted).Shrink(0)
			ui.Text(c, L("Delegated by %s", a.name(d.Bot))).FontSize(13).FontWeight(600).Grow(1).SingleLine().MinWidth(0)
			ui.Text(c, elapsed(time.Since(d.Started))).FontSize(11).TextColor(t.TextMuted).Shrink(0)
		})
		for i, k := range d.Tasks {
			state := k.State
			ui.Column(c.Key(i)).Gap(4).Padding(0, 0, 0, 22).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					switch state {
					case "working":
						ui.Spinner(c).Size(12, 12).Shrink(0)
					case "done":
						ui.Icon(c, iconCheck).FontSize(12).TextColor(p.greenBar).Shrink(0)
					default:
						ui.Icon(c, iconXCircle).FontSize(12).TextColor(p.redBar).Shrink(0)
					}
					ui.Text(c, k.Title).FontSize(13).SingleLine().Shrink(1).MinWidth(0)
					if k.Bot != d.Bot {
						ui.Text(c, L("as %s", a.name(k.Bot))).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1000).MinWidth(0)
					}
				})
				if state == "working" {
					if len(k.Steps) > 0 {
						codeBlock(c, strings.Join(k.Steps, "\n"))
					}
				}
			})
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if smallButton(c.Key("dlg-stop-"+d.ID), L("Stop"), danger).Clicked() {
				go a.rt.StopDelegation(d.ID)
			}
		})
	})
}

// claudeTask is a Claude Code task in Running tasks: what it was given, its
// recent steps, what it said last, and Stop.
func (a *app) claudeTask(c *ui.Context, ch store.Chat, r host.ClaudeRun) {
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
			ui.Text(c, who+elapsed(r.Took)).FontSize(11).TextColor(t.TextMuted).Shrink(0)
		})
		if r.Progress != "" {
			codeBlock(c, r.Progress)
		}
		if s := r.Text; s != "" {
			ui.Text(c, oneLine(s, 200)).FontSize(12).TextColor(t.TextMuted).MaxLines(2)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if smallButton(c.Key("cc-stop-"+r.ID), L("Stop"), danger).Clicked() {
				go a.rt.StopClaude(r.ID, "stopped from Running tasks")
			}
		})
	})
}
