// Package host is the runtime a window works with: the store and the team,
// the managers they run with (plugins, terminals, Claude Code), the
// approvals and the preferences the turns read. The desktop runs one in its
// own process; baggyd runs one for desktops on other machines, which reach
// its methods through remote (docs/remote.md). Every method takes and gives
// plain values, so it can cross the network as JSON.
package host

import (
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/claudecode"
	"github.com/zucchiniEvader/baggy/mcp"
	"github.com/zucchiniEvader/baggy/provider/connect"
	"github.com/zucchiniEvader/baggy/shellenv"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/team"
	"github.com/zucchiniEvader/baggy/terminal"
	"github.com/zucchiniEvader/baggy/tool"
)

// Host is one runtime.
type Host struct {
	Store *store.Store
	Team  *team.Team
	// MCPPath is the user's mcp.json, which Plugins reads.
	MCPPath string

	approvals approvals
}

// Options say where Open keeps its state.
type Options struct {
	Data       string // the store's folder
	Workspaces string // the bots' workspaces; "" beside the database
	Fallback   connect.Config
	Emit       func(team.Event)
}

// Open opens the store at o.Data and runs its bots: the shell and file
// tools, codemode over the plugins of mcp.json, terminals and Claude Code.
// Routines start with Team.StartRoutines, once what o.Emit tells is ready
// for events. Close stops them.
func Open(o Options) (*Host, error) {
	st, err := store.Open(o.Data)
	if err != nil {
		return nil, err
	}
	if o.Workspaces != "" {
		if err := st.UseWorkspaces(o.Workspaces); err != nil {
			// the ones not moved stay where they were
			os.Stderr.WriteString("workspaces: " + err.Error() + "\n")
		}
	}
	if _, err := st.Seed(); err != nil {
		st.Close()
		return nil, err
	}
	tm := NewTeam(st, o.Fallback, o.Emit)
	tm.Plugins = mcp.NewManager(filepath.Join(o.Data, "mcp.json"))
	tm.Codemode = true
	tm.Checks = tm.RunCheck
	mcp.Env = shellenv.Env
	tm.Terminals = terminal.NewManager()
	tm.Terminals.Env = shellenv.Env
	tm.WakeOnExit()
	tm.ClaudeCode = claudecode.NewManager()
	tm.ClaudeCode.Env = shellenv.Env
	h := New(st, tm)
	h.MCPPath = filepath.Join(o.Data, "mcp.json")
	return h, nil
}

// NewTeam runs the bots with the shell and file tools in their workspaces.
func NewTeam(st *store.Store, fallback connect.Config, emit func(team.Event)) *team.Team {
	return &team.Team{
		Store:    st,
		Model:    team.Models(st, fallback),
		Fallback: fallback,
		Tools: func(b store.Bot, ws string) []tool.Tool {
			return []tool.Tool{tool.Shell{Dir: ws}, tool.ReadFile{Dir: ws}, tool.WriteFile{Dir: ws}, tool.EditFile{Dir: ws}, tool.Find{Dir: ws}, tool.Grep{Dir: ws}}
		},
		Emit:       emit,
		Routing:    true,
		Retry:      agent.DefaultRetry,
		Memory:     os.Getenv("GOBOT_MEMORY") != "0",
		Compaction: &team.Compaction{NoFlush: os.Getenv("GOBOT_MEMORY_FLUSH") == "0"},
	}
}

// New is a host over a store and a team made elsewhere (tests): it takes
// the team's approvals and its compaction preferences.
func New(st *store.Store, tm *team.Team) *Host {
	h := &Host{Store: st, Team: tm}
	h.approvals.waiting = map[string]*waiting{}
	tm.Approve = h.approve
	if tm.Compaction != nil {
		tm.Compaction.Policy = h.Policy
	}
	return h
}

