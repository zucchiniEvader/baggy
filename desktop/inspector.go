package main

import (
	"os"
	"strings"

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
	a.inspectorHeader(c, func() { a.chatAvatar(c, ch, 56, pal(c).appBg) }, ch.Title, L("%d bots", len(ch.Members)), func() {
		if inspectorAction(c, "edit-group", iconPencil, L("Edit…")).Clicked() {
			a.editGroup(ch)
		}
		a.moreMenu(c, L("Delete Group"), func() { a.deleteChat(ch) })
	})
	a.about(c, "group-"+ch.ID, ch.Description)
	inspectorSection(c, L("Members"), func() {
		ui.Column(c).Gap(1).Children(func() {
			for _, id := range ch.Members {
				a.memberRow(c, ch, id)
			}
		})
		a.addMemberRow(c, ch)
	})
	a.usageSection(c, ch)
}

func (a *app) addMemberRow(c *ui.Context, ch store.Chat) {
	t := c.Theme()
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
		ui.Box(c).Size(20, 20).Center().Shrink(0).Children(func() { ui.Icon(c, iconPlus).FontSize(iconSize).TextColor(t.TextMuted) })
		ui.Text(c, L("Add Bot")).FontSize(13).TextColor(t.TextMuted)
	})
	if action != nil {
		action()
	}
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
		a.avatar(c, id, 20)
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
	b, ok := a.byID[id]
	if !ok {
		return
	}
	model := b.Model
	if cfg, err := teamConfig(a, b); err == nil {
		model = cfg.Model
	}
	dm, dmErr := a.st.DMOf(id)
	line := a.providerName(b.Provider)
	if model != "" {
		line += " · " + model
	}
	a.inspectorHeader(c, func() { a.avatar(c, b.ID, 56) }, b.Name, line, func() {
		if inspectorAction(c, "edit-bot", iconPencil, L("Edit…")).Clicked() {
			a.editBot(id)
		}
		if a.openFolder != nil && inspectorAction(c, "show-folder", iconFolder, L("Show Folder")).Tooltip(a.st.WorkDir(b)).Clicked() {
			a.openFolder(a.st.WorkDir(b))
		}
		if dmErr == nil {
			a.moreMenu(c, L("Delete Bot"), func() { a.deleteChat(dm) })
		}
	})
	a.about(c, "bot-"+b.ID, b.Description)
	inspectorSection(c, L("Folder"), func() { a.folderRow(c, b) })
	a.routinesDetails(c, b)
	a.memoryDetails(c, b)
	if dmErr == nil {
		a.usageSection(c, dm)
	}
}

// inspectorHeader is who the inspector shows: its avatar, name and a muted
// line, centered, over a row of actions.
func (a *app) inspectorHeader(c *ui.Context, avatar func(), name, line string, actions func()) {
	t := c.Theme()
	ui.Column(c).AlignItems(ui.Center).Gap(10).Padding(4, 0, 12).Children(func() {
		avatar()
		ui.Column(c).AlignItems(ui.Center).Gap(2).MaxWidth(inspectorWidth - 32).Children(func() {
			ui.Text(c, name).FontSize(15).Bold().SingleLine().MinWidth(0)
			ui.Text(c, line).FontSize(12).TextColor(t.TextMuted).SingleLine().MinWidth(0)
		})
		ui.Row(c).Gap(6).Children(actions)
	})
}

// inspectorAction is one of the header's actions: an icon in a gray
// circle, its name as its tooltip.
func inspectorAction(c *ui.Context, key string, svg *ui.SVG, label string) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Size(30, 30).Radius(15).Center().Label(label).Tooltip(label).FocusRing(false)
	styleButton(b, c.Theme(), secondary, false)
	b.Children(func() { ui.Icon(c, svg).FontSize(iconSize) })
	return b
}

// moreMenu is the header's ⋯: the delete, in red words in a menu rather
// than a red button.
func (a *app) moreMenu(c *ui.Context, label string, run func()) {
	var action func()
	inspectorAction(c, "more", iconMore, L("More")).Menu(func(m *ui.Menu) {
		if m.Item(label + "…").Chosen() {
			action = run
		}
	})
	if action != nil {
		action()
	}
}

// aboutLines is how many lines of a description show until More.
const aboutLines = 4

