// Package provider turns a Request into a stream of Events for one assistant
// message. One adapter per wire protocol (Messages, Chat Completions, ...),
// not per vendor.
package provider

import (
	"context"
	"encoding/json"

	"github.com/zucchiniEvader/baggy/agent"
)

// Provider never fails the call itself: errors arrive as an EventDone whose
// Message has StopError or StopAborted.
type Provider interface {
	ID() string
	Model() string
	Stream(ctx context.Context, req Request) <-chan Event
}

// ToolSpec is what the model is told about a tool.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}

type Request struct {
	System      string
	Messages    []agent.Message
	Tools       []ToolSpec
	CachePoints []int // prefix lengths of Messages that later calls send again
	MaxTokens   int
	Thinking    ThinkingLevel
}

type ThinkingLevel string

const (
	ThinkingDefault ThinkingLevel = ""
	ThinkingOff     ThinkingLevel = "off"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
)

type EventKind string

const (
	EventStart         EventKind = "start"
	EventTextDelta     EventKind = "text_delta"
	EventThinkingDelta EventKind = "thinking_delta"
	EventToolCall      EventKind = "tool_call"   // arguments complete
	EventServerTool    EventKind = "server_tool" // Part: a server tool's call or result, complete
	EventDone          EventKind = "done"        // Message is final
)

type Event struct {
	Kind    EventKind
	Delta   string         // text_delta, thinking_delta
	Part    *agent.Part    // tool_call, server_tool
	Message *agent.Message // done
}
