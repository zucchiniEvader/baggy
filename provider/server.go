package provider

import (
	"encoding/json"
	"strings"

	"github.com/spike/gobot/agent"
)

// Server tool names, as the UI words them.
const (
	WebSearch = "web_search"
	WebFetch  = "web_fetch"
)

// ServerBlock describes a server-side tool's block in a reply (PartServer):
// Anthropic's server_tool_use and its *_tool_result, or a Responses
// web_search_call.
type ServerBlock struct {
	Result bool   // a result, not a call
	ID     string // the call's id; a result names the call it answers
	Tool   string // WebSearch, WebFetch, or the server's own name
	Detail string // the query or URL
	Error  string // a result's error code, with spaces
	Count  int    // a search result's number of hits
}

// DescribeServer reads a server part; ok is false for any other block (such
// as redacted_thinking).
func DescribeServer(p agent.Part) (ServerBlock, bool) {
	if p.Kind != agent.PartServer {
		return ServerBlock{}, false
	}
	var v struct {
		Type      string          `json:"type"`
		ID        string          `json:"id"`
		ToolUseID string          `json:"tool_use_id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		Content   json.RawMessage `json:"content"`
		Action    struct {
			Type  string `json:"type"`
			Query string `json:"query"`
			URL   string `json:"url"`
		} `json:"action"`
		Status string `json:"status"`
	}
	if json.Unmarshal(p.Raw, &v) != nil {
		return ServerBlock{}, false
	}
	switch {
	case v.Type == "server_tool_use":
		var in struct{ Query, URL string }
		json.Unmarshal(v.Input, &in)
		b := ServerBlock{ID: v.ID, Tool: v.Name, Detail: strings.TrimSpace(in.Query)}
		if v.Name == WebFetch {
			b.Detail = strings.TrimSpace(in.URL)
		}
		return b, true
	case strings.HasSuffix(v.Type, "_tool_result"):
		b := ServerBlock{Result: true, ID: v.ToolUseID, Tool: strings.TrimSuffix(v.Type, "_tool_result")}
		var one struct {
			ErrorCode string `json:"error_code"`
		}
		var many []struct {
			Type      string `json:"type"`
			ErrorCode string `json:"error_code"`
		}
		if json.Unmarshal(v.Content, &one) == nil && one.ErrorCode != "" {
			b.Error = one.ErrorCode
		} else if json.Unmarshal(v.Content, &many) == nil {
			for _, it := range many {
				if it.ErrorCode != "" {
					b.Error = it.ErrorCode
				} else if strings.HasSuffix(it.Type, "_result") {
					b.Count++
				}
			}
		}
		b.Error = strings.ReplaceAll(b.Error, "_", " ")
		return b, true
	case v.Type == "web_search_call": // Responses: one item is the call and its outcome
		b := ServerBlock{ID: v.ID, Tool: WebSearch, Detail: strings.TrimSpace(v.Action.Query)}
		if v.Action.Type == "open_page" || v.Action.Type == "find" {
			b.Tool, b.Detail = WebFetch, strings.TrimSpace(v.Action.URL)
		}
		if v.Status == "failed" {
			b.Result, b.Error = true, "failed"
		}
		return b, true
	}
	return ServerBlock{}, false
}
