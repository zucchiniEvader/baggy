package team

import (
	"errors"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/provider"
	"github.com/spike/gobot/provider/connect"
	"github.com/spike/gobot/store"
)

// ErrNoProvider is a bot's model when no provider is set up.
var ErrNoProvider = errors.New("no model is set up yet: add a provider in Settings")

// SettingDefaultProvider names the provider a bot without one runs on.
const SettingDefaultProvider = "default_provider"

// Config is the connect.Config a bot runs on: its provider (else the default
// one, else the first), and its model (else the provider's first). With no
// provider stored, fallback is used when it has a key.
func Config(st *store.Store, b store.Bot, fallback connect.Config) (connect.Config, error) {
	ps, err := st.Providers()
	if err != nil {
		return connect.Config{}, err
	}
	var p *store.Provider
	for _, want := range []string{b.Provider, st.Setting(SettingDefaultProvider)} {
		for i := range ps {
			if want != "" && ps[i].ID == want {
				p = &ps[i]
				break
			}
		}
		if p != nil {
			break
		}
	}
	if p == nil && len(ps) > 0 {
		p = &ps[0]
	}
	if p == nil {
		if fallback.APIKey == "" {
			return connect.Config{}, ErrNoProvider
		}
		if b.Model != "" {
			fallback.Model = b.Model
		}
		return fallback, nil
	}
	c := connect.Config{API: p.API, ID: p.ID, BaseURL: p.BaseURL, APIKey: p.APIKey, Model: b.Model,
		ReplayReasoning: p.Options["replay_reasoning"], EncryptedReasoning: p.Options["encrypted_reasoning"], NoImages: p.Options["no_images"], WebSearch: p.Options["web_search"]}
	if c.Model == "" && len(p.Models) > 0 {
		c.Model = p.Models[0]
	}
	if c.Model == "" {
		return connect.Config{}, errors.New("provider " + p.Name + " lists no model")
	}
	return c, nil
}

// Models resolves bots to models through the store's providers.
func Models(st *store.Store, fallback connect.Config) func(store.Bot) (agent.Model, error) {
	return func(b store.Bot) (agent.Model, error) {
		c, err := Config(st, b, fallback)
		if err != nil {
			return nil, err
		}
		p, err := connect.New(c)
		if err != nil {
			return nil, err
		}
		return provider.Model{Provider: p, Thinking: provider.ThinkingLevel(b.Thinking)}, nil
	}
}

func connectFallback(t *Team) connect.Config { return t.Fallback }
