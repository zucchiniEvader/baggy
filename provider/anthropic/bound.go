package anthropic

import (
	"encoding/json"
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/provider"
)

// boundModels bind each thinking block to the system prompt, the tools and
// the messages before it, and refuse one replayed after any of them changed.
var boundModels = []string{"claude-opus-5-5", "claude-fable-5-1", "claude-sonnet-5-5"}

// Bound reports whether model binds its thinking (a dated id counts).
func Bound(model string) bool {
	if i := strings.LastIndexByte(model, '/'); i >= 0 {
		model = model[i+1:]
	}
	for _, m := range boundModels {
		if model == m || strings.HasPrefix(model, m+"-") {
			return true
		}
	}
	return false
}

// contextKey names the system prompt and tools of a request; the adapter
// keeps it on each thinking part it streams (Part.Raw {"ctx": key}).
func contextKey(req provider.Request) string {
	h := fnv.New64a()
	h.Write([]byte(req.System))
	for _, t := range req.Tools {
		h.Write([]byte{0})
		h.Write([]byte(t.Name + "\x00" + t.Description + "\x00"))
		h.Write(t.Parameters)
	}
	return strconv.FormatUint(h.Sum64(), 36)
}

func thinkingRaw(key string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"ctx": key})
	return b
}

func keyOf(p agent.Part) string {
	var v struct{ Ctx string }
	json.Unmarshal(p.Raw, &v)
	return v.Ctx
}

// DropBoundThinking removes all thinking from a transcript for a bound model
// when any of it was made under another system prompt or other tools than
// key's (or under unknown ones), so the request carries none the API would
// refuse; other models' transcripts, and one whose thinking all matches, are
// returned as they are. A compaction (team) drops the thinking it keeps.
func DropBoundThinking(model, key string, msgs []agent.Message) []agent.Message {
	if !Bound(model) {
		return msgs
	}
	stale := false
	for _, m := range msgs {
		for _, p := range m.Parts {
			if m.Role == agent.RoleAssistant && p.Kind == agent.PartThinking && p.Signature != "" && keyOf(p) != key {
				stale = true
			}
		}
	}
	if !stale {
		return msgs
	}
	out := make([]agent.Message, len(msgs))
	for i, m := range msgs {
		if m.Role == agent.RoleAssistant {
			var parts []agent.Part
			for _, p := range m.Parts {
				if p.Kind != agent.PartThinking {
					parts = append(parts, p)
				}
			}
			m.Parts = parts
		}
		out[i] = m
	}
	return out
}
