package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/provider/connect"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/team"
)

func errf(format string, a ...any) error { return fmt.Errorf(L(format), a...) }

// ---- bots ----

type botForm struct {
	open                bool
	id                  string // "" for a new bot
	name, description   string
	providerName, model string
	thinking            string
	icon                string // a botIcons name
	err                 string
}

var thinkingLevels = []string{"Default", "Off", "Low", "Medium", "High"}

func (a *app) providerNames() []string {
	names := []string{L("Default")}
	ps, _ := a.st.Providers()
	for _, p := range ps {
		names = append(names, p.Name)
	}
	return names
}

func (a *app) providerID(name string) string {
	ps, _ := a.st.Providers()
	for _, p := range ps {
		if p.Name == name {
			return p.ID
		}
	}
	return ""
}

func (a *app) providerName(id string) string {
	if p, err := a.st.Provider(id); err == nil {
		return p.Name
	}
	return L("Default")
}

func (a *app) newBot() {
	a.botSheet = &botForm{open: true, providerName: L("Default"), thinking: "Default", icon: "bot"}
}

func (a *app) editBot(id string) {
	b, ok := a.byID[id]
	if !ok {
		return
	}
	th := "Default"
	for _, l := range thinkingLevels {
		if strings.EqualFold(l, b.Thinking) {
			th = l
		}
	}
	a.botSheet = &botForm{open: true, id: b.ID, name: b.Name, description: b.Description, providerName: a.providerName(b.Provider), model: b.Model, thinking: th,
		icon: b.Icon}
}

func (a *app) saveBot(f *botForm) bool {
	th := strings.ToLower(f.thinking)
	if th == "default" {
		th = ""
	}
	b := store.Bot{ID: f.id, Name: f.name, Description: f.description, Provider: a.providerID(f.providerName), Model: strings.TrimSpace(f.model), Thinking: th,
		Icon: f.icon}
	var err error
	if f.id == "" {
		b, err = a.st.CreateBot(b)
		if err == nil {
			a.reload()
			if dm, derr := a.st.DMOf(b.ID); derr == nil {
				a.selectChat(dm.ID)
			}
		}
	} else {
		err = a.st.UpdateBot(b)
		a.reload()
	}
	if err != nil {
		f.err = err.Error()
		return false
	}
	return true
}

func (a *app) botSheetView(c *ui.Context) {
	f := a.botSheet
	ui.Modal(c, &f.open, func() {
		title := L("New Bot")
		action := L("Create")
		if f.id != "" {
			title, action = L("Edit %s", f.name), L("Save")
		}
		ui.Column(c).Width(480).Gap(18).Children(func() {
			dialogTitle(c, title, L("A bot is a name, a description (its system prompt), the model it runs on, and a workspace folder of its own."))
			ui.Form(c, func() {
				ui.Field(c, L("Name"), func() { textField(c, &f.name, L("For example Scout")).AutoFocus() })
				ui.Field(c, L("Look"), func() { a.iconPicker(c, &f.icon) })
				ui.Field(c, L("Description"), func() { textArea(c, &f.description, L("What it does and how: its system prompt."), 4, 10) })
				ui.Field(c, L("Provider"), func() { popup(c, &f.providerName, a.providerNames()) })
				ui.Field(c, L("Model"), func() { textField(c, &f.model, a.modelHint(f.providerName)) })
				ui.Field(c, L("Thinking"), func() { popup(c, &f.thinking, thinkingLevels) })
			})
			if f.err != "" {
				ui.Text(c, f.err).FontSize(12).TextColor(pal(c).red)
			}
			if dialogButtons(c, &f.open, action, strings.TrimSpace(f.name) == "") && a.saveBot(f) {
				f.open = false
			}
		})
	})
}

// iconPicker chooses a bot's look among the icons, as a row of tiles.
func (a *app) iconPicker(c *ui.Context, name *string) {
	t := c.Theme()
	ui.Row(c).Gap(4).Wrap().FocusGroup(ui.Horizontal).Children(func() {
		for _, b := range botIcons {
			chosen := *name == b.name || (*name == "" && b.name == "bot")
			tile := ui.ButtonBase(c.Key("look-"+b.name)).Size(compactHeight, compactHeight).Radius(rowRadius).Center().Label(b.name).Role(ui.RoleRadio).Checked(chosen)
			st := rowPlain
			if chosen {
				st = rowChosen
			}
			styleRow(tile, t, st)
			if tile.Clicked() {
				*name = b.name
			}
			tile.Children(func() {
				ic := ui.Icon(c, b.svg).FontSize(iconSize)
				if !chosen {
					ic.TextColor(t.TextMuted)
				}
			})
		}
	})
}

