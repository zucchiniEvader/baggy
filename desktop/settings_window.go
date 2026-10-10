package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/provider"
	"github.com/spike/gobot/provider/connect"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/team"
)

// Settings is a window of its own, as macOS's System Settings: the panes
// in a sidebar on the left, the pane chosen on the right.

const (
	paneGeneral  = "general"
	paneModels   = "models"
	paneCommands = "commands"
	paneServers  = "servers"
	paneAbout    = "about"

	settingsNavWidth = 200
	providerListW    = 190
)

type settingsPane struct {
	id    string
	label func() string
	icon  func() *ui.SVG
}

var settingsPanes = []settingsPane{
	{paneGeneral, func() string { return L("General") }, func() *ui.SVG { return iconSettings }},
	{paneModels, func() string { return L("Models") }, func() *ui.SVG { return iconSparkle }},
	{paneCommands, func() string { return L("Commands") }, func() *ui.SVG { return iconShield }},
	{paneServers, func() string { return L("MCP Servers") }, func() *ui.SVG { return iconPlug }},
	{paneAbout, func() string { return L("About") }, func() *ui.SVG { return iconInfo }},
}

// settingsState is the Settings window's state.
type settingsState struct {
	pane    string
	nav     listNav
	provSel string        // the provider shown in Models: an id, or "new"
	prov    *providerForm // its form
	provNav listNav
	confirm *confirmation
}

// openSettings shows the Settings window, on a pane ("" keeps the one shown).
func (a *app) openSettings(pane string) {
	if pane != "" {
		a.set.pane = pane
	}
	if a.set.pane == "" {
		a.set.pane = paneGeneral
	}
	if a.showSettings != nil {
		a.showSettings()
		return
	}
	a.settings = true // without windows (tests): settingsRoot is drawn by hand
}

// settingsRoot is the Settings window.
func (a *app) settingsRoot(c *ui.Context) {
	p := applyTheme(c)
	if a.set.pane == "" {
		a.set.pane = paneGeneral
	}
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	} else {
		c.Root().Background(p.sidebar)
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		a.settingsNav(c)
		ui.Column(c).Grow(1).MinWidth(0).Background(p.appBg).Children(func() {
			title := ""
			for _, sp := range settingsPanes {
				if sp.id == a.set.pane {
					title = sp.label()
				}
			}
			ui.Row(c).Height(titleBarHeight).Shrink(0).Padding(0, 16+c.TitleBar().Right, 0, 20).AlignItems(ui.Center).DragWindow().Children(func() {
				ui.Text(c, title).FontSize(13).FontWeight(600).SingleLine().MinWidth(0)
			})
			switch a.set.pane {
			case paneModels:
				a.modelsPane(c)
			default:
				ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
					ui.Column(c).MaxWidth(620).FillWidth().Padding(4, 28, 28).Gap(6).Children(func() {
						switch a.set.pane {
						case paneGeneral:
							a.generalPane(c)
						case paneCommands:
							a.commandsPane(c)
						case paneServers:
							a.pluginsView(c)
						case paneAbout:
							a.aboutPane(c)
						}
					})
				})
			}
		})
	})
	if f := a.set.confirm; f != nil {
		if ui.AlertDialog(c, &f.open, f.title, f.message, L("Cancel"), f.action) == 1 {
			f.run()
		}
		if !f.open {
			a.set.confirm = nil
		}
	}
	if a.redrawMain != nil {
		a.redrawMain() // what changed here shows there
	}
}

