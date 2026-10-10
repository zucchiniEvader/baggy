package main

import (
	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/store"
)

// inspectorPane is the chat's details beside it: a DM's bot, or a group's
// name, description and members, with their owner.
func (a *app) inspectorPane(c *ui.Context, ch store.Chat) {
	p := pal(c)
	ui.Column(c).Width(inspectorWidth).Shrink(0).Background(p.appBg).BorderWidth(0, 0, 0, 1).BorderColor(p.border).Children(func() {
		ui.Row(c).Height(titleBarHeight).Shrink(0).DragWindow()
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).Padding(0, 16, 16).Gap(2).Children(func() {
				if ch.Kind == store.DM {
					a.botDetails(c, ch.Owner)
					return
				}
				a.groupDetails(c, ch)
			})
		})
	})
}

func (a *app) groupDetails(c *ui.Context, ch store.Chat) {
	t := c.Theme()
	ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(0, 0, 8).Children(func() {
		iconTile(c, iconUsers, 32)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, ch.Title).FontSize(15).Bold().SingleLine().MinWidth(0)
			ui.Text(c, L("%d bots", len(ch.Members))).FontSize(12).TextColor(t.TextMuted)
		})
	})
	if ch.Description != "" {
		ui.Text(c, ch.Description).FontSize(12).LineHeight(1.5).Selectable()
	}
	ui.Row(c).Gap(6).Padding(8, 0, 0).Children(func() {
		if smallButton(c.Key("edit-group"), L("Edit…"), secondary).Clicked() {
			a.editGroup(ch)
		}
	})
	sectionTitle(c, L("Members"))
	ui.Column(c).Gap(1).Children(func() {
		for _, id := range ch.Members {
			a.memberRow(c, ch, id)
		}
	})
	var outside []store.Bot
	for _, b := range a.bots {
		if !member(ch, b.ID) {
			outside = append(outside, b)
		}
	}
	var action func()
	row := listRow(c.Key("add-member"), rowPlain).Label(L("Add Bot")).Disabled(len(outside) == 0 || len(ch.Members) >= store.MaxMembers)
	row.Menu(func(m *ui.Menu) {
		for _, b := range outside {
			b := b
			if m.Item(b.Name).Chosen() {
				action = func() { a.do(a.st.AddMember(ch.ID, b.ID)) }
			}
		}
	})
	row.Children(func() {
		ui.Icon(c, iconPlus).FontSize(iconSize).TextColor(t.TextMuted)
		ui.Text(c, L("Add Bot")).FontSize(13).TextColor(t.TextMuted)
	})
	if action != nil {
		action()
	}
	sectionTitle(c, L("Usage"))
	a.spentRow(c, ch)
	ui.Row(c).Padding(16, 0, 0).Children(func() {
		if smallButton(c.Key("delete-group"), L("Delete Group"), danger).Clicked() {
			a.deleteChat(ch)
		}
	})
}

func member(c store.Chat, id string) bool {
	for _, m := range c.Members {
		if m == id {
			return true
		}
	}
	return false
}

func (a *app) memberRow(c *ui.Context, ch store.Chat, id string) {
	t := c.Theme()
	name := a.name(id)
	var action func()
	menu := func(m *ui.Menu) {
		if m.Item(L("Make Owner")).Disabled(ch.Owner == id).Chosen() {
			action = func() { a.do(a.st.SetOwner(ch.ID, id)) }
		}
		if m.Item(L("Edit Bot…")).Chosen() {
			action = func() { a.editBot(id) }
		}
		m.Separator()
		if m.Item(L("Remove from Chat")).Chosen() {
			action = func() {
				if len(ch.Members) == 1 {
					a.deleteChat(ch)
				} else {
					a.do(a.st.RemoveMember(ch.ID, id))
				}
			}
		}
	}
	row := ui.Row(c.Key("member-"+id)).Height(rowHeight).Padding(0, 4, 0, 8).Gap(8).Radius(rowRadius).AlignItems(ui.Center).Label(name)
	styleRow(row, t, rowPlain)
	row.ContextMenu(menu)
	row.Children(func() {
		ui.Icon(c, botIcon(a.byID[id].Icon)).FontSize(iconSize).TextColor(t.TextMuted).Shrink(0)
		ui.Text(c, name).FontSize(13).SingleLine().Grow(1).MinWidth(0)
		if ch.Owner == id {
			badge(c, L("Owner"))
		}
		iconButton(c.Key("more-"+id), iconMore, L("More")).Menu(menu)
	})
	if action != nil {
		action()
	}
}

func (a *app) botDetails(c *ui.Context, id string) {
	t := c.Theme()
	b, ok := a.byID[id]
	if !ok {
		return
	}
	ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(0, 0, 8).Children(func() {
		a.avatar(c, b.ID, 32)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, b.Name).FontSize(15).Bold().SingleLine().MinWidth(0)
			model := b.Model
			if cfg, err := teamConfig(a, b); err == nil {
				model = cfg.Model
			}
			ui.Text(c, a.providerName(b.Provider)+" · "+model).FontSize(12).TextColor(t.TextMuted).SingleLine().MinWidth(0)
		})
	})
	if b.Description != "" {
		ui.Text(c, b.Description).FontSize(12).LineHeight(1.5).Selectable()
	}
	ui.Row(c).Gap(6).Padding(8, 0, 0).Children(func() {
		if smallButton(c.Key("edit-bot"), L("Edit…"), secondary).Clicked() {
			a.editBot(id)
		}
		if a.openFolder != nil {
			if smallButton(c.Key("show-folder"), L("Show Folder"), secondary).Tooltip(a.st.Workspace(b.ID)).Clicked() {
				a.openFolder(a.st.Workspace(b.ID))
			}
		}
	})
	a.routinesDetails(c, b)
	a.memoryDetails(c, b)
	if dm, err := a.st.DMOf(id); err == nil {
		sectionTitle(c, L("Usage"))
		a.spentRow(c, dm)
		ui.Row(c).Padding(16, 0, 0).Children(func() {
			if smallButton(c.Key("delete-bot"), L("Delete Bot"), danger).Clicked() {
				a.deleteChat(dm)
			}
		})
	}
}

func (a *app) spentRow(c *ui.Context, ch store.Chat) {
	t := c.Theme()
	if es, err := a.st.Messages(ch.ID, 0); err == nil {
		ui.Text(c, L("Spent in this chat: %s", money(spent(es)))).FontSize(12).TextColor(t.TextMuted)
	}
	if line := a.contextLine(ch.ID); line != "" {
		ui.Text(c, line).FontSize(12).TextColor(t.TextMuted)
	}
}
