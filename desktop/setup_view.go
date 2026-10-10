package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/provider/connect"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/team"
)

// preset is a provider the setup offers ready-made.
type preset struct {
	name, api, base, key string
	models               []string
	web, encrypted       bool
	custom               bool // asks for the base URL and model too
}

var presets = []preset{
	{name: "DeepSeek", api: connect.Anthropic, base: "https://api.deepseek.com/anthropic", models: []string{"deepseek-flash", "deepseek-v4-pro"}, web: true},
	{name: "Anthropic", api: connect.Anthropic, models: []string{"claude-sonnet-5-5", "claude-opus-5-5", "claude-haiku-5-5"}, web: true},
	{name: "OpenAI", api: connect.Responses, models: []string{"gpt-6.1-sol", "gpt-6-luna"}, web: true, encrypted: true},
	{name: "OpenRouter", api: connect.Chat, base: "https://openrouter.ai/api/v1", models: []string{"anthropic/claude-sonnet-5-5"}},
	{name: "Ollama", api: connect.Chat, base: "http://localhost:11434/v1", key: "ollama", custom: true},
	{name: L("Other"), custom: true},
}

// setup is the form of the first provider.
type setup struct {
	preset            string
	key, base, models string
	api               string
	err               string
}

func presetNamed(name string) preset {
	for _, p := range presets {
		if p.name == name {
			return p
		}
	}
	return presets[0]
}

// setupView is the window before any model is set up: the form that adds
// the first provider, in place of the chat.
func (a *app) setupView(c *ui.Context) {
	t := c.Theme()
	if a.setup == nil {
		a.setup = &setup{preset: presets[0].name, api: apiLabels[0]}
	}
	f := a.setup
	p := presetNamed(f.preset)
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Grow(1).Center().Padding(48, 28).Children(func() {
			ui.Column(c).Width(440).Gap(18).Children(func() {
				ui.Column(c).Gap(12).AlignItems(ui.Center).Children(func() {
					iconTile(c, iconSparkle, 40)
					ui.Column(c).Gap(4).AlignItems(ui.Center).Children(func() {
						ui.Text(c, L("Set up a model")).FontSize(17).Bold()
						ui.Text(c, L("Your bots think with a model of your choice. The key stays on this computer.")).FontSize(12).TextColor(t.TextMuted).TextAlign(ui.Center)
					})
				})
				ui.Form(c, func() {
					ui.Field(c, L("Provider"), func() {
						names := make([]string, len(presets))
						for i, x := range presets {
							names[i] = x.name
						}
						was := f.preset
						popup(c, &f.preset, names)
						if f.preset != was {
							f.err, f.base, f.models = "", "", ""
						}
					})
					if p.custom {
						if p.api == "" {
							ui.Field(c, L("Protocol"), func() { popup(c, &f.api, apiLabels) })
						}
						ui.Field(c, L("Base URL"), func() { textField(c, &f.base, p.base) })
						ui.Field(c, L("Models"), func() { textField(c, &f.models, "llama3.2, qwen3") }).Description(L("Comma separated; the first is the default."))
					}
					if p.key == "" {
						ui.Field(c, L("API key"), func() { textField(c, &f.key, "sk-…").Password().AutoFocus() })
					}
				})
				if f.err != "" {
					errorRow(c, f.err)
				}
				ui.Row(c).Justify(ui.End).Children(func() {
					ready := p.key != "" || strings.TrimSpace(f.key) != ""
					if p.custom {
						ready = ready && strings.TrimSpace(f.models) != "" && (p.base != "" || strings.TrimSpace(f.base) != "")
					}
					if primaryButton(c.Key("setup-save"), L("Start"), !ready).Clicked() {
						a.saveSetup(f, p)
					}
				})
			})
		})
	})
}

func (a *app) saveSetup(f *setup, p preset) {
	pr := store.Provider{Name: p.name, API: p.api, BaseURL: p.base, APIKey: strings.TrimSpace(f.key), Models: p.models,
		Options: map[string]bool{"web_search": p.web, "encrypted_reasoning": p.encrypted}}
	if p.api == "" {
		pr.API = apiID(f.api)
	}
	if p.key != "" {
		pr.APIKey = p.key
	}
	if p.custom {
		if b := strings.TrimSpace(f.base); b != "" {
			pr.BaseURL = b
		}
		pr.Models = nil
		for _, m := range strings.Split(f.models, ",") {
			if m = strings.TrimSpace(m); m != "" {
				pr.Models = append(pr.Models, m)
			}
		}
	}
	saved, err := a.st.SaveProvider(pr)
	if err != nil {
		f.err = err.Error()
		return
	}
	a.st.SetSetting(team.SettingDefaultProvider, saved.ID)
	a.setup = nil
	a.reload()
	if v := a.views[a.sel]; v != nil {
		v.focus = true
	}
}
