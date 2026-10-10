package main

import (
	"errors"
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/memory"
	"github.com/spike/gobot/provider/connect"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/team"
)

func teamConfig(a *app, b store.Bot) (connect.Config, error) { return team.Config(a.st, b, a.fallback) }

// memoryForm edits a bot's MEMORY.md.
type memoryForm struct {
	open      bool
	bot       store.Bot
	was, text string
	err       string
	changed   bool // the file changed under the editor
}

func (a *app) memoryOf(b store.Bot) *memory.Memory { return memory.New(a.st.Workspace(b.ID)) }

// memoryDetails is the memory rows of a bot's inspector: how much of
// MEMORY.md loads, and Edit… and Show Folder.
func (a *app) memoryDetails(c *ui.Context, b store.Bot) {
	t := c.Theme()
	m := a.memoryOf(b)
	sectionTitle(c, L("Memory"))
	lines, bytes, cut := m.Loaded()
	status := L("Empty")
	if lines > 0 {
		status = L("%d lines, %.1f of %d KB load", lines, float64(bytes)/1024, memory.LoadBytes>>10)
	}
	if cut > 0 {
		status += " · " + L("%d lines past the budget", cut)
	}
	row := listRow(c.Key("edit-memory"), rowPlain).Label(L("Edit…"))
	if row.Clicked() {
		text := m.Read()
		a.memSheet = &memoryForm{open: true, bot: b, was: text, text: text}
	}
	row.Children(func() {
		ui.Icon(c, iconPin).FontSize(iconSize).TextColor(t.TextMuted).Shrink(0)
		ui.Text(c, "MEMORY.md").FontSize(13).Shrink(0)
		ui.Text(c, status).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
	})
}

func (a *app) memorySheetView(c *ui.Context) {
	f := a.memSheet
	m := a.memoryOf(f.bot)
	ui.Modal(c, &f.open, func() {
		ui.Column(c).Width(580).Gap(14).Children(func() {
			dialogTitle(c, L("%s's memory", f.bot.Name), L("MEMORY.md opens every turn of this bot. Credentials are redacted when saved."))
			ui.Box(c).Padding(8, 10).Radius(rowRadius).Background(pal(c).codeBg).Children(func() {
				ui.TextAreaBase(c, &f.text).Height(320).Font(monoFont).FontSize(12).LineHeight(1.45).Label(L("Memory"))
			})
			if f.err != "" {
				ui.Text(c, f.err).FontSize(12).TextColor(pal(c).red)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if button(c, L("Cancel")).Clicked() {
					f.open = false
				}
				if f.changed {
					if button(c, L("Reload")).Clicked() {
						f.was, f.text = m.Read(), m.Read()
						f.changed, f.err = false, ""
					}
					if button(c, L("Overwrite")).Clicked() {
						f.was = m.Read()
						f.changed = false
					}
				}
				if primaryButton(c, L("Save"), false).Clicked() {
					err := m.Save(f.was, f.text)
					switch {
					case errors.Is(err, memory.ErrChanged):
						f.changed, f.err = true, L("The bot changed its memory while you edited. Reload to see it, or Overwrite.")
					case err != nil:
						f.err = err.Error()
					default:
						f.open = false
					}
				}
			})
		})
	})
}

// spent is what a chat's replies cost, from the catalog's prices.
func spent(es []store.Entry) float64 {
	total := 0.0
	for _, e := range es {
		if e.Msg.Role == agent.RoleAssistant {
			total += e.Msg.Usage.Cost
		}
	}
	return total
}

func money(d float64) string {
	if d < 0.01 && d > 0 {
		return "<$0.01"
	}
	return fmt.Sprintf("$%.2f", d)
}
