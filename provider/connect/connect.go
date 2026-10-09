// Package connect builds a provider from a protocol name and its settings.
package connect

import (
	"fmt"
	"os"

	"github.com/spike/gobot/provider"
	"github.com/spike/gobot/provider/anthropic"
	"github.com/spike/gobot/provider/catalog"
	"github.com/spike/gobot/provider/openai"
)

// The wire protocols a Config can name.
const (
	Anthropic = "anthropic" // Messages API
	Chat      = "chat"      // Chat Completions
	Responses = "responses" // Responses API
)

type Config struct {
	API     string // Anthropic, Chat or Responses; "" is Anthropic
	ID      string // provider id stamped on replies; "" is the adapter's default
	BaseURL string // "" is the vendor's own
	APIKey  string
	Model   string

	// ReplayReasoning: Chat sends own thinking back as reasoning_content.
	ReplayReasoning bool
	// EncryptedReasoning: Responses replays encrypted reasoning items.
	EncryptedReasoning bool
	// NoImages marks a text-only model the catalog does not know.
	NoImages bool
	// WebSearch declares the server's web search (and fetch, on Anthropic):
	// the Messages API and Responses; Chat Completions has none.
	WebSearch bool
}

func New(c Config) (provider.Provider, error) {
	switch c.API {
	case Anthropic, "":
		p := anthropic.New(c.ID, c.BaseURL, c.APIKey, c.Model)
		p.NoImages = c.NoImages || !catalog.Images(c.Model)
		if c.WebSearch {
			p.ServerTools = anthropic.WebTools(c.BaseURL, c.Model)
		}
		return p, nil
	case Chat:
		p := openai.NewChat(c.ID, c.BaseURL, c.APIKey, c.Model)
		p.ReplayReasoning = c.ReplayReasoning
		p.NoImages = c.NoImages || !catalog.Images(c.Model)
		return p, nil
	case Responses:
		p := openai.NewResponses(c.ID, c.BaseURL, c.APIKey, c.Model)
		p.EncryptedReasoning = c.EncryptedReasoning
		p.NoImages = c.NoImages || !catalog.Images(c.Model)
		p.WebSearch = c.WebSearch
		return p, nil
	}
	return nil, fmt.Errorf("unknown API %q (want %s, %s or %s)", c.API, Anthropic, Chat, Responses)
}

// FromEnv reads GOBOT_API, GOBOT_PROVIDER, GOBOT_BASE_URL, GOBOT_API_KEY and
// GOBOT_MODEL (model when that is unset), with GOBOT_REPLAY_REASONING=1,
// GOBOT_ENCRYPTED_REASONING=1 and GOBOT_WEB_SEARCH=1 as switches. For the Anthropic API the key and
// base URL fall back to ANTHROPIC_API_KEY and ANTHROPIC_BASE_URL.
func FromEnv(model string) Config {
	c := Config{
		API:                os.Getenv("GOBOT_API"),
		ID:                 os.Getenv("GOBOT_PROVIDER"),
		BaseURL:            os.Getenv("GOBOT_BASE_URL"),
		APIKey:             os.Getenv("GOBOT_API_KEY"),
		Model:              os.Getenv("GOBOT_MODEL"),
		ReplayReasoning:    os.Getenv("GOBOT_REPLAY_REASONING") == "1",
		EncryptedReasoning: os.Getenv("GOBOT_ENCRYPTED_REASONING") == "1",
		WebSearch:          os.Getenv("GOBOT_WEB_SEARCH") == "1",
	}
	if c.Model == "" {
		c.Model = model
	}
	if c.API == "" || c.API == Anthropic {
		if c.APIKey == "" {
			c.APIKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		if c.BaseURL == "" {
			c.BaseURL = os.Getenv("ANTHROPIC_BASE_URL")
		}
	}
	return c
}
