package main

import (
	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/store"
)

// inspectorPane is the chat's details beside it: a DM's bot, or a group's
// name, description and members, with their owner.
func (a *app) inspectorPane(c *ui.Context, ch store.Chat) {
	t := c.Theme()
	ui.Scroll(c).Width(260).Shrink(0).FillHeight().Background(t.Surface).BorderWidth(0, 0, 0, 1).BorderColor(t.Border).Children(func() {
		ui.Column(c).Padding(14).Gap(10).AlignItems(ui.Start).Children(func() {
			if ch.Kind == store.DM {
				a.botDetails(c, ch.Owner)
				return
			}
			ui.Text(c, L("Group")).FontSize(11).Bold().TextColor(t.TextMuted)
			ui.Text(c, ch.Title).Bold()
			if ch.Description != "" {
				ui.Text(c, ch.Description).FontSize(12).Selectable()
			}
			if ui.Button(c.Key("edit-group"), L("Edit…")).Clicked() {
				a.editGroup(ch)
			}
			ui.Divider(c)
			ui.Text(c, L("Members")).FontSize(11).Bold().TextColor(t.TextMuted)
			ui.Column(c).Gap(2).AlignSelf(ui.Stretch).Children(func() {
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
			ui.MenuButton(c.Key("add-member"), L("Add Bot"), func(m *ui.Menu) {
				for _, b := range outside {
					if m.Item(b.Name).Disabled(len(ch.Members) >= store.MaxMembers).Chosen() {
						a.do(a.st.AddMember(ch.ID, b.ID))
					}
				}
			})
			ui.Divider(c)
			if ui.Button(c.Key("delete-group"), L("Delete Group")).Clicked() {
				a.deleteChat(ch)
			}
		})
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
	row := ui.Row(c.Key("member-"+id)).Gap(8).AlignItems(ui.Center).Padding(4, 6).Radius(6).Label(name)
	if row.Hovered() {
		row.Background(t.SurfaceHover)
	}
	menu := func(m *ui.Menu) {
		if m.Item(L("Make Owner")).Disabled(ch.Owner == id).Chosen() {
			a.do(a.st.SetOwner(ch.ID, id))
		}
		if m.Item(L("Edit Bot…")).Chosen() {
			a.editBot(id)
		}
		m.Separator()
		if m.Item(L("Remove from Chat")).Chosen() {
			if len(ch.Members) == 1 {
				a.deleteChat(ch)
			} else {
				a.do(a.st.RemoveMember(ch.ID, id))
			}
		}
	}
	row.ContextMenu(menu)
	row.Children(func() {
		ui.Avatar(c, name, nil).FontSize(10)
		ui.Text(c, name).Grow(1).SingleLine().TextColor(a.authorColor(id))
		if ch.Owner == id {
			ui.Text(c, L("Owner")).FontSize(11).TextColor(t.TextMuted)
		}
		ui.Button(c.Key("more-"+id), "⋯").Label(L("More")).Menu(menu)
	})
}

func (a *app) botDetails(c *ui.Context, id string) {
	t := c.Theme()
	b, ok := a.byID[id]
	if !ok {
		return
	}
	ui.Text(c, L("Bot")).FontSize(11).Bold().TextColor(t.TextMuted)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Avatar(c, b.Name, nil)
		ui.Text(c, b.Name).Bold()
	})
	if b.Description != "" {
		ui.Text(c, b.Description).FontSize(12).Selectable()
	}
	model := b.Model
	if cfg, err := teamConfig(a, b); err == nil {
		model = cfg.Model
	}
	ui.Text(c, L("Model: %s · %s", a.providerName(b.Provider), model)).FontSize(12).TextColor(t.TextMuted)
	ui.Text(c, L("Workspace: %s", a.st.Workspace(b.ID))).FontSize(11).TextColor(t.TextMuted).Selectable()
	if ui.Button(c.Key("edit-bot"), L("Edit…")).Clicked() {
		a.editBot(id)
	}
	a.memoryDetails(c, b)
	ui.Divider(c)
	if dm, err := a.st.DMOf(id); err == nil {
		if ui.Button(c.Key("delete-bot"), L("Delete Bot")).Clicked() {
			a.deleteChat(dm)
		}
	}
}
