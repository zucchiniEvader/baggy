package main

import (
	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/team"
)

// gallery is New Bot's first step: the templates, a blank bot, and the
// groups of templates (store.Templates, store.GroupTemplates).
type gallery struct{ open bool }

func (a *app) newFromTemplate() { a.gallery = &gallery{open: true} }

func templateLook(tp store.Template) look {
	return look{icon: botIcon(tp.Icon), color: colorOf(tp.Color)}
}

// fromTemplate opens New Bot filled in from tp.
func (a *app) fromTemplate(tp store.Template) {
	b := tp.Bot()
	icon := b.Icon
	if icon == "" {
		icon = "bot"
	}
	a.botSheet = &botForm{open: true, name: L(b.Name), description: b.Description, providerName: L("Default"), thinking: "Default",
		icon: icon, color: b.Color, workdir: b.Workdir, template: tp.ID}
}

// groupFromTemplate opens New Group filled in from gt: its members are the
// bots made from its templates, those missing to be made with the group.
func (a *app) groupFromTemplate(gt store.GroupTemplate) {
	f := &groupForm{open: true, title: L(gt.Name), picked: map[string]bool{}}
	names := map[string]string{}
	for _, id := range gt.Members {
		s := groupSlot{tmpl: id, on: true}
		tp, _ := store.TemplateOf(id)
		names[id] = L(tp.Name)
		for _, b := range a.bots {
			if b.Template == id {
				s.bot, names[id] = b.ID, b.Name
				break
			}
		}
		f.slots = append(f.slots, s)
	}
	f.desc = store.Named(L(gt.Description), names)
	a.groupSheet = f
}

// templateSummary is a bot's line from its template while its description
// is still the template's, else "".
func templateSummary(b store.Bot) string {
	if tp, ok := store.TemplateOf(b.Template); ok && b.Description == tp.Description {
		return L(tp.Summary)
	}
	return ""
}

// suggestionsFor are the first messages a bot's empty chat offers.
func suggestionsFor(b store.Bot) []string {
	if tp, ok := store.TemplateOf(b.Template); ok {
		out := make([]string, len(tp.Suggestions))
		for i, s := range tp.Suggestions {
			out[i] = L(s)
		}
		return out
	}
	return []string{L("What can you do?"), L("Look at what is in your workspace."), L("Every weekday at 8, give me a short brief.")}
}

const galleryWidth = 560

func (a *app) galleryView(c *ui.Context) {
	f := a.gallery
	ui.Modal(c, &f.open, func() {
		dialogBody(c, galleryWidth, 14).Children(func() {
			dialogTitle(c, L("New Bot"), L("Start from a template, then change anything before you create it."))
			ui.Scroll(c).MaxHeight(460).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					sectionTitle(c, L("Bots"))
					cards := []func(){}
					for _, tp := range store.Templates {
						cards = append(cards, func() {
							if a.templateCard(c.Key("tpl-"+tp.ID), L(tp.Name), L(tp.Summary), func(c *ui.Context) {
								a.avatarOf(c, templateLook(tp), 32)
							}) {
								f.open = false
								a.fromTemplate(tp)
							}
						})
					}
					cards = append(cards, func() {
						if a.templateCard(c.Key("tpl-blank"), L("Blank Bot"), L("A name and a description of your own."), func(c *ui.Context) {
							iconTile(c, iconPlus, 32).Radius(16)
						}) {
							f.open = false
							a.newBot()
						}
					})
					grid(c.Key("bots"), cards)
					sectionTitle(c, L("Groups"))
					cards = cards[:0]
					for _, gt := range store.GroupTemplates {
						cards = append(cards, func() {
							var looks []look
							for _, id := range gt.Members {
								if tp, ok := store.TemplateOf(id); ok {
									looks = append(looks, templateLook(tp))
								}
							}
							if a.templateCard(c.Key("grp-"+gt.ID), L(gt.Name), L(gt.Summary), func(c *ui.Context) {
								a.packedAvatar(c, looks, 32, pal(c).surface)
							}) {
								f.open = false
								a.groupFromTemplate(gt)
							}
						})
					}
					grid(c.Key("groups"), cards)
				})
			})
			ui.Row(c).Justify(ui.End).Children(func() {
				if button(c, L("Cancel")).Clicked() {
					f.open = false
				}
			})
		})
	})
}

