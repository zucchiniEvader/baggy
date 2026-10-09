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
	a.botSheet = &botForm{open: true, providerName: L("Default"), thinking: "Default"}
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
	a.botSheet = &botForm{open: true, id: b.ID, name: b.Name, description: b.Description, providerName: a.providerName(b.Provider), model: b.Model, thinking: th}
}

func (a *app) saveBot(f *botForm) bool {
	th := strings.ToLower(f.thinking)
	if th == "default" {
		th = ""
	}
	b := store.Bot{ID: f.id, Name: f.name, Description: f.description, Provider: a.providerID(f.providerName), Model: strings.TrimSpace(f.model), Thinking: th}
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
		if f.id != "" {
			title = L("Edit %s", f.name)
		}
		ui.Column(c).Width(460).Gap(12).Children(func() {
			ui.Text(c, title).Bold().FontSize(16)
			ui.Form(c, func() {
				ui.Field(c, L("Name"), func() { ui.TextInput(c, &f.name).AutoFocus() })
				ui.Field(c, L("Description"), func() { ui.TextArea(c, &f.description).Lines(4, 10) }).
					Description(L("What it does and how: its system prompt."))
				ui.Field(c, L("Provider"), func() { ui.Select(c, &f.providerName, a.providerNames()) })
				ui.Field(c, L("Model"), func() { ui.TextInput(c, &f.model).Placeholder(a.modelHint(f.providerName)) })
				ui.Field(c, L("Thinking"), func() { ui.Select(c, &f.thinking, thinkingLevels) })
			})
			if f.err != "" {
				ui.Text(c, f.err).TextColor(c.Theme().Danger)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, L("Cancel")).Clicked() {
					f.open = false
				}
				label := L("Create")
				if f.id != "" {
					label = L("Save")
				}
				if ui.PrimaryButton(c, label).Disabled(strings.TrimSpace(f.name) == "").Clicked() && a.saveBot(f) {
					f.open = false
				}
			})
		})
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
	f := a.groupSheet
	ui.Modal(c, &f.open, func() {
		title := L("New Group")
		if f.id != "" {
			title = L("Edit Group")
		}
		ui.Column(c).Width(460).Gap(12).Children(func() {
			ui.Text(c, title).Bold().FontSize(16)
			ui.Form(c, func() {
				ui.Field(c, L("Name"), func() { ui.TextInput(c, &f.title).AutoFocus() })
				ui.Field(c, L("Description"), func() { ui.TextArea(c, &f.desc).Lines(2, 6) }).Description(L("What the group is for; every member reads it."))
			})
			if f.id == "" {
				ui.Text(c, L("Members (the first one owns the work)")).FontSize(12).Bold()
				ui.Column(c).Gap(4).Children(func() {
					for _, b := range a.bots {
						on := f.picked[b.ID]
						if ui.Checkbox(c.Key("pick-"+b.ID), &on, b.Name).Changed() {
							f.picked[b.ID] = on
						}
					}
				})
			}
			if f.err != "" {
				ui.Text(c, f.err).TextColor(c.Theme().Danger)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, L("Cancel")).Clicked() {
					f.open = false
				}
				label := L("Create")
				if f.id != "" {
					label = L("Save")
				}
				if ui.PrimaryButton(c, label).Disabled(strings.TrimSpace(f.title) == "").Clicked() && a.saveGroup(f) {
					f.open = false
				}
			})
		})
	})
}

// ---- providers ----

type providerForm struct {
	open                 bool
	id                   string
	name, api, base, key string
	models               string // comma separated
	replay, encrypted    bool
	err                  string
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
		models: strings.Join(p.Models, ", "), replay: p.Options["replay_reasoning"], encrypted: p.Options["encrypted_reasoning"]}
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
		Options: map[string]bool{"replay_reasoning": f.replay, "encrypted_reasoning": f.encrypted}}
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
		ui.Column(c).Width(480).Gap(12).Children(func() {
			title := L("New Provider")
			if f.id != "" {
				title = L("Edit Provider")
			}
			ui.Text(c, title).Bold().FontSize(16)
			ui.Form(c, func() {
				ui.Field(c, L("Name"), func() { ui.TextInput(c, &f.name).AutoFocus().Placeholder("DeepSeek") })
				ui.Field(c, L("Protocol"), func() { ui.Select(c, &f.api, apiLabels) })
				ui.Field(c, L("Base URL"), func() { ui.TextInput(c, &f.base).Placeholder(L("the vendor's own")) }).
					Description(L("For example https://api.deepseek.com, or http://localhost:11434/v1 for Ollama."))
				ui.Field(c, L("API key"), func() { ui.TextInput(c, &f.key).Password() })
				ui.Field(c, L("Models"), func() { ui.TextInput(c, &f.models).Placeholder("deepseek-chat, deepseek-reasoner") }).
					Description(L("Comma separated; the first is the default."))
				if apiID(f.api) == connect.Chat {
					ui.Field(c, L("Send reasoning back"), func() { ui.Switch(c, &f.replay) }).Description(L("As reasoning_content, which DeepSeek's thinking mode takes."))
				}
				if apiID(f.api) == connect.Responses {
					ui.Field(c, L("Encrypted reasoning"), func() { ui.Switch(c, &f.encrypted) }).Description(L("Keeps reasoning across tool calls on OpenAI's API."))
				}
			})
			if f.err != "" {
				ui.Text(c, f.err).TextColor(c.Theme().Danger)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, L("Cancel")).Clicked() {
					f.open = false
				}
				if ui.PrimaryButton(c, L("Save")).Clicked() && a.saveProvider(f) {
					f.open = false
				}
			})
		})
	})
}

