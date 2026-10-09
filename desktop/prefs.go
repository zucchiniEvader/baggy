package main

import (
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/store"
)

// prefs are the app's preferences, kept in the store's settings.
type prefs struct {
	st           *store.Store
	showThinking bool
	commands     string // ModeRun, ModeAsk, ModeReview
	keepRunning  bool   // closing the window hides it; the tray brings it back
	language     string // "Auto", "English", "中文"
	sideWidth    float32
	inspector    bool
}

var languages = []string{"Auto", "English", "中文"}

func loadPrefs(st *store.Store) *prefs {
	p := &prefs{st: st, showThinking: true, keepRunning: true, language: "Auto", sideWidth: 240}
	get := func(key string, def bool) bool {
		switch st.Setting(key) {
		case "1":
			return true
		case "0":
			return false
		}
		return def
	}
	p.showThinking = get("pref.show_thinking", true)
	p.commands = st.Setting("pref.commands")
	if p.commands == "" {
		p.commands = ModeRun
		if get("pref.approve_shell", false) { // the switch it replaces
			p.commands = ModeAsk
		}
	}
	p.keepRunning = get("pref.keep_running", true)
	p.inspector = get("pref.inspector", false)
	if l := st.Setting("pref.language"); l != "" {
		p.language = l
	}
	if w, err := strconv.ParseFloat(st.Setting("pref.side_width"), 32); err == nil && w >= 160 {
		p.sideWidth = float32(w)
	}
	return p
}

func (p *prefs) save() {
	b := func(v bool) string {
		if v {
			return "1"
		}
		return "0"
	}
	p.st.SetSetting("pref.show_thinking", b(p.showThinking))
	p.st.SetSetting("pref.commands", p.commands)
	p.st.SetSetting("pref.keep_running", b(p.keepRunning))
	p.st.SetSetting("pref.inspector", b(p.inspector))
	p.st.SetSetting("pref.language", p.language)
	p.st.SetSetting("pref.side_width", strconv.FormatFloat(float64(p.sideWidth), 'f', 0, 32))
}

// apply puts the preferences into effect.
func (a *app) applyPrefs() {
	a.approvals.setMode(a.prefs.commands)
	setLanguage(a.prefs.language, a.locale)
}

func (a *app) prefsView(c *ui.Context) {
	p := a.prefs
	changed := false
	ui.Text(c, L("Preferences")).Bold()
	ui.Form(c, func() {
		ui.Field(c, L("Show thinking"), func() { changed = ui.Switch(c, &p.showThinking).Changed() || changed })
		ui.Field(c, L("Commands"), func() {
			choice := modeLabel(p.commands)
			if ui.Select(c, &choice, modeLabels()).Changed() {
				p.commands, changed = modeOf(choice), true
			}
		}).Description(L("Before a shell command or a plugin action that changes something: run it, ask every time, or let the bot's model review it and ask only when in doubt. Always Allow keeps an exact command."))
		ui.Field(c, L("Keep running when closed"), func() { changed = ui.Switch(c, &p.keepRunning).Changed() || changed }).
			Description(L("Bots keep working; the tray icon opens the window again."))
		ui.Field(c, L("Language"), func() { changed = ui.Select(c, &p.language, languages).Changed() || changed })
	})
	if changed {
		p.save()
		a.applyPrefs()
	}
}

// layoutChanged saves the sidebar's width and the inspector after a change.
func (a *app) layoutChanged() {
	if a.prefs.sideWidth != a.sideWidth || a.prefs.inspector != a.inspector {
		a.prefs.sideWidth, a.prefs.inspector = a.sideWidth, a.inspector
		a.prefs.save()
	}
}

var modes = []string{ModeRun, ModeAsk, ModeReview}

func modeLabel(m string) string {
	switch m {
	case ModeAsk:
		return L("Ask every time")
	case ModeReview:
		return L("Auto-review")
	}
	return L("Run")
}

func modeLabels() []string {
	var out []string
	for _, m := range modes {
		out = append(out, modeLabel(m))
	}
	return out
}

func modeOf(label string) string {
	for _, m := range modes {
		if modeLabel(m) == label {
			return m
		}
	}
	return ModeRun
}