// grid lays cards out two to a row.
func grid(c *ui.Context, cards []func()) {
	ui.Column(c).Gap(4).Children(func() {
		gridRows(c, cards)
	})
}

func gridRows(c *ui.Context, cards []func()) {
	for i := 0; i < len(cards); i += 2 {
		ui.Row(c.Key(i)).Gap(4).Children(func() {
			for j := i; j < i+2; j++ {
				ui.Box(c.Key(j)).Grow(1).Basis(0).MinWidth(0).Children(func() {
					if j < len(cards) {
						cards[j]()
					}
				})
			}
		})
	}
}

// templateCard is a gallery's choice: its avatar beside its name and line.
// It reports a click.
func (a *app) templateCard(c *ui.Context, name, summary string, avatar func(*ui.Context)) bool {
	t := c.Theme()
	card := ui.ButtonBase(c).FillWidth().Padding(8).Gap(10).Radius(8).AlignItems(ui.Start).Justify(ui.Start).Label(name).FocusRing(false)
	styleRow(card, t, rowPlain)
	card.Children(func() {
		avatar(c)
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, name).FontSize(13).FontWeight(600).SingleLine().MinWidth(0)
			ui.Text(c, summary).FontSize(12).LineHeight(1.35).TextColor(t.TextMuted).MaxLines(2).MinWidth(0)
		})
	})
	return card.Clicked()
}

// templateNotes says what a template's bot needs to work well: web search
// on its provider, a folder, approvals for its commands.
func (a *app) templateNotes(c *ui.Context, f *botForm) {
	tp, ok := store.TemplateOf(f.template)
	if !ok {
		return
	}
	var notes []string
	if tp.Web && !a.webSearch(f.providerName) {
		notes = append(notes, L("It searches the web: turn on Web search for its provider in Settings › Models."))
	}
	if tp.Folder && f.workdir == "" {
		notes = append(notes, L("Choose the folder it works in, such as a project."))
	}
	if tp.Commands && a.prefs.commands == ModeRun {
		notes = append(notes, L("It runs commands on your files. Settings › Commands › Ask every time has you approve each first."))
	}
	for _, n := range notes {
		ui.Row(c).Gap(6).AlignItems(ui.Start).Children(func() {
			ui.Icon(c, iconInfo).FontSize(12).TextColor(c.Theme().TextMuted).Shrink(0).Padding(1, 0, 0)
			ui.Text(c, n).FontSize(11).LineHeight(1.4).TextColor(c.Theme().TextMuted).Grow(1).MinWidth(0)
		})
	}
}

// webSearch reports whether a bot on this provider can search the web; a
// provider from the environment, unknown here, counts as able.
func (a *app) webSearch(providerName string) bool {
	id := a.providerID(providerName)
	if id == "" {
		id = a.st.Setting(team.SettingDefaultProvider)
	}
	p, err := a.st.Provider(id)
	if err != nil {
		return true
	}
	return p.Options["web_search"]
}

// slotted reports whether a template's slot holds this bot.
func (f *groupForm) slotted(id string) bool {
	for _, s := range f.slots {
		if s.bot == id {
			return true
		}
	}
	return false
}

// slotRows are a group template's members as check rows: a bot there is,
// or one to be made, marked New.
func (a *app) slotRows(c *ui.Context, f *groupForm) {
	t := c.Theme()
	for i, s := range f.slots {
		tp, _ := store.TemplateOf(s.tmpl)
		name, line := L(tp.Name), L(tp.Summary)
		if s.bot != "" {
			b := a.byID[s.bot]
			name, line = b.Name, firstLine(b.Description)
			if sum := templateSummary(b); sum != "" {
				line = sum
			}
		}
		row := listRow(c.Key("slot-"+s.tmpl), rowPlain).Label(name).Role(ui.RoleCheckBox).Checked(s.on)
		if row.Clicked() {
			f.slots[i].on = !s.on
		}
		row.Children(func() {
			if s.bot != "" {
				a.avatar(c, s.bot, 20)
			} else {
				a.avatarOf(c, templateLook(tp), 20)
			}
			ui.Text(c, name).FontSize(13).Shrink(0)
			if s.bot == "" {
				badge(c, L("New"))
			}
			ui.Text(c, line).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
			ui.Spacer(c)
			if s.on {
				ui.Icon(c, iconCheck).FontSize(iconSize).Shrink(0)
			}
		})
	}
}