// settingsNav is the panes, as a list the keys move through.
func (a *app) settingsNav(c *ui.Context) {
	t := c.Theme()
	bar := c.TitleBar()
	ui.Column(c).Width(settingsNavWidth).Shrink(0).Children(func() {
		ui.Row(c).Height(titleBarHeight).Shrink(0).Padding(0, sidebarPad, 0, max(bar.Left+6, sidebarPad)).DragWindow()
		box := ui.Column(c.Key("panes")).Gap(1).Padding(0, sidebarPad).Focusable().FocusRing(false).Label(L("Settings")).Role(ui.RoleList)
		focused := box.FocusWithin()
		if !focused {
			a.set.nav.keyboard = false
		}
		chosen := 0
		for i, sp := range settingsPanes {
			if sp.id == a.set.pane {
				chosen = i
			}
		}
		a.set.nav.keys(box, chosen, len(settingsPanes), func(i int) { a.set.pane = settingsPanes[i].id })
		if a.set.nav.focus {
			box.Focus()
			a.set.nav.focus = false
		}
		box.Children(func() {
			for _, sp := range settingsPanes {
				sp := sp
				st := a.set.nav.state(sp.id == a.set.pane, focused)
				row := ui.Row(c.Key("pane-"+sp.id)).Height(rowHeight).Padding(0, 8).Gap(8).Radius(rowRadius).AlignItems(ui.Center).Label(sp.label()).Role(ui.RoleListItem)
				styleRow(row, t, st)
				if row.Pressed() || row.Clicked() {
					a.set.nav.keyboard, a.set.nav.focus = false, true
					a.set.pane = sp.id
				}
				row.Children(func() {
					ic := ui.Icon(c, sp.icon()).FontSize(iconSize).Shrink(0)
					if st != rowKeyboard {
						ic.TextColor(t.TextMuted)
					}
					ui.Text(c, sp.label()).FontSize(13).SingleLine().MinWidth(0)
				})
			}
		})
	})
}

// card is a pane's block of rows, a hairline between them.
func card(c *ui.Context, rows ...func()) {
	p := pal(c)
	ui.Column(c).Radius(cardRadius).Background(p.surface).Border(1, p.border).Clip().Children(func() {
		for i, r := range rows {
			row := ui.Column(c.Key(i))
			if i > 0 {
				row.BorderWidth(1, 0, 0, 0).BorderColor(p.border)
			}
			row.Children(r)
		}
	})
}

// settingRow is one setting: its title and a muted line on the left, its
// control on the right, as macOS's settings.
func settingRow(c *ui.Context, title, desc string, control func()) {
	ui.Row(c).Padding(10, 14).Gap(16).AlignItems(ui.Center).MinHeight(44).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(13)
			if desc != "" {
				ui.Text(c, desc).FontSize(11).LineHeight(1.4).TextColor(c.Theme().TextMuted)
			}
		})
		ui.Row(c).Shrink(0).AlignItems(ui.Center).Children(control)
	})
}

// switchRow is a setting turned on and off; it reports a change.
func switchRow(c *ui.Context, title, desc string, on *bool) bool {
	changed := false
	settingRow(c, title, desc, func() { changed = ui.Switch(c, on).Label(title).Changed() })
	return changed
}

func (a *app) generalPane(c *ui.Context) {
	pr := a.prefs
	changed := false
	sectionTitle(c, L("Language"))
	card(c, func() {
		settingRow(c, L("Language"), L("The language of the app; bots answer in the language you write in."), func() {
			labels := make([]string, len(languages))
			for i, l := range languages {
				labels[i] = languageLabel(l)
			}
			choice := languageLabel(pr.language)
			popup(c, &choice, labels).Width(180)
			for i, l := range labels {
				if l == choice && languages[i] != pr.language {
					pr.language = languages[i]
					changed = true
					if a.onLanguage != nil {
						defer a.onLanguage()
					}
				}
			}
		})
	})
	sectionTitle(c, L("Conversation"))
	card(c, func() {
		changed = switchRow(c, L("Show thinking"), L("A reply's longer thinking, closed under a line that opens it."), &pr.showThinking) || changed
	})
	sectionTitle(c, L("Window"))
	card(c, func() {
		changed = switchRow(c, L("Keep running when closed"), L("Bots keep working; the tray icon opens the window again."), &pr.keepRunning) || changed
	})
	if changed {
		pr.save()
		a.applyPrefs()
	}
}

