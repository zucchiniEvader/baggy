// Package openai holds the adapters for OpenAI's two wire protocols: streaming
// Chat Completions (POST {base}/chat/completions), which most servers speak
// (OpenAI, DeepSeek, OpenRouter, Ollama, vLLM, ...), and Responses
// (POST {base}/responses).
package openai

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/zucchiniEvader/baggy/agent"
)

const defaultBase = "https://api.openai.com/v1"

// NoToolOutput stands in for a tool result with no text.
const NoToolOutput = "(no tool output)"

func base(b string) string {
	if b == "" {
		b = defaultBase
	}
	return strings.TrimRight(b, "/")
}

func bearer(key string) map[string]string {
	return map[string]string{"authorization": "Bearer " + key}
}

func dataURL(p agent.Part) string {
	return "data:" + p.MediaType + ";base64," + base64.StdEncoding.EncodeToString(p.Data)
}

// argsString is a call's arguments as the string both APIs send back: the
// JSON text, or the original text when the model's arguments were not JSON
// (wire.Args kept those as a JSON string).
func argsString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var s string
	if raw[0] == '"' && json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

func resultText(p agent.Part) string {
	if strings.TrimSpace(p.Text) == "" {
		return NoToolOutput
	}
	return p.Text
}

// assistantText joins the text parts of an assistant message.
func assistantText(m agent.Message) string {
	var b strings.Builder
	for _, p := range m.Parts {
		if p.Kind == agent.PartText {
			b.WriteString(p.Text)
		}
	}
	if strings.TrimSpace(b.String()) == "" {
		return ""
	}
	return b.String()
}
