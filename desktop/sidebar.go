package main

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/baggy/store"
)

// listNav is a list whose chosen row shows in the accent only while the
// keyboard moves the choice: a key sets keyboard, a click or losing the
// focus clears it.
type listNav struct {
	list     ui.ListState
	keyboard bool
	focus    bool // take the focus in the next frame
	reveal   bool // scroll the chosen row into view once
}

// keys moves the choice of a list of count rows with the arrows, Home and
// End, calling choose; it reports whether one moved it.
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
	n.keyboard, n.reveal = true, true
	choose(at)
	return true
}

// state is how a row of the list is chosen.
func (n *listNav) state(chosen, focused bool) rowState {
	switch {
	case !chosen:
		return rowPlain
	case focused && n.keyboard:
		return rowKeyboard
	}
	return rowChosen
}

// sidebarPane is the sidebar as it slides open and closed: a clip whose
// width moves, over the sidebar at its full width.
func (a *app) sidebarPane(c *ui.Context) {
	target := float32(0)
	if a.sidebarShown {
		target = a.sideWidth
	}
	d := 200 * time.Millisecond
	if a.resizing || a.reduceMotion(c) {
		d = 0
	}
	pane := ui.Column(c).Shrink(0).Clip()
	width := pane.AnimateWith("width", target, d, ui.EaseInOut)
	pane.Width(width)
	if width <= 0 {
		return
	}
	pane.Children(func() {
		ui.Column(c).Absolute().Top(0).Bottom(0).Left(width - a.sideWidth).Width(a.sideWidth).Children(func() { a.sidebar(c) })
	})
}

func (a *app) reduceMotion(c *ui.Context) bool { return c.Preferences().ReduceMotion }

func (a *app) toggleSidebar() {
	a.sidebarShown = !a.sidebarShown
	if a.sidebarShown && a.sideWidth < sidebarMin {
		a.sideWidth = sidebarDefault
	}
	a.dragWidth = a.sideWidth
}

func (a *app) sidebarToggle(c *ui.Context) {
	tip := L("Hide Sidebar (⌘⇧B)")
	if !a.sidebarShown {
		tip = L("Show Sidebar (⌘⇧B)")
	}
	if iconButton(c, iconSidebar, tip).Clicked() {
		a.toggleSidebar()
	}
}

// newMenu is New: a bot or a group, from the native menu.
func (a *app) newMenu(c *ui.Context) {
	var action func()
	iconButton(c, iconNewChat, L("New bot or group")).Menu(func(m *ui.Menu) {
		if m.Item(L("New Bot…")).Shortcut(ui.Cmd, ui.KeyN).Chosen() {
			action = a.newFromTemplate
		}
		if m.Item(L("New Group…")).Disabled(len(a.bots) == 0).Chosen() {
			action = a.newGroup
		}
	})
	if action != nil {
		action()
	}
}

func (a *app) sidebar(c *ui.Context) {
	t := c.Theme()
	bar := c.TitleBar()
	ui.Column(c).Fill().Background(a.sidebarBg(c)).Children(func() {
		// The title bar: the toggle and New keep to the right of the traffic
		// lights on macOS, to the left of the window buttons elsewhere.
		mac := runtime.GOOS == "darwin"
		row := ui.Row(c).Height(titleBarHeight).Shrink(0).Gap(2).AlignItems(ui.Center).DragWindow().Padding(0, sidebarPad, 0, max(bar.Left+6, sidebarPad))
		row.Children(func() {
			if mac {
				ui.Spacer(c)
			}
			a.sidebarToggle(c)
			a.newMenu(c)
			if !mac {
				ui.Spacer(c)
			}
		})
		ui.Column(c).Shrink(0).Padding(0, sidebarPad, 6).Children(func() {
			searchField(c, &a.filter, L("Search chats"), &a.filterFocus)
		})
		ui.Scroll(c).Grow(1).MinHeight(0).Padding(0, sidebarPad, sidebarPad).Children(func() { a.chatList(c) })
		ui.Row(c).Shrink(0).Height(titleBarHeight).Padding(0, sidebarPad).Gap(2).AlignItems(ui.Center).Children(func() {
			if iconButton(c, iconSettings, L("Settings (⌘,)")).Clicked() {
				a.openSettings("")
			}
			ui.Spacer(c)
			if !a.hasProvider() {
				ui.Text(c, L("No model set up")).FontSize(11).TextColor(pal(c).orange).SingleLine().Shrink(1).MinWidth(0)
			}
		})
	})
	_ = t
}

