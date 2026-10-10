package main

import (
	"strconv"
	"time"

	"github.com/spike/gobot/store"
)

// prefs are the app's preferences, kept in the store's settings.
type prefs struct {
	st           *store.Store
	showThinking bool
	commands     string // ModeRun, ModeAsk, ModeReview
	keepRunning  bool   // closing the window hides it; the tray brings it back
	// Compaction: the share of the window a chat may fill while the cache is
	// warm, and once it has expired (0: no earlier compaction), and how long
	// a provider keeps the cache.
	compactWarm, compactCold float64
	cacheTTL                 time.Duration
	language                 string // "Auto", "English", "中文"
	sideWidth                float32
	inspector                bool
}

// languages are the stored choices; languageLabel names them.
var languages = []string{"Auto", "English", "中文"}

func languageLabel(l string) string {
	switch l {
	case "English":
		return "English"
	case "中文":
		return "简体中文"
	}
	return L("Follow System")
}

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
	p.compactWarm, p.compactCold, p.cacheTTL = 0.8, 0.4, 5*time.Minute
	if v, err := strconv.ParseFloat(st.Setting("pref.compact_warm"), 64); err == nil && v > 0 {
		p.compactWarm = v
	}
	if v, err := strconv.ParseFloat(st.Setting("pref.compact_cold"), 64); err == nil && v >= 0 {
		p.compactCold = v
	}
	if v, err := time.ParseDuration(st.Setting("pref.cache_ttl")); err == nil && v > 0 {
		p.cacheTTL = v
	}
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
	p.st.SetSetting("pref.compact_warm", strconv.FormatFloat(p.compactWarm, 'f', 2, 64))
	p.st.SetSetting("pref.compact_cold", strconv.FormatFloat(p.compactCold, 'f', 2, 64))
	p.st.SetSetting("pref.cache_ttl", p.cacheTTL.String())
	p.st.SetSetting("pref.inspector", b(p.inspector))
	p.st.SetSetting("pref.language", p.language)
	p.st.SetSetting("pref.side_width", strconv.FormatFloat(float64(p.sideWidth), 'f', 0, 32))
}

// apply puts the preferences into effect.
func (a *app) applyPrefs() {
	a.approvals.setMode(a.prefs.commands)
	a.policy.Store(compactPolicy{a.prefs.compactWarm, a.prefs.compactCold, a.prefs.cacheTTL})
	setLanguage(a.prefs.language, a.locale)
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

// compactPolicy is the compaction preferences as the turns read them, from
// their goroutines (app.policy holds one).
type compactPolicy struct {
	warm, cold float64
	ttl        time.Duration
}

// compactionPolicy is team.Compaction's Policy over the preferences.
func (a *app) compactionPolicy() (float64, float64, time.Duration) {
	p, _ := a.policy.Load().(compactPolicy)
	return p.warm, p.cold, p.ttl
}
