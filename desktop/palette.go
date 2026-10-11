package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/baggy/store"
)

// paletteState is the command palette: a field filtering the chats and the
// commands, the arrows moving among them, Enter running one.
type paletteState struct {
	open  bool
	query string
	at    int
}

type command struct {
	label string
	run   func()
}

func (a *app) commands() []command {
	var cs []command
	for _, ch := range a.chats {
		id := ch.ID
		cs = append(cs, command{L("Go to %s", a.title(ch)), func() { a.selectChat(id) }})
	}
	cs = append(cs,
		command{L("New Bot…"), a.newFromTemplate},
		command{L("New Group…"), a.newGroup},
		command{L("Settings…"), func() { a.openSettings("") }},
		command{L("Toggle Info"), func() { a.inspector = !a.inspector }},
	)
	if ch := a.chat(a.sel); ch != nil {
		c := *ch
		if a.busy[c.ID] {
			cs = append(cs, command{L("Stop"), a.stop})
		}
		if c.Kind == store.DM {
			cs = append(cs, command{L("Edit Bot…"), func() { a.editBot(c.Owner) }})
		} else {
			cs = append(cs, command{L("Edit Group…"), func() { a.editGroup(c) }})
		}
	}
	return cs
}

func matchCommands(cs []command, q string) []command {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return cs
	}
	var out []command
	for _, c := range cs {
		if strings.Contains(strings.ToLower(c.label), q) {
			out = append(out, c)
		}
	}
	return out
}

func (a *app) openPalette() { a.palette = &paletteState{open: true} }

func (a *app) paletteView(c *ui.Context) {
	t := c.Theme()
	p := a.palette
	cs := matchCommands(a.commands(), p.query)
	p.at = max(0, min(len(cs)-1, p.at))
	run := func(i int) {
		a.palette = nil
		cs[i].run()
	}
	ui.Modal(c, &p.open, func() {
		ui.Column(c).Width(480).Gap(8).Children(func() {
			var in ui.Element
			fieldBox(c.Key("palette-box"), func(box ui.Element) {
				ui.Icon(c, iconSearch).FontSize(13).TextColor(t.TextMuted)
				in = fieldInput(c.Key("palette"), box, &p.query, L("Type a command or a chat")).AutoFocus()
			})
			if in.Changed() {
				p.at = 0
			}
			in.HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind != ui.InputKeyDown {
					return false
				}
				switch ev.Key {
				case ui.KeyDown:
					p.at = min(len(cs)-1, p.at+1)
				case ui.KeyUp:
					p.at = max(0, p.at-1)
				case ui.KeyEnter:
					if len(cs) > 0 {
						run(p.at)
					}
				default:
					return false
				}
				return true
			})
			ui.Column(c).Gap(1).MaxHeight(360).Children(func() {
				for i, cmd := range cs {
					st := rowPlain
					if i == p.at {
						st = rowKeyboard // the palette owns the keyboard
					}
					item := listRow(c.Key("cmd-"+cmd.label), st).Label(cmd.label)
					if item.Clicked() {
						run(i)
						return
					}
					item.Children(func() { ui.Text(c, cmd.label).FontSize(13).SingleLine().MinWidth(0) })
				}
				if len(cs) == 0 {
					ui.Text(c, L("Nothing matches.")).TextColor(t.TextMuted).Padding(6, 10)
				}
			})
		})
	})
	if a.palette != nil && !p.open {
		a.palette = nil
	}
}

var chatKeys = []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9}

// shortcuts are the window's keys: Cmd+K the palette, Cmd+N a new bot,
// Cmd+, Settings, Cmd+1–9 a chat, Escape stops the chat shown.
func (a *app) shortcuts(c *ui.Context) {
	if a.hasSheet() {
		return
	}
	switch {
	case c.Shortcut(ui.Cmd, ui.KeyK):
		a.openPalette()
	case c.Shortcut(ui.Cmd, ui.KeyN):
		a.newFromTemplate()
	case c.Shortcut(ui.Cmd, ui.KeyComma):
		a.openSettings("")
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyB):
		a.toggleSidebar()
	case c.Shortcut(ui.Cmd, ui.KeyF):
		a.sidebarShown, a.filterFocus = true, true
	case c.Shortcut(0, ui.KeyEscape) && a.busy[a.sel]:
		a.stop()
	}
	for i, k := range chatKeys {
		if i < len(a.chats) && c.Shortcut(ui.Cmd, k) {
			a.selectChat(a.chats[i].ID)
		}
	}
}