func (a *app) hasProvider() bool {
	ps, _ := a.st.Providers()
	return len(ps) > 0 || a.fallback.APIKey != ""
}

// shownChats are the chats the list shows, filtered by the search: the
// pinned ones first, in the order they were pinned, then the rest by their
// last activity.
func (a *app) shownChats() []store.Chat {
	q := strings.ToLower(strings.TrimSpace(a.filter))
	var pinned, rest []store.Chat
	for _, ch := range a.chats {
		if q != "" && !strings.Contains(strings.ToLower(a.title(ch)), q) && !strings.Contains(strings.ToLower(a.previews[ch.ID].Text), q) {
			continue
		}
		if ch.Pinned != 0 {
			pinned = append(pinned, ch)
		} else {
			rest = append(rest, ch)
		}
	}
	sort.SliceStable(pinned, func(i, j int) bool { return pinned[i].Pinned < pinned[j].Pinned })
	return append(pinned, rest...)
}

// chatGroup names when a chat was last active, as Today.
func chatGroup(now, t time.Time) string {
	days := daysBetween(t, now)
	switch {
	case days <= 0:
		return L("Today")
	case days == 1:
		return L("Yesterday")
	case days < 7:
		return L("Previous 7 Days")
	case days < 30:
		return L("Previous 30 Days")
	}
	return L("Earlier")
}

func daysBetween(t, now time.Time) int {
	day := func(t time.Time) time.Time { y, m, d := t.Date(); return time.Date(y, m, d, 0, 0, 0, 0, t.Location()) }
	return int(day(now).Sub(day(t)).Hours() / 24)
}

// stamp is when a chat was last active, as a row says it: the time today,
// Yesterday, the weekday this week, else the date.
func stamp(now, t time.Time) string {
	switch days := daysBetween(t, now); {
	case days <= 0:
		return t.Format("15:04")
	case days == 1:
		return L("Yesterday")
	case days < 7:
		return L(t.Weekday().String()[:3])
	case t.Year() == now.Year():
		return fmt.Sprintf("%d/%d", t.Month(), t.Day())
	}
	return fmt.Sprintf("%d/%d/%d", t.Year()%100, t.Month(), t.Day())
}

// previewLine is a chat's last words with who said them, when that is not
// plain: "You: …" for the user, "Name: …" for a bot in a group.
func (a *app) previewLine(ch store.Chat) string {
	p, ok := a.previews[ch.ID]
	if !ok {
		return ""
	}
	switch {
	case p.Author == store.User:
		return L("You: %s", p.Text)
	case ch.Kind == store.Group:
		return a.name(p.Author) + ": " + p.Text
	}
	return p.Text
}

func (a *app) togglePin(ch store.Chat) {
	a.do(a.st.SetPinned(ch.ID, ch.Pinned == 0))
}