func (a *app) commandsPane(c *ui.Context) {
	t := c.Theme()
	pr := a.prefs
	sectionTitle(c, L("Before a command runs"))
	card(c, func() {
		settingRow(c, L("Commands"), L("Before a shell command or a plugin action that changes something: run it, ask every time, or let the bot's model review it and ask only when in doubt. Always Allow keeps an exact command."), func() {
			choice := modeLabel(pr.commands)
			popup(c, &choice, modeLabels()).Width(180)
			if m := modeOf(choice); m != pr.commands {
				pr.commands = m
				pr.save()
				a.applyPrefs()
			}
		})
	})
	sectionTitle(c, L("Always allowed"))
	rules := a.approvals.rules()
	ui.Column(c).Radius(cardRadius).Background(pal(c).surface).Border(1, pal(c).border).Padding(4).Gap(1).Children(func() {
		if len(rules) == 0 {
			ui.Text(c, L("Nothing yet: Always Allow on a waiting command adds it here.")).FontSize(12).TextColor(t.TextMuted).Padding(8, 10)
		}
		for _, r := range rules {
			r := r
			kind, what, _ := strings.Cut(r, ":")
			ui.Row(c.Key("rule-"+r)).Height(rowHeight).Padding(0, 4, 0, 8).Gap(8).Radius(rowRadius).AlignItems(ui.Center).Children(func() {
				svg := iconTerminal
				if kind == "tool" {
					svg = iconPlug
				}
				ui.Icon(c, svg).FontSize(iconSize).TextColor(t.TextMuted).Shrink(0)
				ui.Text(c, what).Font(monoFont).FontSize(12).SingleLine().Grow(1).MinWidth(0)
				if iconButton(c.Key("remove-"+r), iconClose, L("Remove")).Clicked() {
					a.approvals.removeRule(r)
				}
			})
		}
	})
}

func (a *app) aboutPane(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Gap(12).AlignItems(ui.Center).Padding(24, 0).Children(func() {
		iconTile(c, iconBot, 48)
		ui.Column(c).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "gobot").FontSize(17).Bold()
			ui.Text(c, L("A desktop assistant with group chat.")).FontSize(12).TextColor(t.TextMuted)
		})
	})
	sectionTitle(c, L("Data"))
	card(c, func() {
		settingRow(c, a.st.Dir(), L("The chats, bots and settings (gobot.db), the bots' workspaces, and mcp.json."), func() {
			if a.openFolder != nil {
				if smallButton(c.Key("show-data"), L("Show Folder"), secondary).Clicked() {
					a.openFolder(a.st.Dir())
				}
			}
		})
	})
}

// ---- Models: the providers, a list and the one chosen ----

// apiShort is a protocol in a word, for the list.
func apiShort(id string) string {
	switch id {
	case connect.Chat:
		return "Chat Completions"
	case connect.Responses:
		return "Responses"
	}
	return "Anthropic"
}

