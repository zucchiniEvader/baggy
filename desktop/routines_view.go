package main

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/schedule"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/team"
)

// routineSheet shows one routine.
type routineSheet struct {
	open bool
	id   string
}

// routineLine is a routine's schedule in words and its next run, or why it
// is not running.
func routineLine(r store.Routine, now time.Time) string {
	sc, err := schedule.Parse(r.Schedule, r.Timezone)
	if err != nil {
		return L("Schedule no longer reads: %s", err.Error())
	}
	line := sc.Text()
	switch {
	case !r.Enabled && r.PausedReason == "away":
		return line + " · " + L("Paused while you were away")
	case !r.Enabled:
		return line + " · " + L("Paused")
	}
	if n, err := team.NextRun(r); err == nil {
		if n.Before(now) {
			n = now
		}
		line += " · " + L("next %s", n.Local().Format("Mon 15:04"))
	}
	return line
}

func outcomeText(o string) string {
	switch o {
	case store.OutcomeSent:
		return L("replied")
	case store.OutcomePass:
		return L("nothing to report")
	case store.OutcomeError:
		return L("failed")
	}
	return ""
}

// routinesDetails is the Routines section of a bot's inspector.
func (a *app) routinesDetails(c *ui.Context, b store.Bot) {
	t := c.Theme()
	rs, _ := a.st.Routines(b.ID)
	inspectorSection(c, L("Routines"), func() {
		if len(rs) == 0 {
			ui.Text(c, L("Ask %s in chat to set one up, such as a morning brief.", b.Name)).FontSize(12).LineHeight(1.45).TextColor(t.TextMuted).Padding(0, inspectorInset)
			return
		}
		now := time.Now()
		ui.Column(c).Gap(1).Children(func() {
			for _, r := range rs {
				r := r
				row := ui.Row(c.Key("routine-"+r.ID)).Height(40).Gap(8).AlignItems(ui.Center).Padding(0, 4, 0, 8).Radius(rowRadius).Label(r.Name)
				styleRow(row, t, rowPlain)
				if row.Clicked() {
					a.routine = &routineSheet{open: true, id: r.ID}
				}
				row.Children(func() {
					ui.Box(c).Size(20, 20).Center().Shrink(0).Children(func() { ui.Icon(c, iconClock).FontSize(iconSize).TextColor(t.TextMuted) })
					ui.Column(c).Grow(1).MinWidth(0).Children(func() {
						ui.Text(c, r.Name).FontSize(13).SingleLine().MinWidth(0)
						ui.Text(c, routineLine(r, now)).FontSize(11).TextColor(t.TextMuted).SingleLine().MinWidth(0)
					})
					on := r.Enabled
					if ui.Switch(c.Key("on-"+r.ID), &on).Label(L("On")).Changed() {
						a.setRoutineOn(r, on)
					}
				})
			}
		})
	})
}

func (a *app) setRoutineOn(r store.Routine, on bool) {
	r.Enabled, r.PausedReason = on, ""
	if on { // counts from now
		r.LastScheduledAt, r.EnabledAt = 0, time.Now().UnixMilli()
	}
	_, err := a.st.SaveRoutine(r)
	a.do(err)
}

func (a *app) routineSheetView(c *ui.Context) {
	t := c.Theme()
	f := a.routine
	r, err := a.st.Routine(f.id)
	if err != nil {
		f.open = false
		return
	}
	ui.Modal(c, &f.open, func() {
		dialogBody(c, 520, 14).Children(func() {
			dialogTitle(c, r.Name, routineLine(r, time.Now()))
			tz := r.Timezone
			if tz == "" {
				tz = schedule.LocalZone()
			}
			ui.Form(c, func() {
				ui.Field(c, L("Schedule"), func() { ui.Text(c, r.Schedule+" · "+tz).Selectable() })
				ui.Field(c, L("Missed runs"), func() {
					label := L("Run once")
					if r.Missed == store.Skip {
						label = L("Skip")
					}
					ui.Text(c, label)
				})
				if r.LastRunAt > 0 {
					ui.Field(c, L("Last run"), func() {
						ui.Text(c, time.UnixMilli(r.LastRunAt).Format("Jan 2 15:04")+" · "+outcomeText(r.LastOutcome))
					})
				}
				if r.LastCheckAt > 0 {
					ui.Field(c, L("Last check"), func() { ui.Text(c, time.UnixMilli(r.LastCheckAt).Format("Jan 2 15:04")+" · "+r.LastCheck) })
				}
			})
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, L("Task")).FontSize(11).FontWeight(600).TextColor(t.TextMuted)
				ui.Text(c, r.Prompt).FontSize(13).LineHeight(1.5).Selectable()
			})
			if r.Check != "" {
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, L("Check")).FontSize(11).FontWeight(600).TextColor(t.TextMuted)
					codeBlock(c, r.Check)
				})
			}
			ui.Row(c).Gap(8).Children(func() {
				if textButton(c, L("Delete"), danger, false).Clicked() {
					f.open = false
					id, name := r.ID, r.Name
					a.ask(L("Delete %s?", name), L("The routine stops running. Its past runs stay in the chat."), L("Delete"), func() { a.do(a.st.DeleteRoutine(id)) })
				}
				ui.Spacer(c)
				if button(c, L("Edit in Chat")).Clicked() {
					f.open = false
					if dm, err := a.st.DMOf(r.BotID); err == nil {
						a.selectChat(dm.ID)
						a.view(dm.ID).draft = L("Edit my routine \"%s\": ", r.Name)
					}
				}
				pause := L("Pause")
				if !r.Enabled {
					pause = L("Resume")
				}
				if button(c, pause).Clicked() {
					a.setRoutineOn(r, !r.Enabled)
				}
				if primaryButton(c, L("Run Now"), false).Clicked() {
					f.open = false
					if err := a.team.RunRoutine(r); err != nil {
						a.do(err)
					}
				}
			})
		})
	})
}