// about is a description, muted, its first lines until More opens it.
func (a *app) about(c *ui.Context, key, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	t := c.Theme()
	open := a.aboutOpen[key]
	ui.Column(c).Gap(2).Padding(0, inspectorInset, 4).Children(func() {
		body := ui.Text(c, text).FontSize(12).LineHeight(1.5).TextColor(t.TextMuted).Selectable()
		long := len([]rune(text)) > 160 || strings.Count(text, "\n") >= aboutLines
		if long && !open {
			body.MaxLines(aboutLines)
		}
		if !long {
			return
		}
		label := L("More")
		if open {
			label = L("Less")
		}
		more := ui.ButtonBase(c.Key("about-"+key)).AlignSelf(ui.Start).Label(label).FocusRing(false)
		more.Children(func() { ui.Text(c, label).FontSize(12).FontWeight(600) })
		if more.Clicked() {
			if a.aboutOpen == nil {
				a.aboutOpen = map[string]bool{}
			}
			a.aboutOpen[key] = !open
		}
	})
}

// inspectorInset lines the inspector's words up with its rows' icons.
const inspectorInset = 8

// inspectorSection is a titled part of the inspector, a hairline above it.
func inspectorSection(c *ui.Context, title string, body func()) {
	ui.Column(c).BorderWidth(1, 0, 0, 0).BorderColor(pal(c).border).Padding(4, 0, 8).Margin(8, 0, 0).Children(func() {
		ui.Box(c).Padding(0, inspectorInset).Children(func() { sectionTitle(c, title) })
		body()
	})
}

// valueRow is a label on the left and its value on the right.
func valueRow(c *ui.Context, label, value string) {
	t := c.Theme()
	ui.Row(c).Height(24).Gap(8).AlignItems(ui.Center).Padding(0, inspectorInset).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(0)
		ui.Spacer(c)
		ui.Text(c, value).FontSize(12).SingleLine().Shrink(1).MinWidth(0)
	})
}

// meter is a thin bar of how full something is; orange past 80%.
func meter(c *ui.Context, frac float64) {
	p := pal(c)
	frac = min(max(frac, 0), 1)
	fill := c.Theme().TextMuted
	if frac > 0.8 {
		fill = p.orange
	}
	ui.Row(c).Height(4).Radius(2).Background(p.pill).Clip().Children(func() {
		if frac > 0 {
			ui.Box(c).Grow(float32(frac)).Background(fill)
		}
		if frac < 1 {
			ui.Box(c).Grow(float32(1 - frac))
		}
	})
}

// folderRow is where the bot's tools run, opening in Finder on a click.
func (a *app) folderRow(c *ui.Context, b store.Bot) {
	t := c.Theme()
	dir := a.st.WorkDir(b)
	shown := dir
	if home, err := os.UserHomeDir(); err == nil {
		if rest, ok := strings.CutPrefix(dir, home); ok {
			shown = "~" + rest
		}
	}
	if b.Workdir == "" {
		shown = L("Its own workspace")
	}
	row := listRow(c.Key("folder-row"), rowPlain).Label(dir).Tooltip(dir)
	if row.Clicked() && a.openFolder != nil {
		a.openFolder(dir)
	}
	row.Children(func() {
		ui.Box(c).Size(20, 20).Center().Shrink(0).Children(func() { ui.Icon(c, iconFolder).FontSize(iconSize).TextColor(t.TextMuted) })
		ui.Text(c, shown).FontSize(13).SingleLine().Grow(1).MinWidth(0)
		ui.Icon(c, iconChevronRight).FontSize(11).TextColor(t.TextMuted).Shrink(0)
	})
}

// usageSection is what the chat cost and how full its last request was.
func (a *app) usageSection(c *ui.Context, ch store.Chat) {
	inspectorSection(c, L("Usage"), func() {
		if es, err := a.st.Messages(ch.ID, 0); err == nil {
			valueRow(c, L("Spent in this chat"), money(spent(es)))
		}
		used, window := a.contextUse(ch.ID)
		if used == 0 {
			return
		}
		valueRow(c, L("Context"), L("%s of %s", short(used), short(window)))
		ui.Box(c).Padding(2, inspectorInset, 0).Children(func() { meter(c, float64(used)/float64(max(window, 1))) })
	})
}
