package team

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/codemode"
	"github.com/spike/gobot/mcp"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/tool"
)

// CheckTimeout bounds a routine's check.
const CheckTimeout = 5 * time.Minute

// scriptable are the bot's own tools a script may call.
var scriptable = map[string]bool{"read_file": true, "write_file": true, "edit_file": true, "find": true, "grep": true, "ls": true,
	"shell": true, "memory_update": true, "memory_log": true, "recall": true,
	"list_mcp_resources": true, "list_mcp_resource_templates": true, "read_mcp_resource": true}

// readOnly are the bot's tools a routine's check may call.
var readOnly = map[string]bool{"read_file": true, "find": true, "grep": true, "ls": true, "recall": true,
	"list_mcp_resources": true, "list_mcp_resource_templates": true, "read_mcp_resource": true}

// scriptHost is what a script reaches: some of the bot's tools, through the
// turn's hooks, and the plugins' tools.
type scriptHost struct {
	t        *Team
	own      map[string]tool.Tool
	hooks    agent.Hooks
	plugins  []mcp.Status
	readOnly bool // a check: only tools that only read
	progress *scriptProgress
}

// scriptProgress is what a running script shows: what it has printed, then
// the newest word of each plugin call still under way.
type scriptProgress struct {
	ctx     context.Context
	mu      sync.Mutex
	printed string
	calls   map[string]string // a call's tool name → its progress
	order   []string
}

func (p *scriptProgress) print(s string) {
	p.mu.Lock()
	p.printed = s
	p.mu.Unlock()
	p.send()
}

func (p *scriptProgress) call(id, line string) {
	p.mu.Lock()
	if p.calls == nil {
		p.calls = map[string]string{}
	}
	if _, ok := p.calls[id]; !ok {
		p.order = append(p.order, id)
	}
	p.calls[id] = line
	p.mu.Unlock()
	p.send()
}

func (p *scriptProgress) done(id string) {
	p.mu.Lock()
	delete(p.calls, id)
	p.order = slices.DeleteFunc(p.order, func(x string) bool { return x == id })
	p.mu.Unlock()
}

func (p *scriptProgress) send() {
	p.mu.Lock()
	text := p.printed
	for _, id := range p.order {
		text += p.calls[id] + "\n"
	}
	p.mu.Unlock()
	tool.Update(p.ctx, tool.Result{Text: text})
}

func (h *scriptHost) Tools() []codemode.ToolInfo {
	var out []codemode.ToolInfo
	for name, tl := range h.own {
		out = append(out, codemode.ToolInfo{Name: name, Description: tl.Description(), Schema: tl.Schema(), ReadOnly: readOnly[name]})
	}
	for _, p := range h.plugins {
		for _, tl := range p.Tools {
			if h.readOnly && !tl.Annotations.ReadOnlyHint {
				continue
			}
			out = append(out, codemode.ToolInfo{Name: p.ID + "__" + tl.Name, Description: tl.Description, Schema: tl.InputSchema, ReadOnly: tl.Annotations.ReadOnlyHint})
		}
	}
	return out
}

func (h *scriptHost) Call(ctx context.Context, name string, args json.RawMessage) (any, error) {
	call := agent.Part{Kind: agent.PartToolCall, CallID: store.NewID("script"), Name: name, Args: args}
	if h.hooks.BeforeToolCall != nil {
		if r := h.hooks.BeforeToolCall(call); r != nil {
			return nil, fmt.Errorf("%s was not run: %s", name, r.Text)
		}
	}
	if tl, ok := h.own[name]; ok {
		v, err := tool.ValidateArgs(name, tl.Schema(), args)
		if err != nil {
			return nil, err
		}
		r := tl.Execute(ctx, v)
		if r.IsError {
			return nil, fmt.Errorf("%s", r.Text)
		}
		return r.Text, nil
	}
	if h.t.Plugins == nil {
		return nil, fmt.Errorf("no tool %q", name)
	}
	if h.readOnly {
		if tl, ok := h.t.Plugins.Lookup(name); !ok || !tl.Annotations.ReadOnlyHint {
			return nil, fmt.Errorf("%s does not only read, so a check may not call it", name)
		}
	}
	if h.progress != nil {
		id := call.CallID
		ctx = mcp.WithProgress(ctx, func(pr mcp.Progress) { h.progress.call(id, name+": "+pr.String()) })
		defer h.progress.done(id)
	}
	r, err := h.t.Plugins.Call(ctx, name, args)
	if err != nil {
		return nil, err
	}
	return r, nil
}

type scriptStore struct {
	st        *store.Store
	chat, bot string
}

func (s scriptStore) Load(k string) (json.RawMessage, bool) {
	v, ok := s.st.ScriptLoad(s.chat, s.bot, k)
	return json.RawMessage(v), ok
}

