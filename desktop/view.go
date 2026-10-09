package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/store"
)

const (
	maxBubble   = 620
	composerMax = 8
)

// chosenGray is a chosen row's color unless the keyboard moved the choice.
var chosenGray = ui.RGBA(127, 127, 127, 0.2)

// listNav is a list whose chosen row shows in the accent color only while
// the keyboard moves the choice: a key sets keyboard, a click or losing the
// focus clears it.
type listNav struct {
	list     ui.ListState
	keyboard bool
	focus    bool // take the focus in the next frame
}

// rowColor is the background of a row: chosen, with the list focused.
func (n *listNav) rowColor(t *ui.Theme, chosen, focused bool) (bg ui.Color, accent bool) {
	if !chosen {
		return ui.Color{}, false
	}
	if focused && n.keyboard {
		return t.Accent, true
	}
	return chosenGray, false
}

// keys moves the choice of a list of n rows with the arrows, Home and End,
// calling choose; it reports whether one moved it.
func (n *listNav) keys(holder ui.Element, chosen, count int, choose func(int)) bool {
	at := -2
	switch {
	case holder.Shortcut(0, ui.KeyDown):
		at = chosen + 1
	case holder.Shortcut(0, ui.KeyUp):
		at = chosen - 1
	case holder.Shortcut(0, ui.KeyHome):
		at = 0
	case holder.Shortcut(0, ui.KeyEnd):
		at = count - 1
	}
	if at == -2 || count == 0 {
		return false
	}
	at = max(0, min(count-1, at))
	n.keyboard = true
	n.list.ScrollIntoView(at)
	choose(at)
	return true
}

// palette holds the authors' colors, one per bot in the order they were made.
var palette = []ui.Color{ui.Hex("#d9480f"), ui.Hex("#1971c2"), ui.Hex("#2b8a3e"), ui.Hex("#9c36b5"), ui.Hex("#0c8599"), ui.Hex("#c2255c"), ui.Hex("#5f3dc4"), ui.Hex("#a0522d")}

// authorColor is a bot's color: steady, and different for the first eight.
func (a *app) authorColor(id string) ui.Color {
	for i, b := range a.bots {
		if b.ID == id {
			return palette[i%len(palette)]
		}
	}
	return palette[0]
}

// root is the window: the chats beside the chat shown, and the sheets.
func (a *app) root(c *ui.Context) {
	t := c.Theme()
	a.shortcuts(c)
	if ui.Split(c, &a.sideWidth, func() { a.sidebar(c) }, func() { a.main(c) }).Fill().Background(t.Background).Changed() {
		a.layoutChanged()
	}
	a.sheets(c)
}

func (a *app) sidebar(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Surface).Children(func() {
		ui.Row(c).Padding(12, 12, 6, 14).Gap(6).Children(func() {
			ui.Text(c, L("Chats")).Bold().Grow(1)
			ui.Button(c.Key("new"), "+").Label(L("New")).Tooltip(L("New bot or group")).Menu(func(m *ui.Menu) {
				if m.Item(L("New Bot…")).Chosen() {
					a.newBot()
				}
				if m.Item(L("New Group…")).Disabled(len(a.bots) == 0).Chosen() {
					a.newGroup()
				}
			})
		})
		chosen := -1
		for i, ch := range a.chats {
			if ch.ID == a.sel {
				chosen = i
			}
		}
		holder := ui.Column(c.Key("chats")).Grow(1).MinHeight(0).Focusable().FocusRing(false).Label(L("Chats")).Role(ui.RoleList)
		if a.side.focus {
			holder.Focus()
			a.side.focus = false
		}
		focused := holder.FocusWithin()
		if !focused {
			a.side.keyboard = false
		}
		a.side.keys(holder, chosen, len(a.chats), func(i int) { a.sel = a.chats[i].ID; a.markRead() }) // the keys stay in the list
		holder.Children(func() {
			chats := a.chats
			a.side.list.Key = func(i int) any { return chats[i].ID }
			ui.List(c, &a.side.list, len(chats), func(i int) { a.chatRow(c, chats[i], i == chosen, focused) }).Grow(1).Padding(2, 8, 8, 8).Gap(2)
		})
		ui.Row(c).Padding(8, 12).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
			if ui.Button(c.Key("settings"), L("Settings")).Clicked() {
				a.settings = true
			}
		})
	})
}

