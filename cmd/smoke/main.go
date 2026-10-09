// Command smoke runs one tool round trip against the real Anthropic API:
//
//	ANTHROPIC_API_KEY=... go run ./cmd/smoke [model]
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/provider"
	"github.com/spike/gobot/provider/anthropic"
	"github.com/spike/gobot/tool"
)

type clock struct{}

func (clock) Name() string        { return "now" }
func (clock) Description() string { return "Returns the current time in RFC 3339." }
func (clock) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (clock) Execute(context.Context, json.RawMessage) tool.Result {
	return tool.Result{Text: time.Now().Format(time.RFC3339)}
}

func main() {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "set ANTHROPIC_API_KEY")
		os.Exit(2)
	}
	model := "claude-sonnet-5-5"
	if len(os.Args) > 1 {
		model = os.Args[1]
	}
	m := provider.Model{Provider: anthropic.New("anthropic", os.Getenv("ANTHROPIC_BASE_URL"), key, model)}
	user := agent.Message{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: "What time is it? Use the tool."}}}
	out := agent.Run(context.Background(), m, []agent.Message{user}, agent.Options{
		Tools: []tool.Tool{clock{}},
		Emit: func(e agent.Event) {
			switch e.Kind {
			case "text_delta", "thinking_delta":
				fmt.Print(e.Delta)
			case "tool_start", "tool_end":
				fmt.Printf("\n[%s]\n", e.Kind)
			}
		},
	})
	last := out[len(out)-1]
	fmt.Printf("\nstop=%s usage=%+v messages=%d\n", last.StopReason, last.Usage, len(out))
	if last.StopReason != agent.StopEnd {
		os.Exit(1)
	}
}