// modelHint is the model a bot runs on with no model set.
func (a *app) modelHint(providerName string) string {
	b := store.Bot{Provider: a.providerID(providerName)}
	if cfg, err := team.Config(a.st, b, a.fallback); err == nil {
		return cfg.Model
	}
	return L("model id")
}

// ---- groups ----

type groupForm struct {
	open        bool
	id          string
	title, desc string
	picked      map[string]bool
	err         string
}

func (a *app) newGroup() {
	a.groupSheet = &groupForm{open: true, picked: map[string]bool{}}
}

func (a *app) editGroup(ch store.Chat) {
	a.groupSheet = &groupForm{open: true, id: ch.ID, title: ch.Title, desc: ch.Description}
}

func (a *app) saveGroup(f *groupForm) bool {
	if f.id != "" {
		if err := a.st.SetGroupInfo(f.id, f.title, f.desc); err != nil {
			f.err = err.Error()
			return false
		}
		a.reload()
		return true
	}
	var members []string
	for _, b := range a.bots {
		if f.picked[b.ID] {
			members = append(members, b.ID)
		}
	}
	g, err := a.st.CreateGroup(f.title, f.desc, members)
	if err != nil {
		f.err = err.Error()
		return false
	}
	a.reload()
	a.selectChat(g.ID)
	return true
}

func (a *app) groupSheetView(c *ui.Context) {
	t := c.Theme()
	f := a.groupSheet
	ui.Modal(c, &f.open, func() {
		title, action := L("New Group"), L("Create")
		if f.id != "" {
			title, action = L("Edit Group"), L("Save")
		}
		ui.Column(c).Width(480).Gap(18).Children(func() {
			dialogTitle(c, title, L("Members take turns: the ones you @ go first, and each replies or passes."))
			ui.Form(c, func() {
				ui.Field(c, L("Name"), func() { textField(c, &f.title, L("For example Launch")).AutoFocus() })
				ui.Field(c, L("Description"), func() { textArea(c, &f.desc, L("What the group is for; every member reads it."), 2, 6) })
			})
			if f.id == "" {
				ui.Column(c).Gap(1).FocusGroup(ui.Vertical).Children(func() {
					ui.Text(c, L("Members (the first one owns the work)")).FontSize(13).FontWeight(600).Padding(0, 0, 5)
					for _, b := range a.bots {
						on := f.picked[b.ID]
						row := listRow(c.Key("pick-"+b.ID), rowPlain).Label(b.Name).Role(ui.RoleCheckBox).Checked(on)
						if row.Clicked() {
							f.picked[b.ID] = !on
						}
						row.Children(func() {
							iconTile(c, botIcon(b.Icon), 20)
							ui.Text(c, b.Name).FontSize(13).Shrink(0)
							ui.Text(c, firstLine(b.Description)).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
							ui.Spacer(c)
							if on {
								ui.Icon(c, iconCheck).FontSize(iconSize).Shrink(0)
							}
						})
					}
				})
			}
			if f.err != "" {
				ui.Text(c, f.err).FontSize(12).TextColor(pal(c).red)
			}
			if dialogButtons(c, &f.open, action, strings.TrimSpace(f.title) == "") && a.saveGroup(f) {
				f.open = false
			}
		})
	})
}

// ---- providers ----

type providerForm struct {
	open                   bool
	id                     string
	name, api, base, key   string
	models                 string // comma separated
	replay, encrypted, web bool
	err                    string
}

var apiLabels = []string{"Anthropic Messages", "OpenAI Chat Completions", "OpenAI Responses"}
var apiIDs = []string{connect.Anthropic, connect.Chat, connect.Responses}

func apiLabel(id string) string {
	for i, x := range apiIDs {
		if x == id {
			return apiLabels[i]
		}
	}
	return apiLabels[0]
}

func apiID(label string) string {
	for i, x := range apiLabels {
		if x == label {
			return apiIDs[i]
		}
	}
	return connect.Anthropic
}