func (a *app) modelsPane(c *ui.Context) {
	t := c.Theme()
	p := pal(c)
	ps, _ := a.st.Providers()
	def := a.st.Setting(team.SettingDefaultProvider)
	if def == "" && len(ps) > 0 {
		def = ps[0].ID
	}
	if a.set.provSel == "" && len(ps) > 0 {
		a.set.provSel = ps[0].ID
	}
	var chosen *store.Provider
	for i := range ps {
		if ps[i].ID == a.set.provSel {
			chosen = &ps[i]
		}
	}
	// The form follows the choice.
	switch {
	case a.set.provSel == "new":
		if a.set.prov == nil || a.set.prov.id != "" {
			a.set.prov = &providerForm{api: apiLabels[0], web: true}
		}
	case chosen == nil:
		a.set.prov = nil
	case a.set.prov == nil || a.set.prov.id != chosen.ID:
		a.set.prov = formOf(*chosen)
	}
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Padding(4, 28, 24, 28).Gap(16).Children(func() {
		// The list, with + and − at its foot as macOS's.
		ui.Column(c).Width(providerListW).Shrink(0).Radius(cardRadius).Background(p.surface).Border(1, p.border).Clip().Children(func() {
			box := ui.Column(c.Key("providers")).Grow(1).MinHeight(0).Padding(4).Gap(1).Focusable().FocusRing(false).Label(L("Providers")).Role(ui.RoleList)
			focused := box.FocusWithin()
			if !focused {
				a.set.provNav.keyboard = false
			}
			at := -1
			for i, pr := range ps {
				if pr.ID == a.set.provSel {
					at = i
				}
			}
			a.set.provNav.keys(box, at, len(ps), func(i int) { a.set.provSel = ps[i].ID })
			if a.set.provNav.focus {
				box.Focus()
				a.set.provNav.focus = false
			}
			box.Children(func() {
				for _, pr := range ps {
					pr := pr
					st := a.set.provNav.state(pr.ID == a.set.provSel, focused)
					row := ui.Row(c.Key("prov-"+pr.ID)).Height(40).Padding(0, 8).Gap(8).Radius(rowRadius).AlignItems(ui.Center).Label(pr.Name).Role(ui.RoleListItem)
					styleRow(row, t, st)
					if row.Pressed() || row.Clicked() {
						a.set.provNav.keyboard, a.set.provNav.focus = false, true
						a.set.provSel = pr.ID
					}
					row.Children(func() {
						sub := t.TextMuted
						ic := ui.Icon(c, iconPlug).FontSize(iconSize).Shrink(0)
						if st == rowKeyboard {
							sub = t.AccentText
						} else {
							ic.TextColor(t.TextMuted)
						}
						ui.Column(c).Grow(1).MinWidth(0).Children(func() {
							ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
								ui.Text(c, pr.Name).FontSize(13).SingleLine().Shrink(1).MinWidth(0)
								if pr.ID == def {
									tag(c, L("Default"))
								}
							})
							line := apiShort(pr.API)
							if len(pr.Models) > 0 {
								line = pr.Models[0] + " · " + line
							}
							ui.Text(c, line).FontSize(11).TextColor(sub).SingleLine().MinWidth(0)
						})
					})
				}
				if a.set.provSel == "new" {
					row := ui.Row(c.Key("prov-new")).Height(40).Padding(0, 8).Gap(8).Radius(rowRadius).AlignItems(ui.Center)
					styleRow(row, t, rowChosen)
					row.Children(func() {
						ui.Icon(c, iconPlus).FontSize(iconSize).TextColor(t.TextMuted)
						ui.Text(c, L("New Provider")).FontSize(13).SingleLine().MinWidth(0)
					})
				}
			})
			ui.Row(c).Height(30).Shrink(0).Padding(0, 4).Gap(2).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(p.border).Children(func() {
				if iconButton(c.Key("add-provider"), iconPlus, L("Add Provider")).Clicked() {
					a.set.provSel, a.set.prov = "new", &providerForm{api: apiLabels[0], web: true}
				}
				minus := iconButton(c.Key("remove-provider"), iconMinus, L("Delete Provider")).Disabled(chosen == nil)
				if minus.Clicked() && chosen != nil {
					a.askDeleteProvider(*chosen)
				}
			})
		})
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			if a.set.prov == nil {
				ui.Column(c).Grow(1).Center().Gap(12).Padding(60, 0).Children(func() {
					iconTile(c, iconSparkle, 40)
					ui.Text(c, L("Add a provider to give your bots a model.")).FontSize(12).TextColor(t.TextMuted)
					if smallButton(c.Key("first-provider"), L("Add Provider"), primary).Clicked() {
						a.set.provSel, a.set.prov = "new", &providerForm{api: apiLabels[0], web: true}
					}
				})
				return
			}
			ui.Scroll(c).Grow(1).MinHeight(0).Children(func() { a.providerDetail(c, a.set.prov, chosen, def) })
			a.providerActions(c, a.set.prov, chosen)
		})
	})
}

func (a *app) askDeleteProvider(pr store.Provider) {
	a.set.confirm = &confirmation{open: true, title: L("Delete %s?", pr.Name), message: L("Bots on it move to the default provider."), action: L("Delete"), run: func() {
		a.do(a.st.DeleteProvider(pr.ID))
		if a.st.Setting(team.SettingDefaultProvider) == pr.ID {
			a.st.SetSetting(team.SettingDefaultProvider, "")
		}
		a.set.provSel, a.set.prov = "", nil
	}}
}

// tag is a word in a gray capsule, as Default.
func tag(c *ui.Context, text string) ui.Element {
	p := pal(c)
	return ui.Text(c, text).FontSize(10).FontWeight(600).TextColor(p.muted).Padding(1, 6).Radius(8).Background(p.pill).Shrink(0)
}

func formOf(p store.Provider) *providerForm {
	return &providerForm{id: p.ID, name: p.Name, api: apiLabel(p.API), base: p.BaseURL, key: p.APIKey,
		models: strings.Join(p.Models, ", "), replay: p.Options["replay_reasoning"], encrypted: p.Options["encrypted_reasoning"], web: p.Options["web_search"]}
}

