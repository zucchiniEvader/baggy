// Package agent is the loop that runs one assistant turn: model calls, tool
// calls, steering, and cancellation. It knows no vendor wire format.
package agent

import "encoding/json"

// Role of a transcript message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Part is one block of a message. Exactly one field group is set per Kind.
type Part struct {
	Kind PartKind `json:"kind"`

	Text string `json:"text,omitempty"` // text, thinking

	// thinking: the vendor's signature, replayed verbatim.
	Signature string `json:"signature,omitempty"`

	// image
	MediaType string `json:"media_type,omitempty"`
	Data      []byte `json:"data,omitempty"`

	// tool_call / tool_result
	CallID  string          `json:"call_id,omitempty"`
	Name    string          `json:"name,omitempty"`
	Args    json.RawMessage `json:"args,omitempty"`
	IsError bool            `json:"is_error,omitempty"`

	// server: a vendor block (web search, ...) kept verbatim for replay.
	Raw json.RawMessage `json:"raw,omitempty"`
}

type PartKind string

const (
	PartText       PartKind = "text"
	PartThinking   PartKind = "thinking"
	PartImage      PartKind = "image"
	PartToolCall   PartKind = "tool_call"
	PartToolResult PartKind = "tool_result"
	PartServer     PartKind = "server"
)

// Message is one entry of the transcript.
type Message struct {
	ID    string `json:"id"`
	Role  Role   `json:"role"`
	Parts []Part `json:"parts"`

	// Set on assistant messages.
	Provider   string     `json:"provider,omitempty"`
	Model      string     `json:"model,omitempty"`
	StopReason StopReason `json:"stop_reason,omitempty"`
	Usage      Usage      `json:"usage,omitempty"`

	// Kind marks custom messages, e.g. "compaction" for a summary.
	Kind string `json:"kind,omitempty"`
}

type StopReason string

const (
	StopEnd     StopReason = "stop"
	StopLength  StopReason = "length"
	StopToolUse StopReason = "tool_use"
	StopPause   StopReason = "pause" // the server paused a long turn (server tools); send it back to go on
	StopError   StopReason = "error"
	StopAborted StopReason = "aborted"
)

// Usage is token accounting for one model call.
type Usage struct {
	Input      int     `json:"input"` // read fresh, cache reads excluded
	Output     int     `json:"output"`
	CacheRead  int     `json:"cache_read"`
	CacheWrite int     `json:"cache_write"`
	Reasoning  int     `json:"reasoning"`
	Cost       float64 `json:"cost"` // dollars; 0 when the rate is unknown
}

// Context is the number of tokens the call's request carried plus its reply.
func (u Usage) Context() int { return u.Input + u.Output + u.CacheRead + u.CacheWrite }
