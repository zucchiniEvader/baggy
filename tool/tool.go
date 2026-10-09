// Package tool defines what the agent loop can call.
package tool

import (
	"context"
	"encoding/json"
)

type Result struct {
	Text    string
	IsError bool
}

type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage // JSON Schema of the arguments
	Execute(ctx context.Context, args json.RawMessage) Result
}