// dirty reports whether the form differs from what is stored.
func (f *providerForm) dirty(stored *store.Provider) bool {
	if stored == nil {
		return true
	}
	s := formOf(*stored)
	return f.name != s.name || f.api != s.api || f.base != s.base || f.key != s.key || f.models != s.models ||
		f.replay != s.replay || f.encrypted != s.encrypted || f.web != s.web
}

// fieldRow is a setting whose control is a field: its title in a narrow
// column on the left, the field taking the rest, a muted line under it.
func fieldRow(c *ui.Context, title, desc string, field func()) {
	ui.Row(c).Padding(10, 14).Gap(12).AlignItems(ui.Start).MinHeight(44).Children(func() {
		ui.Text(c, title).FontSize(13).Width(96).Shrink(0).Margin(6, 0, 0)
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			field()
			if desc != "" {
				ui.Text(c, desc).FontSize(11).LineHeight(1.35).TextColor(c.Theme().TextMuted)
			}
		})
	})
}

// providerDetail is the chosen provider: who it is, then its connection,
// models and features, edited in place.
func (a *app) providerDetail(c *ui.Context, f *providerForm, stored *store.Provider, def string) {
	t := c.Theme()
	ui.Column(c).Gap(6).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Padding(0, 0, 6).Children(func() {
			iconTile(c, iconPlug, 36)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				name := f.name
				if strings.TrimSpace(name) == "" {
					name = L("New Provider")
				}
				ui.Text(c, name).FontSize(15).Bold().SingleLine().MinWidth(0)
				ui.Text(c, apiLabel(apiID(f.api))).FontSize(12).TextColor(t.TextMuted).SingleLine().MinWidth(0)
			})
			if f.id != "" {
				if f.id == def {
					tag(c, L("Default"))
				} else if smallButton(c.Key("make-default"), L("Make Default"), secondary).Clicked() {
					a.st.SetSetting(team.SettingDefaultProvider, f.id)
				}
			}
		})
		sectionTitle(c, L("Connection"))
		card(c,
			func() { fieldRow(c, L("Name"), "", func() { textField(c.Key("p-name"+f.id), &f.name, "DeepSeek") }) },
			func() { fieldRow(c, L("Protocol"), "", func() { popup(c, &f.api, apiLabels) }) },
			func() {
				fieldRow(c, L("Base URL"), L("Empty is the vendor's own."), func() {
					textField(c.Key("p-base"+f.id), &f.base, "https://api.deepseek.com/anthropic")
				})
			},
			func() {
				fieldRow(c, L("API key"), L("Kept on this computer only."), func() {
					fieldBox(c.Key("p-keybox"+f.id), func(box ui.Element) {
						in := fieldInput(c.Key("p-key"+f.id), box, &f.key, "sk-…")
						if !f.showKey {
							in.Password()
						}
						tip := L("Show key")
						svg := iconEye
						if f.showKey {
							tip, svg = L("Hide key"), iconEyeOff
						}
						eye := ui.ButtonBase(c.Key("p-eye"+f.id)).Size(20, 20).Radius(4).Center().Label(tip).Tooltip(tip).FocusRing(false)
						styleButton(eye, t, ghost, false)
						eye.Children(func() { ui.Icon(c, svg).FontSize(12) })
						if eye.Clicked() {
							f.showKey = !f.showKey
						}
					})
				})
			},
		)
		sectionTitle(c, L("Models"))
		card(c, func() {
			fieldRow(c, L("Models"), L("Comma separated; the first is the default."), func() {
				textField(c.Key("p-models"+f.id), &f.models, "deepseek-flash, deepseek-v4-pro")
			})
		})
		var rows []func()
		if apiID(f.api) != connect.Chat {
			rows = append(rows, func() {
				switchRow(c, L("Web search"), L("The server searches the web for the bot (and reads pages, on Anthropic)."), &f.web)
			})
		}
		if apiID(f.api) == connect.Chat {
			rows = append(rows, func() {
				switchRow(c, L("Send reasoning back"), L("As reasoning_content, which DeepSeek's thinking mode takes."), &f.replay)
			})
		}
		if apiID(f.api) == connect.Responses {
			rows = append(rows, func() {
				switchRow(c, L("Encrypted reasoning"), L("Keeps reasoning across tool calls on OpenAI's API."), &f.encrypted)
			})
		}
		sectionTitle(c, L("Features"))
		card(c, rows...)
		if f.err != "" {
			ui.Box(c).Padding(8, 0, 0).Children(func() { errorRow(c, f.err) })
		}
	})
}