// chatList lists the chats. It takes the focus as a whole, its rows do
// not: the arrows, Home and End move the choice, Enter goes on to the
// composer.
func (a *app) chatList(c *ui.Context) {
	t := c.Theme()
	list := a.shownChats()
	box := ui.Column(c.Key("chats")).Gap(1).Focusable().FocusRing(false).Label(L("Chats")).Role(ui.RoleList)
	if a.side.focus {
		box.Focus()
		a.side.focus = false
	}
	focused := box.FocusWithin()
	if !focused {
		a.side.keyboard = false
	}
	chosen := -1
	for i, ch := range list {
		if ch.ID == a.sel {
			chosen = i
		}
	}
	a.side.keys(box, chosen, len(list), func(i int) { a.sel = list[i].ID; a.markRead() }) // the keys stay in the list
	if box.Shortcut(0, ui.KeyEnter) && a.sel != "" {
		a.view(a.sel).focus = true
	}
	now := time.Now()
	group := ""
	var action func()
	box.Children(func() {
		for _, ch := range list {
			g := L("Pinned")
			if ch.Pinned == 0 {
				g = chatGroup(now, time.UnixMilli(ch.Updated))
			}
			if g != group {
				group = g
				ui.Text(c, g).Key("g:"+g).FontSize(11).FontWeight(600).TextColor(t.TextMuted).Padding(10, 8, 4)
			}
			ch := ch
			st := a.side.state(ch.ID == a.sel, focused)
			title := a.title(ch)
			// A plain row, not a button: a click leaves the focus with the
			// list, so the arrows go on from there.
			row := ui.Row(c.Key(ch.ID)).Height(chatRowHeight).Padding(0, 8, 0, 8).Gap(8).Radius(rowRadius).AlignItems(ui.Center).Label(title).Role(ui.RoleListItem)
			styleRow(row, t, st)
			if row.Pressed() || row.Clicked() {
				a.side.keyboard = false
				a.side.focus = true
				if ch.ID != a.sel {
					action = func() { a.sel = ch.ID; a.markRead() }
				}
			}
			if st != rowPlain && a.side.reveal {
				row.ScrollIntoView()
				a.side.reveal = false
			}
			row.ContextMenu(func(m *ui.Menu) {
				pin := L("Pin")
				if ch.Pinned != 0 {
					pin = L("Unpin")
				}
				if m.Item(pin).Chosen() {
					action = func() { a.togglePin(ch) }
				}
				m.Separator()
				if ch.Kind == store.DM {
					if m.Item(L("Edit Bot…")).Chosen() {
						action = func() { a.editBot(ch.Owner) }
					}
				} else if m.Item(L("Edit Group…")).Chosen() {
					action = func() { a.editGroup(ch) }
				}
				m.Separator()
				label := L("Delete Group")
				if ch.Kind == store.DM {
					label = L("Delete Bot")
				}
				if m.Item(label).Chosen() {
					action = func() { a.deleteChat(ch) }
				}
			})
			onAccent := st == rowKeyboard
			row.Children(func() {
				a.chatAvatar(c, ch, chatAvatarSize, pal(c).sidebar)
				ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Text(c, title).FontSize(13).SingleLine().Grow(1).MinWidth(0)
						if p, ok := a.previews[ch.ID]; ok {
							muted(ui.Text(c, stamp(now, time.UnixMilli(p.At))).FontSize(11).SingleLine().Shrink(0), t, onAccent)
						}
					})
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						line := a.previewLine(ch)
						if line == "" {
							line = L("No messages yet")
						}
						muted(ui.Text(c, line).FontSize(12).SingleLine().Grow(1).MinWidth(0), t, onAccent)
						switch {
						case a.busy[ch.ID]:
							ui.Spinner(c).Size(12, 12).Shrink(0)
						case a.unread[ch.ID] > 0 && ch.ID != a.sel:
							badge(c, fmt.Sprint(a.unread[ch.ID])).Label(L("%d unread", a.unread[ch.ID]))
						}
					})
				})
			})
		}
		if len(list) == 0 {
			ui.Text(c, L("No chats match")).FontSize(12).TextColor(t.TextMuted).Padding(16, 8)
		}
	})
	if action != nil {
		action()
	}
}

// muted gives secondary text of a row its color, unless the row shows in
// the accent.
func muted(e ui.Element, t *ui.Theme, onAccent bool) ui.Element {
	if !onAccent {
		e.TextColor(t.TextMuted)
	}
	return e
}

// sidebarResizer is the sidebar's inner edge, which the user drags. It draws
// nothing: the sidebar's material sets it apart from the chat.
func (a *app) sidebarResizer(c *ui.Context) {
	ui.Box(c).Width(0).Shrink(0).Children(func() {
		handle := ui.Box(c).Absolute().Top(0).Bottom(0).Left(-4).Width(8).Cursor(ui.CursorResizeEW)
		dx, _, held := handle.Dragged()
		if a.resizing && !held {
			a.layoutChanged()
		}
		a.resizing = held
		if !held {
			a.dragWidth = a.sideWidth
			return
		}
		a.dragWidth += dx
		if a.dragWidth < sidebarCollapse {
			a.sidebarShown = false
		} else {
			a.sidebarShown = true
			a.sideWidth = min(max(a.dragWidth, sidebarMin), sidebarMax)
		}
	})
}