// Close stops what runs and closes the store.
func (h *Host) Close() {
	tm := h.Team
	tm.StopDelegations()
	if tm.ClaudeCode != nil {
		tm.ClaudeCode.StopAll()
	}
	if tm.Terminals != nil {
		tm.Terminals.StopAll()
	}
	if tm.Plugins != nil {
		tm.Plugins.Close()
	}
	h.Store.Close()
}

func (h *Host) emit(e team.Event) {
	if h.Team.Emit != nil {
		h.Team.Emit(e)
	}
}

// Snapshot is everything a chat list needs.
type Snapshot struct {
	Bots     []store.Bot              `json:"bots"`
	Chats    []store.Chat             `json:"chats"`
	Previews map[string]store.Preview `json:"previews"`
	Unread   map[string]int           `json:"unread"`
	Busy     []string                 `json:"busy"`
}

// Snapshot reads the bots, the chats, their last words and counts.
func (h *Host) Snapshot() (Snapshot, error) {
	st := h.Store
	bots, err := st.Bots()
	if err != nil {
		return Snapshot{}, err
	}
	chats, err := st.Chats()
	if err != nil {
		return Snapshot{}, err
	}
	pv, err := st.Previews()
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Bots: bots, Chats: chats, Previews: pv, Unread: map[string]int{}, Busy: []string{}}
	for _, c := range chats {
		if n := st.Unread(c.ID); n > 0 {
			snap.Unread[c.ID] = n
		}
		if h.Team.Busy(c.ID) {
			snap.Busy = append(snap.Busy, c.ID)
		}
	}
	return snap, nil
}

// SendFiles sends the user's message with files to a chat.
func (h *Host) SendFiles(chatID, text string, files []team.Attachment, mentions []string, replyTo *store.Quote) error {
	return h.Team.SendFiles(chatID, text, files, mentions, replyTo)
}

// Stop cancels a chat's work and denies what it has waiting.
func (h *Host) Stop(chatID string) {
	h.Team.Stop(chatID)
	h.DenyAll(chatID)
}

// Interrupt denies what a chat has waiting and cuts its step short.
func (h *Host) Interrupt(chatID string) bool {
	h.DenyAll(chatID)
	return h.Team.Interrupt(chatID)
}

func (h *Host) Continue(chatID string) error     { return h.Team.Continue(chatID) }
func (h *Host) Busy(chatID string) bool          { return h.Team.Busy(chatID) }
func (h *Host) RunRoutine(r store.Routine) error { return h.Team.RunRoutine(r) }
func (h *Host) Config(b store.Bot) (connect.Config, error) {
	return team.Config(h.Store, b, h.Team.Fallback)
}

// HasFallback reports whether bots run on the environment's provider when
// none is stored.
func (h *Host) HasFallback() bool { return h.Team.Fallback.APIKey != "" }

// ClaudeCodeMissing is why the claude command cannot run, "" when it can.
// It reads the login shell's PATH, which takes a moment the first time.
func (h *Host) ClaudeCodeMissing() string {
	if h.Team.ClaudeCode == nil {
		return "Claude Code is not set up."
	}
	if _, err := h.Team.ClaudeCode.Available(); err != nil {
		return "Claude Code is not installed here: the claude command is not on the PATH."
	}
	return ""
}

// Policy is team.Compaction's Policy over the preferences: the share of the
// window a chat may fill while the cache is warm and once it has expired,
// and how long a provider keeps the cache.
func (h *Host) Policy() (warm, cold float64, ttl time.Duration) {
	warm, cold, ttl = 0.8, 0.4, 5*time.Minute
	if v, err := strconv.ParseFloat(h.Store.Setting("pref.compact_warm"), 64); err == nil && v > 0 {
		warm = v
	}
	if v, err := strconv.ParseFloat(h.Store.Setting("pref.compact_cold"), 64); err == nil && v >= 0 {
		cold = v
	}
	if v, err := time.ParseDuration(h.Store.Setting("pref.cache_ttl")); err == nil && v > 0 {
		ttl = v
	}
	return warm, cold, ttl
}