// providerActions are the form's buttons, kept in view under it.
func (a *app) providerActions(c *ui.Context, f *providerForm, stored *store.Provider) {
	t := c.Theme()
	dirty := f.dirty(stored)
	ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Padding(12, 0, 0).Children(func() {
		label := L("Test Connection")
		if f.checking {
			label = L("Testing…")
		}
		if smallButton(c.Key("p-test"+f.id), label, secondary).Clicked() && !f.checking {
			a.testProvider(f)
		}
		switch {
		case f.checking:
			ui.Spinner(c).Size(12, 12)
		case f.check != "" && f.checkOK:
			ui.Icon(c, iconCheck).FontSize(12).TextColor(pal(c).greenBar).Shrink(0)
			ui.Text(c, f.check).FontSize(12).TextColor(pal(c).green).SingleLine().Shrink(1).MinWidth(0)
		case f.check != "":
			ui.Icon(c, iconXCircle).FontSize(12).TextColor(pal(c).redBar).Shrink(0)
			ui.Text(c, f.check).FontSize(12).TextColor(pal(c).red).SingleLine().Shrink(1).MinWidth(0).Tooltip(f.check)
		}
		ui.Spacer(c)
		if f.id == "" {
			if button(c, L("Cancel")).Clicked() {
				a.set.provSel, a.set.prov = "", nil
			}
			if primaryButton(c.Key("p-save"), L("Create"), strings.TrimSpace(f.name) == "").Clicked() && a.saveProvider(f) {
				a.set.provSel = f.id
				a.reload()
			}
			return
		}
		if !dirty {
			ui.Text(c, L("Saved")).FontSize(12).TextColor(t.TextMuted)
			return
		}
		if button(c, L("Revert")).Clicked() && stored != nil {
			a.set.prov = formOf(*stored)
		}
		if primaryButton(c.Key("p-save"), L("Save"), false).Clicked() && a.saveProvider(f) {
			a.reload()
		}
	})
}

// testProvider sends the form's provider one short request, in the
// background, and shows how it went.
func (a *app) testProvider(f *providerForm) {
	cfg := connect.Config{API: apiID(f.api), ID: f.id, BaseURL: strings.TrimSpace(f.base), APIKey: strings.TrimSpace(f.key)}
	for _, m := range strings.Split(f.models, ",") {
		if m = strings.TrimSpace(m); m != "" {
			cfg.Model = m
			break
		}
	}
	if cfg.Model == "" {
		f.check, f.checkOK = L("Name a model first."), false
		return
	}
	f.checking, f.check = true, ""
	redraw := a.redrawSettings
	go func() {
		ok, msg := checkProvider(cfg)
		post := a.post
		post(func() {
			f.checking, f.checkOK, f.check = false, ok, msg
			if redraw != nil {
				redraw()
			}
		})
	}()
}

// mcpTemplatePath writes an mcp.json to start from when there is none.
func ensureMCP(path string) {
	if _, err := os.Stat(path); err != nil {
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(mcpTemplate), 0o644)
	}
}

// checkProvider asks a model for a word, 30 seconds at most.
func checkProvider(cfg connect.Config) (bool, string) {
	p, err := connect.New(cfg)
	if err != nil {
		return false, err.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	m := provider.Model{Provider: p, MaxTokens: 16}
	reply := m.Call(ctx, "", []agent.Message{{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: "Reply with OK."}}}}, nil, func(agent.Event) {})
	if reply.StopReason == agent.StopError || reply.StopReason == agent.StopAborted {
		why := errorText(reply)
		if why == "" {
			why = L("No answer.")
		}
		return false, oneLine(why, 160)
	}
	return true, L("Connected: %s answered.", cfg.Model)
}