func (a *app) chatRow(c *ui.Context, ch store.Chat, chosen, focused bool) {
	t := c.Theme()
	title := a.title(ch)
	row := ui.Row(c.Key(ch.ID)).Height(44).Padding(0, 8).Radius(8).Gap(10).Label(title).Role(ui.RoleListItem)
	bg, accent := a.side.rowColor(t, chosen, focused)
	sub := t.TextMuted
	if chosen {
		row.Background(bg)
	}
	if accent {
		row.TextColor(t.AccentText)
		sub = t.AccentText
	}
	if row.Pressed() || row.Clicked() {
		a.side.keyboard = false
		a.side.focus = true // the list takes the keys, the arrows move from here
		if !chosen {
			a.sel = ch.ID
			a.markRead()
		}
	}
	row.ContextMenu(func(m *ui.Menu) {
		if ch.Kind == store.DM {
			if m.Item(L("Edit Bot…")).Chosen() {
				a.editBot(ch.Owner)
			}
		} else if m.Item(L("Edit Group…")).Chosen() {
			a.editGroup(ch)
		}
		m.Separator()
		label := L("Delete Group")
		if ch.Kind == store.DM {
			label = L("Delete Bot")
		}
		if m.Item(label).Chosen() {
			a.deleteChat(ch)
		}
	})
	row.Children(func() {
		if ch.Kind == store.DM {
			a.avatar(c, ch.Owner, title, 12)
		} else {
			ui.Avatar(c, title, nil).FontSize(12)
		}
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, title).FontSize(13).FontWeight(500).SingleLine()
			status := ""
			switch {
			case a.busy[ch.ID]:
				status = L("Working…")
			case ch.Kind == store.Group:
				status = L("%d bots", len(ch.Members))
			}
			if status != "" {
				ui.Text(c, status).FontSize(11).TextColor(sub).SingleLine()
			}
		})
		if n := a.unread[ch.ID]; n > 0 && !chosen {
			ui.Badge(c, fmt.Sprint(n)).Label(L("%d unread", n))
		}
	})
}

func (a *app) main(c *ui.Context) {
	t := c.Theme()
	ch := a.chat(a.sel)
	if ch == nil {
		ui.Column(c).Fill().Justify(ui.Center).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, L("No chat yet.")).TextColor(t.TextMuted)
			if ui.PrimaryButton(c.Key("first-bot"), L("New Bot…")).Clicked() {
				a.newBot()
			}
		})
		return
	}
	v := a.view(ch.ID)
	ui.Row(c).Fill().Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Fill().Children(func() {
			a.header(c, *ch)
			a.transcript(c, *ch, v)
			a.approvalBar(c, *ch)
			a.composer(c, *ch, v)
		})
		if a.inspector {
			a.inspectorPane(c, *ch)
		}
	})
}

func (a *app) header(c *ui.Context, ch store.Chat) {
	t := c.Theme()
	ui.Row(c).Padding(10, 16).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, a.title(ch)).Bold().SingleLine()
			sub := ""
			if ch.Kind == store.Group {
				var names []string
				for _, id := range ch.Members {
					names = append(names, a.name(id))
				}
				sub = strings.Join(names, ", ")
			} else if b, ok := a.byID[ch.Owner]; ok {
				sub = firstLine(b.Description)
			}
			if v := a.views[ch.ID]; v != nil && v.working != "" && a.busy[ch.ID] {
				sub = L("%s is working…", a.name(v.working))
				if v.retrying != "" {
					sub = L("Retrying: %s", oneLine(v.retrying, 80))
				}
			}
			if sub != "" {
				ui.Text(c, sub).FontSize(12).TextColor(t.TextMuted).SingleLine()
			}
		})
		a.tasksButton(c, ch)
		if a.busy[ch.ID] {
			if ui.Button(c.Key("stop"), L("Stop")).Tooltip(L("Stop (Esc)")).Clicked() {
				a.stop()
			}
		}
		label := L("Info")
		if a.inspector {
			label = L("Hide Info")
		}
		if ui.Button(c.Key("info"), label).Clicked() {
			a.inspector = !a.inspector
			a.layoutChanged()
		}
	})
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return oneLine(s, 100)
}

func (a *app) transcript(c *ui.Context, ch store.Chat, v *chatView) {
	t := c.Theme()
	rows := v.rows
	ui.List(c, &v.list, len(rows), func(i int) { a.drawRow(c, ch, v, rows[i]) }).
		Grow(1).MinHeight(0).Padding(16, 20).Gap(10).Justify(ui.End).Children(func() {
		if len(rows) == 0 {
			hint := L("Ask %s for something.", a.title(ch))
			if ch.Kind == store.Group {
				hint = L("Write to the group. @Name asks a member first.")
			}
			ui.Text(c, hint).TextColor(t.TextMuted).Padding(12)
		}
	})
}