func (a *app) editProvider(p store.Provider) {
	a.provSheet = &providerForm{open: true, id: p.ID, name: p.Name, api: apiLabel(p.API), base: p.BaseURL, key: p.APIKey,
		models: strings.Join(p.Models, ", "), replay: p.Options["replay_reasoning"], encrypted: p.Options["encrypted_reasoning"], web: p.Options["web_search"]}
}

func (a *app) saveProvider(f *providerForm) bool {
	var models []string
	for _, m := range strings.Split(f.models, ",") {
		if m = strings.TrimSpace(m); m != "" {
			models = append(models, m)
		}
	}
	if strings.TrimSpace(f.name) == "" || len(models) == 0 {
		f.err = L("A provider needs a name and at least one model.")
		return false
	}
	p := store.Provider{ID: f.id, Name: strings.TrimSpace(f.name), API: apiID(f.api), BaseURL: strings.TrimSpace(f.base), APIKey: strings.TrimSpace(f.key), Models: models,
		Options: map[string]bool{"replay_reasoning": f.replay, "encrypted_reasoning": f.encrypted, "web_search": f.web && apiID(f.api) != connect.Chat}}
	p, err := a.st.SaveProvider(p)
	if err != nil {
		f.err = err.Error()
		return false
	}
	if a.st.Setting(team.SettingDefaultProvider) == "" {
		a.st.SetSetting(team.SettingDefaultProvider, p.ID)
	}
	return true
}

func (a *app) providerSheetView(c *ui.Context) {
	f := a.provSheet
	ui.Modal(c, &f.open, func() {
		title := L("New Provider")
		if f.id != "" {
			title = L("Edit Provider")
		}
		ui.Column(c).Width(500).Gap(18).Children(func() {
			dialogTitle(c, title, L("Any server that speaks one of these protocols: Anthropic, DeepSeek, OpenAI, OpenRouter, Ollama, vLLM…"))
			ui.Form(c, func() {
				ui.Field(c, L("Name"), func() { textField(c, &f.name, "DeepSeek").AutoFocus() })
				ui.Field(c, L("Protocol"), func() { popup(c, &f.api, apiLabels) })
				ui.Field(c, L("Base URL"), func() { textField(c, &f.base, L("the vendor's own")) }).
					Description(L("For example https://api.deepseek.com, or http://localhost:11434/v1 for Ollama."))
				ui.Field(c, L("API key"), func() { textField(c, &f.key, "sk-…").Password() })
				ui.Field(c, L("Models"), func() { textField(c, &f.models, "deepseek-chat, deepseek-reasoner") }).
					Description(L("Comma separated; the first is the default."))
				if apiID(f.api) != connect.Chat {
					ui.Field(c, L("Web search"), func() { ui.Switch(c, &f.web) }).Description(L("The server searches the web (and reads pages, on Anthropic) for the bot."))
				}
				if apiID(f.api) == connect.Chat {
					ui.Field(c, L("Send reasoning back"), func() { ui.Switch(c, &f.replay) }).Description(L("As reasoning_content, which DeepSeek's thinking mode takes."))
				}
				if apiID(f.api) == connect.Responses {
					ui.Field(c, L("Encrypted reasoning"), func() { ui.Switch(c, &f.encrypted) }).Description(L("Keeps reasoning across tool calls on OpenAI's API."))
				}
			})
			if f.err != "" {
				ui.Text(c, f.err).FontSize(12).TextColor(pal(c).red)
			}
			if dialogButtons(c, &f.open, L("Save"), false) && a.saveProvider(f) {
				f.open = false
			}
		})
	})
}

