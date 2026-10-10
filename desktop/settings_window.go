package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
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
	providerListW    = 210
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
	// The form follows the choice.
	if a.set.provSel != "" && (a.set.prov == nil || (a.set.prov.id != a.set.provSel && !(a.set.provSel == "new" && a.set.prov.id == ""))) {
		a.set.prov = nil
		if a.set.provSel == "new" {
			a.set.prov = &providerForm{api: apiLabels[0], web: true}
		}
		for _, pr := range ps {
			if pr.ID == a.set.provSel {
				a.set.prov = formOf(pr)
			}
		}
	}
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Padding(4, 28, 28, 28).Gap(16).Children(func() {
		ui.Column(c).Width(providerListW).Shrink(0).Gap(8).Children(func() {
			box := ui.Column(c.Key("providers")).Grow(1).MinHeight(0).Radius(cardRadius).Background(p.surface).Border(1, p.border).Padding(4).Gap(1).
				Focusable().FocusRing(false).Label(L("Providers")).Role(ui.RoleList)
			focused := box.FocusWithin()
			if !focused {
				a.set.provNav.keyboard = false
			}
			chosen := -1
			for i, pr := range ps {
				if pr.ID == a.set.provSel {
					chosen = i
				}
			}
			a.set.provNav.keys(box, chosen, len(ps), func(i int) { a.set.provSel = ps[i].ID })
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
						ic := ui.Icon(c, iconPlug).FontSize(iconSize).Shrink(0)
						sub := t.TextMuted
						if st == rowKeyboard {
							sub = t.AccentText
						} else {
							ic.TextColor(t.TextMuted)
						}
						ui.Column(c).Grow(1).MinWidth(0).Children(func() {
							ui.Text(c, pr.Name).FontSize(13).SingleLine().MinWidth(0)
							ui.Text(c, apiLabel(pr.API)).FontSize(11).TextColor(sub).SingleLine().MinWidth(0)
						})
						if pr.ID == def {
							badge(c, L("default"))
						}
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
				if len(ps) == 0 && a.set.provSel != "new" {
					ui.Text(c, L("No provider yet.")).FontSize(12).TextColor(t.TextMuted).Padding(8)
				}
			})
			if smallButton(c.Key("add-provider"), L("Add Provider"), secondary).AlignSelf(ui.Start).Clicked() {
				a.set.provSel, a.set.prov = "new", &providerForm{api: apiLabels[0], web: true}
			}
		})
		ui.Scroll(c).Grow(1).MinWidth(0).Children(func() {
			if a.set.prov == nil {
				ui.Column(c).Grow(1).Center().Gap(12).Padding(40, 0).Children(func() {
					iconTile(c, iconSparkle, 40)
					ui.Text(c, L("Add a provider to give your bots a model.")).FontSize(12).TextColor(t.TextMuted)
				})
				return
			}
			a.providerForm(c, a.set.prov, def)
		})
	})
}

func formOf(p store.Provider) *providerForm {
	return &providerForm{id: p.ID, name: p.Name, api: apiLabel(p.API), base: p.BaseURL, key: p.APIKey,
		models: strings.Join(p.Models, ", "), replay: p.Options["replay_reasoning"], encrypted: p.Options["encrypted_reasoning"], web: p.Options["web_search"]}
}

// providerForm edits a provider in place, saved with Save.
func (a *app) providerForm(c *ui.Context, f *providerForm, def string) {
	t := c.Theme()
	ui.Column(c).Gap(14).Children(func() {
		ui.Column(c).Radius(cardRadius).Background(pal(c).surface).Border(1, pal(c).border).Padding(14).Gap(12).Children(func() {
			ui.Form(c, func() {
				ui.Field(c, L("Name"), func() { textField(c.Key("p-name"+f.id), &f.name, "DeepSeek") })
				ui.Field(c, L("Protocol"), func() { popup(c, &f.api, apiLabels) })
				ui.Field(c, L("Base URL"), func() { textField(c.Key("p-base"+f.id), &f.base, L("the vendor's own")) }).
					Description(L("For example https://api.deepseek.com, or http://localhost:11434/v1 for Ollama."))
				ui.Field(c, L("API key"), func() { textField(c.Key("p-key"+f.id), &f.key, "sk-…").Password() })
				ui.Field(c, L("Models"), func() { textField(c.Key("p-models"+f.id), &f.models, "deepseek-chat, deepseek-reasoner") }).
					Description(L("Comma separated; the first is the default."))
			})
		})
		var rows []func()
		if apiID(f.api) != connect.Chat {
			rows = append(rows, func() {
				switchRow(c, L("Web search"), L("The server searches the web (and reads pages, on Anthropic) for the bot."), &f.web)
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
		card(c, rows...)
		if f.err != "" {
			errorRow(c, f.err)
		}
		if f.saved {
			ui.Text(c, L("Saved.")).FontSize(12).TextColor(pal(c).green)
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if f.id != "" {
				if textButton(c, L("Delete"), danger, false).Clicked() {
					id, name := f.id, f.name
					a.set.confirm = &confirmation{open: true, title: L("Delete %s?", name), message: L("Bots on it move to the default provider."), action: L("Delete"), run: func() {
						a.do(a.st.DeleteProvider(id))
						if a.st.Setting(team.SettingDefaultProvider) == id {
							a.st.SetSetting(team.SettingDefaultProvider, "")
						}
						a.set.provSel, a.set.prov = "", nil
					}}
				}
				if f.id != def {
					if button(c, L("Make Default")).Clicked() {
						a.st.SetSetting(team.SettingDefaultProvider, f.id)
					}
				}
			}
			ui.Spacer(c)
			if f.id == "" {
				if button(c, L("Cancel")).Clicked() {
					a.set.provSel, a.set.prov = "", nil
				}
			}
			if primaryButton(c.Key("p-save"), L("Save"), false).Clicked() && a.saveProvider(f) {
				f.saved = true
				a.set.provSel = f.id
				a.reload()
			}
		})
		_ = t
	})
}

// mcpTemplatePath writes an mcp.json to start from when there is none.
func ensureMCP(path string) {
	if _, err := os.Stat(path); err != nil {
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(mcpTemplate), 0o644)
	}
}