func (a *app) drawRow(c *ui.Context, ch store.Chat, v *chatView, r row) {
	t := c.Theme()
	group := ch.Kind == store.Group
	switch r.kind {
	case rowUser:
		ui.Column(c).AlignItems(ui.End).Gap(4).Children(func() {
			if r.from != "" {
				ui.Text(c, L("From %s, via message_bot", a.name(r.from))).FontSize(11).TextColor(t.TextMuted)
			}
			if r.quote != nil {
				quoteView(c, a.name(r.quote.Author), r.quote.Text)
			}
			bubble := ui.Column(c).Padding(8, 12).Radius(12).MaxWidth(maxBubble).Background(t.Accent).TextColor(t.AccentText).Gap(4)
			if r.from != "" {
				bubble.Background(t.Surface).TextColor(t.Text)
			}
			a.rowMenu(bubble, r)
			bubble.Children(func() {
				if r.text != "" {
					ui.Text(c, r.text).Selectable()
				}
				if r.images > 0 {
					ui.Text(c, L("%d image(s) attached", r.images)).FontSize(12).Italic()
				}
			})
		})
	case rowAssistant:
		col := ui.Column(c).Gap(4).MaxWidth(maxBubble)
		a.rowMenu(col, r)
		col.Children(func() {
			if group {
				ui.Text(c, a.name(r.author)).FontSize(12).Bold().TextColor(a.authorColor(r.author))
			}
			for _, s := range r.searches {
				color := t.TextMuted
				if s.err != "" {
					color = t.Danger
				}
				ui.Text(c, "⌕ "+searchText(s)).FontSize(12).TextColor(color).Selectable()
			}
			if r.thinking != "" && a.prefs.showThinking {
				ui.Text(c, strings.TrimSpace(r.thinking)).Selectable().Italic().FontSize(12).TextColor(t.TextMuted)
			}
			switch {
			case r.failed:
				ui.Text(c, r.text).Selectable().TextColor(t.Danger)
			case r.text != "":
				markdown(c, r.text)
			case !r.stopped && r.seq == 0 && len(r.searches) == 0:
				ui.Text(c, "…").TextColor(t.TextMuted)
			}
			if r.stopped {
				ui.Text(c, L("Stopped")).FontSize(12).TextColor(t.TextMuted)
			}
		})
	case rowTool:
		a.drawTool(c, ch, r)
	case rowNotice:
		ui.Text(c, r.text).Selectable().FontSize(12).TextColor(t.TextMuted).Padding(4, 0)
	}
}

// rowMenu gives a message Reply and the editing commands.
func (a *app) rowMenu(e ui.Element, r row) {
	e.Selectable().ContextMenu(func(m *ui.Menu) {
		if m.Item(L("Reply")).Disabled(r.seq == 0).Chosen() {
			a.replyTo(r)
		}
		m.Separator()
		m.EditItems()
	})
}

func quoteView(c *ui.Context, who, text string) {
	t := c.Theme()
	ui.Column(c).Padding(4, 10).BorderWidth(0, 0, 0, 3).BorderColor(t.Border).MaxWidth(maxBubble).Children(func() {
		ui.Text(c, who).FontSize(11).Bold().TextColor(t.TextMuted)
		ui.Text(c, oneLine(text, 160)).FontSize(12).TextColor(t.TextMuted).SingleLine()
	})
}

func (a *app) drawTool(c *ui.Context, ch store.Chat, r row) {
	t := c.Theme()
	color := t.TextMuted
	switch {
	case r.pending:
		color = t.Warning
	case r.result != nil && r.failed:
		color = t.Danger
	case r.result != nil:
		color = t.Success
	}
	ui.Column(c).Gap(4).Padding(8, 12).Radius(8).Background(t.Surface).MaxWidth(maxBubble).Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			if ch.Kind == store.Group {
				ui.Text(c, a.name(r.author)).FontSize(12).Bold().TextColor(a.authorColor(r.author))
			}
			ui.Text(c, r.call.Name).Bold().FontSize(12)
			ui.Text(c, toolStatus(r)).FontSize(12).TextColor(color)
		})
		if code, ok := scriptOf(r.call); ok {
			ui.Column(c).Padding(6, 8).Radius(6).Background(t.Background).Children(func() {
				ui.Text(c, tail(code, 12)).Font(monoFont).FontSize(11).Selectable()
			})
		} else {
			ui.Text(c, oneLine(string(r.call.Args), 200)).Selectable().FontSize(12).TextColor(t.TextMuted)
		}
		if r.pending {
			a.approvalButtons(c, ch.ID, r.call.CallID)
		}
		if r.result == nil && !r.pending {
			if sess := a.sessionOf(ch.ID, r.call); sess != nil {
				if last := sess.Last(8); strings.TrimSpace(last) != "" {
					ui.Column(c).Padding(6, 8).Radius(6).Background(t.Background).Children(func() {
						ui.Text(c, last).Font(monoFont).FontSize(11).Selectable()
					})
				}
			}
		}
		if r.result != nil {
			ui.Text(c, tail(r.result.Text, 12)).Selectable().FontSize(12)
		}
	})
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// tail keeps the last n lines.
func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return fmt.Sprintf("… %d lines above\n%s", len(lines)-n, strings.Join(lines[len(lines)-n:], "\n"))
}

// scriptOf is a codemode call's script.
func scriptOf(call agent.Part) (string, bool) {
	if call.Name != "codemode" {
		return "", false
	}
	var a struct{ Code string }
	if json.Unmarshal(call.Args, &a) != nil || a.Code == "" {
		return "", false
	}
	return strings.TrimSpace(a.Code), true
}
