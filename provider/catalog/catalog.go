// Package catalog knows the models the adapters offer: context window,
// output cap, whether they take images, and their prices, from an embedded
// models.dev snapshot. An unknown model has no entry; callers fall back.
package catalog

import (
	_ "embed"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/zucchiniEvader/baggy/agent"
)

// Rates are dollars per million tokens.
type Rates struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

// Tier is the rates of a request whose input is above InputTokensAbove.
type Tier struct {
	InputTokensAbove int   `json:"input_tokens_above"`
	Rates            Rates `json:"rates"`
}

type Model struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Vendor        string `json:"vendor"`
	ContextWindow int    `json:"context_window"`
	MaxOutput     int    `json:"max_output"`
	Reasoning     bool   `json:"reasoning"`
	Images        bool   `json:"images"`
	Rates         Rates  `json:"rates"`
	Tiers         []Tier `json:"tiers"`
}

//go:embed catalog.json
var raw []byte

var models = func() []Model {
	var c struct{ Models []Model }
	if err := json.Unmarshal(raw, &c); err != nil {
		panic(err)
	}
	return c.Models
}()

// Models lists every entry.
func Models() []Model { return models }

// Find looks a model id up: a gateway's "vendor/" prefix is ignored, and a
// dated variant (claude-haiku-4-5-20251001) finds its base entry.
func Find(id string) (Model, bool) {
	if i := strings.LastIndexByte(id, '/'); i >= 0 {
		id = id[i+1:]
	}
	for _, m := range models {
		if m.ID == id {
			return m, true
		}
	}
	for _, m := range models {
		if rest, ok := strings.CutPrefix(id, m.ID+"-"); ok && rest != "" && strings.IndexFunc(rest, func(r rune) bool { return !unicode.IsDigit(r) }) < 0 {
			return m, true
		}
	}
	return Model{}, false
}

// Cost is what a call cost in dollars: every token at the rates of the tier
// its input lands in.
func (m Model) Cost(u agent.Usage) float64 {
	r, above := m.Rates, -1
	input := u.Input + u.CacheRead + u.CacheWrite
	for _, t := range m.Tiers {
		if input > t.InputTokensAbove && t.InputTokensAbove > above {
			r, above = t.Rates, t.InputTokensAbove
		}
	}
	return (float64(u.Input)*r.Input + float64(u.Output)*r.Output + float64(u.CacheRead)*r.CacheRead + float64(u.CacheWrite)*r.CacheWrite) / 1e6
}

// DefaultWindow is the context window assumed for a model the catalog does
// not know.
const DefaultWindow = 128000

// Window is a model's context window, DefaultWindow when unknown.
func Window(id string) int {
	if m, ok := Find(id); ok && m.ContextWindow > 0 {
		return m.ContextWindow
	}
	return DefaultWindow
}

// Images reports whether a model takes images; an unknown model is assumed
// to.
func Images(id string) bool {
	if m, ok := Find(id); ok {
		return m.Images
	}
	return true
}