func (s scriptStore) Save(k string, v json.RawMessage) error {
	return s.st.ScriptSave(s.chat, s.bot, k, string(v))
}

// plugins are the servers of mcp.json as they stand, connected as needed.
func (t *Team) plugins(ctx context.Context) []mcp.Status {
	if t.Plugins == nil {
		return nil
	}
	return t.Plugins.Statuses(ctx)
}

// codemodeTool runs scripts for a bot's turn.
func (t *Team) codemodeTool(b store.Bot, chat store.Chat, own []tool.Tool, hooks agent.Hooks, plugins []mcp.Status) tool.Tool {
	byName := map[string]tool.Tool{}
	for _, tl := range own {
		if scriptable[tl.Name()] {
			byName[tl.Name()] = tl
		}
	}
	return Func{N: "codemode", D: "Run JavaScript, the body of an async function (top-level await and return work), that calls tools: `await tools.<name>(args)` for your file, shell and memory tools and every plugin tool (`tools.<server>__<tool>`; a plugin tool resolves to {content, structuredContent, isError}, yours to text). Calls run in parallel with Promise.all. Only what the script prints (console.log) or returns reaches you, at most about 10,000 tokens, so filter and summarize inside the script. searchTools(query) and describeTool(name) find tools and their schemas; store(key, value) and load(key) keep JSON for later scripts in this chat. No timers, network, files or modules except through tools. A first line `// @options: {\"timeout_ms\": 60000}` shortens the 30-minute limit.",
		S: `{"type":"object","properties":{"code":{"type":"string"}},"required":["code"],"additionalProperties":false}`,
		F: func(ctx context.Context, raw json.RawMessage) tool.Result {
			var a struct{ Code string }
			json.Unmarshal(raw, &a)
			progress := &scriptProgress{ctx: ctx}
			host := &scriptHost{t: t, own: byName, hooks: hooks, plugins: plugins, progress: progress}
			r := codemode.Run(ctx, a.Code, host, codemode.Options{Store: scriptStore{t.Store, chat.ID, b.ID}, Progress: progress.print})
			return tool.Result{Text: r.Text(), IsError: r.Err != nil}
		}}
}

// PluginsPrompt is the plugins part of a system prompt: each server with its
// tools, to call from codemode.
func PluginsPrompt(ps []mcp.Status) string {
	if len(ps) == 0 {
		return ""
	}
	var s strings.Builder
	s.WriteString("## Plugins\n\nThe user's MCP servers. Call their tools from codemode scripts as tools.<server>__<tool>(args); describeTool gives a tool's schema.\n")
	for _, p := range ps {
		switch p.State {
		case "off":
			continue
		case "failed":
			fmt.Fprintf(&s, "\n### %s (%s): not available: %s\n", p.Server.Name, p.ID, oneLine(p.Err, 200))
			continue
		}
		fmt.Fprintf(&s, "\n### %s (%s)\n", p.Server.Name, p.ID)
		if d := strings.TrimSpace(p.Server.Description); d != "" {
			s.WriteString(d + "\n")
		}
		if p.Resources {
			s.WriteString("It offers resources: list them with list_mcp_resources (server " + p.ID + ") and read one with read_mcp_resource.\n")
		}
		for _, tl := range p.Tools {
			line := "- " + p.ID + "__" + tl.Name
			if d := firstLine(tl.Description); d != "" {
				line += ": " + d
			}
			if tl.Annotations.ReadOnlyHint {
				line += " (reads only)"
			}
			s.WriteString(line + "\n")
		}
	}
	return s.String()
}

// RunCheck runs a routine's check: a script that may only read, five
// minutes at most; what it returns ("" for nothing) starts the run.
func (t *Team) RunCheck(ctx context.Context, r store.Routine) (string, error) {
	b, err := t.Store.Bot(r.BotID)
	if err != nil {
		return "", err
	}
	dm, err := t.Store.DMOf(b.ID)
	if err != nil {
		return "", err
	}
	own := map[string]tool.Tool{}
	if t.Tools != nil {
		for _, tl := range t.Tools(b, t.Store.WorkDir(b)) {
			if readOnly[tl.Name()] {
				own[tl.Name()] = tl
			}
		}
	}
	plugins := t.plugins(ctx)
	for _, tl := range t.resourceTools(plugins) {
		own[tl.Name()] = tl
	}
	host := &scriptHost{t: t, own: own, plugins: plugins, readOnly: true}
	res := codemode.Run(ctx, r.Check, host, codemode.Options{Timeout: CheckTimeout, MaxOutputTokens: 1000, Store: scriptStore{t.Store, dm.ID, b.ID}})
	if res.Err != nil {
		return "", res.Err
	}
	if res.Empty() {
		return "", nil
	}
	v := res.Value
	if len(v) > 4000 {
		v = v[:4000] + "…"
	}
	return v, nil
}