// settingsView is the Settings sheet: providers, the default one, MCP
// servers, and the app's preferences.
func (a *app) settingsView(c *ui.Context) {
	t := c.Theme()
	ui.Modal(c, &a.settings, func() {
		ui.Column(c).Width(540).Gap(14).Children(func() {
			dialogTitle(c, L("Settings"), "")
			ui.Scroll(c).MaxHeight(560).Children(func() {
				ui.Column(c).Gap(6).Padding(0, 12, 0, 0).Children(func() {
					sectionTitle(c, L("Providers"))
					ps, _ := a.st.Providers()
					def := a.st.Setting(team.SettingDefaultProvider)
					if len(ps) == 0 {
						note := L("No provider yet: bots run on the environment's (GOBOT_API_KEY or ANTHROPIC_API_KEY).")
						if a.fallback.APIKey == "" {
							note = L("No provider yet. Add one to start chatting.")
						}
						ui.Text(c, note).FontSize(12).TextColor(t.TextMuted)
					}
					ui.Column(c).Gap(1).Children(func() {
						for _, p := range ps {
							p := p
							isDef := p.ID == def || (def == "" && p.ID == ps[0].ID)
							row := ui.Row(c.Key(p.ID)).Height(40).Padding(0, 8).Gap(8).Radius(rowRadius).AlignItems(ui.Center).Label(p.Name)
							styleRow(row, t, rowPlain)
							var action func()
							menu := func(m *ui.Menu) {
								if m.Item(L("Make Default")).Disabled(isDef).Chosen() {
									action = func() { a.st.SetSetting(team.SettingDefaultProvider, p.ID) }
								}
								if m.Item(L("Edit…")).Chosen() {
									action = func() { a.settings = false; a.editProvider(p) }
								}
								m.Separator()
								if m.Item(L("Delete")).Chosen() {
									action = func() {
										a.settings = false
										a.ask(L("Delete %s?", p.Name), L("Bots on it move to the default provider."), L("Delete"), func() {
											a.do(a.st.DeleteProvider(p.ID))
											if a.st.Setting(team.SettingDefaultProvider) == p.ID {
												a.st.SetSetting(team.SettingDefaultProvider, "")
											}
										})
									}
								}
							}
							row.ContextMenu(menu)
							if row.DoubleClicked() {
								action = func() { a.settings = false; a.editProvider(p) }
							}
							row.Children(func() {
								ui.Icon(c, iconPlug).FontSize(iconSize).TextColor(t.TextMuted).Shrink(0)
								ui.Column(c).Grow(1).MinWidth(0).Children(func() {
									ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
										ui.Text(c, p.Name).FontSize(13).FontWeight(600).SingleLine().Shrink(1).MinWidth(0)
										if isDef {
											badge(c, L("default"))
										}
									})
									ui.Text(c, apiLabel(p.API)+" · "+strings.Join(p.Models, ", ")).FontSize(11).TextColor(t.TextMuted).SingleLine().MinWidth(0)
								})
								if smallButton(c.Key("edit-"+p.ID), L("Edit…"), secondary).Clicked() {
									action = func() { a.settings = false; a.editProvider(p) }
								}
								iconButton(c.Key("more-"+p.ID), iconMore, L("More")).Menu(menu)
							})
							if action != nil {
								action()
							}
						}
					})
					if smallButton(c.Key("add-provider"), L("Add Provider…"), secondary).AlignSelf(ui.Start).Clicked() {
						a.settings = false
						a.provSheet = &providerForm{open: true, api: apiLabels[0], web: true}
					}
					a.pluginsView(c)
					a.prefsView(c)
				})
			})
			ui.Row(c).Justify(ui.End).Children(func() {
				if primaryButton(c, L("Done"), false).Clicked() {
					a.settings = false
				}
			})
		})
	})
}

// confirmView asks before a delete, as an alert.
func (a *app) confirmView(c *ui.Context) {
	f := a.confirm
	if ui.AlertDialog(c, &f.open, f.title, f.message, L("Cancel"), f.action) == 1 {
		f.run()
	}
}

// sheets draws whichever sheet is up.
func (a *app) sheets(c *ui.Context) {
	if a.botSheet != nil {
		a.botSheetView(c)
		if !a.botSheet.open {
			a.botSheet = nil
		}
	}
	if a.groupSheet != nil {
		a.groupSheetView(c)
		if !a.groupSheet.open {
			a.groupSheet = nil
		}
	}
	if a.provSheet != nil {
		a.providerSheetView(c)
		if !a.provSheet.open {
			a.provSheet = nil
			a.settings = true // back to Settings
		}
	}
	if a.settings {
		a.settingsView(c)
	}
	if a.confirm != nil {
		a.confirmView(c)
		if !a.confirm.open {
			a.confirm = nil
		}
	}
	if a.routine != nil {
		a.routineSheetView(c)
		if a.routine != nil && !a.routine.open {
			a.routine = nil
		}
	}
	if a.memSheet != nil {
		a.memorySheetView(c)
		if !a.memSheet.open {
			a.memSheet = nil
		}
	}
	if a.palette != nil {
		a.paletteView(c)
	}
}

func (a *app) hasSheet() bool {
	return a.botSheet != nil || a.groupSheet != nil || a.provSheet != nil || a.settings || a.confirm != nil || a.palette != nil || a.memSheet != nil || a.routine != nil
}
