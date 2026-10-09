// Command smoke runs a multi-tool task against a real Anthropic-protocol API,
// in a scratch directory:
//
//	ANTHROPIC_API_KEY=... go run ./cmd/smoke [model]
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/provider"
	"github.com/spike/gobot/provider/anthropic"
	"github.com/spike/gobot/tool"
)

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
	dir, err := os.MkdirTemp("", "gobot-smoke-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	m := provider.Model{Provider: anthropic.New("anthropic", os.Getenv("ANTHROPIC_BASE_URL"), key, model)}
	user := agent.Message{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: "Write notes/a.txt containing the numbers 1 to 3, one per line, and notes/b.txt containing the word done. Then, in one step, use the shell to count the lines of both files, and tell me the counts."}}}
	out := agent.Run(context.Background(), m, []agent.Message{user}, agent.Options{
		Tools: []tool.Tool{tool.Shell{Dir: dir}, tool.ReadFile{Dir: dir}, tool.WriteFile{Dir: dir}},
		Emit: func(e agent.Event) {
			switch e.Kind {
			case "text_delta", "thinking_delta":
				fmt.Print(e.Delta)
			case "tool_start", "tool_end":
				fmt.Printf("\n[%s]\n", e.Kind)
			case "message_end":
				for _, p := range e.Message.Parts {
					if p.Kind == agent.PartToolCall {
						fmt.Printf("\n[call %s %s]", p.Name, p.Args)
					}
				}
			}
		},
	})
	last := out[len(out)-1]
	fmt.Printf("\nstop=%s usage=%+v messages=%d\n", last.StopReason, last.Usage, len(out))
	if last.StopReason != agent.StopEnd {
		os.Exit(1)
	}
}