// settingsView is the Settings sheet: providers, the default one, and the
// app's preferences.
func (a *app) settingsView(c *ui.Context) {
	t := c.Theme()
	ui.Modal(c, &a.settings, func() {
		ui.Column(c).Width(520).Gap(14).Children(func() {
			ui.Text(c, L("Settings")).Bold().FontSize(16)
			ui.Text(c, L("Providers")).Bold()
			ps, _ := a.st.Providers()
			def := a.st.Setting(team.SettingDefaultProvider)
			if len(ps) == 0 {
				note := L("No provider yet: bots run on the environment's (GOBOT_API_KEY or ANTHROPIC_API_KEY).")
				if a.fallback.APIKey == "" {
					note = L("No provider yet. Add one to start chatting.")
				}
				ui.Text(c, note).FontSize(12).TextColor(t.TextMuted)
			}
			ui.Column(c).Gap(6).Children(func() {
				for _, p := range ps {
					ui.Row(c.Key(p.ID)).Gap(8).AlignItems(ui.Center).Padding(6, 10).Radius(8).Background(t.Surface).Children(func() {
						ui.Column(c).Grow(1).MinWidth(0).Children(func() {
							name := p.Name
							if p.ID == def || (def == "" && p.ID == ps[0].ID) {
								name += " · " + L("default")
							}
							ui.Text(c, name).Bold().SingleLine()
							ui.Text(c, apiLabel(p.API)+" · "+strings.Join(p.Models, ", ")).FontSize(11).TextColor(t.TextMuted).SingleLine()
						})
						if p.ID != def {
							if ui.Button(c, L("Make Default")).Clicked() {
								a.st.SetSetting(team.SettingDefaultProvider, p.ID)
							}
						}
						if ui.Button(c, L("Edit…")).Clicked() {
							a.settings = false
							a.editProvider(p)
						}
						if ui.Button(c, L("Delete")).Clicked() {
							id := p.ID
							a.settings = false
							a.ask(L("Delete %s?", p.Name), L("Bots on it move to the default provider."), L("Delete"), func() {
								a.do(a.st.DeleteProvider(id))
								if a.st.Setting(team.SettingDefaultProvider) == id {
									a.st.SetSetting(team.SettingDefaultProvider, "")
								}
							})
						}
					})
				}
			})
			if ui.Button(c, L("Add Provider…")).Clicked() {
				a.settings = false
				a.provSheet = &providerForm{open: true, api: apiLabels[0]}
			}
			ui.Divider(c)
			a.prefsView(c)
			ui.Row(c).Justify(ui.End).Children(func() {
				if ui.PrimaryButton(c, L("Done")).Clicked() {
					a.settings = false
				}
			})
		})
	})
}

func (a *app) confirmView(c *ui.Context) {
	f := a.confirm
	ui.Modal(c, &f.open, func() {
		ui.Column(c).Width(400).Gap(12).Children(func() {
			ui.Text(c, f.title).Bold().FontSize(15)
			ui.Text(c, f.message)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, L("Cancel")).Clicked() {
					f.open = false
				}
				if ui.PrimaryButton(c, f.action).Clicked() {
					f.open = false
					f.run()
				}
			})
		})
	})
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
	if a.palette != nil {
		a.paletteView(c)
	}
}

func (a *app) hasSheet() bool {
	return a.botSheet != nil || a.groupSheet != nil || a.provSheet != nil || a.settings || a.confirm != nil || a.palette != nil
}
