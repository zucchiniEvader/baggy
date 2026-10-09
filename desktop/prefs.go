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
	approveShell bool
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
	p.approveShell = get("pref.approve_shell", false)
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
	p.st.SetSetting("pref.approve_shell", b(p.approveShell))
	p.st.SetSetting("pref.keep_running", b(p.keepRunning))
	p.st.SetSetting("pref.inspector", b(p.inspector))
	p.st.SetSetting("pref.language", p.language)
	p.st.SetSetting("pref.side_width", strconv.FormatFloat(float64(p.sideWidth), 'f', 0, 32))
}

// apply puts the preferences into effect.
func (a *app) applyPrefs() {
	a.approvals.on.Store(a.prefs.approveShell)
	setLanguage(a.prefs.language, a.locale)
}

func (a *app) prefsView(c *ui.Context) {
	p := a.prefs
	changed := false
	ui.Text(c, L("Preferences")).Bold()
	ui.Form(c, func() {
		ui.Field(c, L("Show thinking"), func() { changed = ui.Switch(c, &p.showThinking).Changed() || changed })
		ui.Field(c, L("Ask before shell commands"), func() { changed = ui.Switch(c, &p.approveShell).Changed() || changed }).
			Description(L("Each command waits on its card for Allow or Deny."))
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
