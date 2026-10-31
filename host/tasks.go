package host

import (
	"context"
	"time"

	"github.com/zucchiniEvader/baggy/mcp"
	"github.com/zucchiniEvader/baggy/memory"
)

// Tasks are what runs in the background of a chat: commands, Claude Code,
// delegations, as they stand.
type Tasks struct {
	Shells      []Shell      `json:"shells"`
	Claude      []ClaudeRun  `json:"claude"`
	Delegations []Delegation `json:"delegations"`
}

// Shell is a running command.
type Shell struct {
	ID, Chat, Bot, Command, Description string
	Started                             time.Time
	Background                          bool
	Last                                string // its last 10 lines
}

// ClaudeRun is a Claude Code task at work.
type ClaudeRun struct {
	ID, Bot, Task string
	Took          time.Duration
	Progress      string // its last 8 steps
	Text          string // what it said last
}

// Delegation is a delegation under way.
type Delegation struct {
	ID, Bot string
	Started time.Time
	Tasks   []DelegatedTask
}

// DelegatedTask is one worker of a delegation.
type DelegatedTask struct {
	Title, Bot string
	State      string   // "working", "done", "failed", "stopped"
	Steps      []string // its last 4 tool calls
}

// Tasks lists a chat's tasks ("" for every chat's).
func (h *Host) Tasks(chatID string) Tasks {
	out := Tasks{Shells: []Shell{}, Claude: []ClaudeRun{}, Delegations: []Delegation{}}
	tm := h.Team
	if tm.Terminals != nil {
		for _, s := range tm.Terminals.Running(chatID) {
			out.Shells = append(out.Shells, Shell{ID: s.ID, Chat: s.Chat, Bot: s.Bot, Command: s.Command, Description: s.Description,
				Started: s.Started, Background: s.Background(), Last: s.Last(10)})
		}
	}
	if tm.ClaudeCode != nil {
		for _, r := range tm.ClaudeCode.Running(chatID) {
			out.Claude = append(out.Claude, ClaudeRun{ID: r.ID, Bot: r.Bot, Task: r.Task, Took: r.Took(), Progress: r.Progress(8), Text: r.Text()})
		}
	}
	for _, d := range tm.Delegations(chatID) {
		dd := Delegation{ID: d.ID, Bot: d.Bot, Started: d.Started}
		for _, k := range d.Tasks {
			state, _ := k.Status()
			dd.Tasks = append(dd.Tasks, DelegatedTask{Title: k.Title, Bot: k.Bot, State: state, Steps: k.Steps(4)})
		}
		out.Delegations = append(out.Delegations, dd)
	}
	return out
}

// StopShell stops a command.
func (h *Host) StopShell(id, reason string) {
	if h.Team.Terminals != nil {
		if s, ok := h.Team.Terminals.Get(id); ok {
			s.Stop(reason)
		}
	}
}

// BackgroundShell lets a command run on while the bot goes on.
func (h *Host) BackgroundShell(id string) {
	if h.Team.Terminals != nil {
		if s, ok := h.Team.Terminals.Get(id); ok {
			s.SendToBackground()
		}
	}
}

// StopClaude stops a Claude Code task.
func (h *Host) StopClaude(id, reason string) {
	if h.Team.ClaudeCode != nil {
		if r, ok := h.Team.ClaudeCode.Get(id); ok {
			r.Stop(reason)
		}
	}
}

// StopDelegation ends a delegation's tasks under way.
func (h *Host) StopDelegation(id string) {
	for _, d := range h.Team.Delegations("") {
		if d.ID == id {
			d.Stop()
		}
	}
}

// Plugins are the servers of mcp.json as they stood when last reached.
func (h *Host) Plugins() []mcp.Status {
	if h.Team.Plugins == nil {
		return nil
	}
	return h.Team.Plugins.Known()
}

// CheckPlugins connects every server and tells how each stands.
func (h *Host) CheckPlugins() []mcp.Status {
	if h.Team.Plugins == nil {
		return nil
	}
	return h.Team.Plugins.Statuses(context.Background())
}

// PluginsError is why mcp.json could not be read, "" when it could.
func (h *Host) PluginsError() string {
	if h.Team.Plugins == nil {
		return ""
	}
	if err := h.Team.Plugins.FileError(); err != nil {
		return err.Error()
	}
	return ""
}

// RestartPlugins reads mcp.json again and starts fresh.
func (h *Host) RestartPlugins() {
	if h.Team.Plugins != nil {
		h.Team.Plugins.Close()
	}
}

// SaveServer writes a server to mcp.json, replacing the one named old.
func (h *Host) SaveServer(old string, s mcp.Server) error { return mcp.SaveServer(h.MCPPath, old, s) }

// DeleteServer takes a server out of mcp.json.
func (h *Host) DeleteServer(name string) error { return mcp.DeleteServer(h.MCPPath, name) }

// ServerCheck is how reaching a server went.
type ServerCheck struct {
	OK        bool
	Error     string
	Tools     int
	Resources bool
}

// CheckServer starts or reaches a server and lists its tools, a minute at
// most.
func (h *Host) CheckServer(s mcp.Server) ServerCheck {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cl, err := mcp.Connect(ctx, s)
	if err != nil {
		return ServerCheck{Error: err.Error()}
	}
	defer cl.Close()
	tools, err := cl.Tools(ctx)
	if err != nil {
		return ServerCheck{Error: err.Error()}
	}
	return ServerCheck{OK: true, Tools: len(tools), Resources: cl.HasResources}
}

// Memory is a bot's MEMORY.md and how much of it opens every turn.
type Memory struct {
	Text              string
	Lines, Bytes, Cut int
}

// Memory reads a bot's MEMORY.md.
func (h *Host) Memory(botID string) Memory {
	m := memory.New(h.Store.Workspace(botID))
	out := Memory{Text: m.Read()}
	out.Lines, out.Bytes, out.Cut = m.Loaded()
	return out
}

// SaveMemory writes a bot's MEMORY.md, unless it changed from was
// (memory.ErrChanged).
func (h *Host) SaveMemory(botID, was, text string) error {
	return memory.New(h.Store.Workspace(botID)).Save(was, text)
}
