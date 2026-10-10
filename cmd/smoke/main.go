// Command smoke runs a multi-tool task against a real API, in a scratch
// directory. The provider comes from the environment (connect.FromEnv):
//
//	ANTHROPIC_API_KEY=... go run ./cmd/smoke [model]
//	GOBOT_API=chat GOBOT_BASE_URL=https://api.deepseek.com GOBOT_API_KEY=... go run ./cmd/smoke deepseek-chat
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/provider"
	"github.com/zucchiniEvader/baggy/provider/connect"
	"github.com/zucchiniEvader/baggy/tool"
)

func main() {
	cfg := connect.FromEnv("claude-sonnet-5-5")
	if len(os.Args) > 1 {
		cfg.Model = os.Args[1]
	}
	if cfg.APIKey == "" {
		fmt.Fprintln(os.Stderr, "set GOBOT_API_KEY (or ANTHROPIC_API_KEY)")
		os.Exit(2)
	}
	p, err := connect.New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	dir, err := os.MkdirTemp("", "gobot-smoke-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	m := provider.Model{Provider: p, Thinking: provider.ThinkingLevel(os.Getenv("GOBOT_THINKING"))}
	task := "Write notes/a.txt containing the numbers 1 to 3, one per line, and notes/b.txt containing the word done. Then, in one step, use the shell to count the lines of both files, and tell me the counts."
	if os.Getenv("GOBOT_TASK") == "edit" { // find, grep and edit_file over a small tree
		os.MkdirAll(dir+"/docs/old", 0o755)
		os.WriteFile(dir+"/docs/plan.md", []byte("# Plan\n\n- TODO: pick a venue\n- book flights\n"), 0o644)
		os.WriteFile(dir+"/docs/old/notes.md", []byte("nothing here\n"), 0o644)
		os.WriteFile(dir+"/main.go", []byte("package main\n// TODO: remove\n"), 0o644)
		task = "Without the shell: find the markdown files, grep them for TODO, and in the markdown file that has one, replace the TODO line with '- DONE: picked the Blue Note'. Tell me which file you changed."
	}
	user := agent.Message{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: task}}}
	out := agent.Run(context.Background(), m, []agent.Message{user}, agent.Options{
		Tools: []tool.Tool{tool.Shell{Dir: dir}, tool.ReadFile{Dir: dir}, tool.WriteFile{Dir: dir}, tool.EditFile{Dir: dir}, tool.Find{Dir: dir}, tool.Grep{Dir: dir}},
		Retry: agent.DefaultRetry,
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
	if os.Getenv("GOBOT_TASK") == "edit" {
		b, _ := os.ReadFile(dir + "/docs/plan.md")
		fmt.Printf("\n--- docs/plan.md now:\n%s", b)
	}
	last := out[len(out)-1]
	thinking, signed, replayed := 0, 0, 0
	for i, m := range out {
		for _, p := range m.Parts {
			if p.Kind == agent.PartThinking {
				thinking++
				if p.Signature != "" {
					signed++
					if i < len(out)-1 { // a later call of the run sent it back
						replayed++
					}
				}
			}
		}
	}
	fmt.Printf("\nstop=%s usage=%+v messages=%d thinking=%d signed=%d replayed=%d\n", last.StopReason, last.Usage, len(out), thinking, signed, replayed)
	if last.StopReason != agent.StopEnd {
		os.Exit(1)
	}
}
