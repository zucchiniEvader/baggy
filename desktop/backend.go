package main

import (
	"github.com/zucchiniEvader/baggy/host"
	"github.com/zucchiniEvader/baggy/mcp"
	"github.com/zucchiniEvader/baggy/provider/connect"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/team"
)

// Store is the store as the window uses it: *store.Store when the bots run
// in this process, remoteStore when they run on another machine.
type Store interface {
	Dir() string
	Workspace(botID string) string
	Workspaces() string
	WorkDir(b store.Bot) string
	Setting(key string) string
	SetSetting(key, value string) error
	SaveProvider(p store.Provider) (store.Provider, error)
	Providers() ([]store.Provider, error)
	Provider(id string) (store.Provider, error)
	DeleteProvider(id string) error
	CreateBot(b store.Bot) (store.Bot, error)
	UpdateBot(b store.Bot) error
	SaveAvatar(png []byte) (string, error)
	AvatarPath(name string) string
	Bot(id string) (store.Bot, error)
	Bots() ([]store.Bot, error)
	DeleteBot(id string) error
	SaveRoutine(r store.Routine) (store.Routine, error)
	Routine(id string) (store.Routine, error)
	Routines(botID string) ([]store.Routine, error)
	DeleteRoutine(id string) error
	Chats() ([]store.Chat, error)
	DMOf(botID string) (store.Chat, error)
	CreateGroup(title, description string, members []string) (store.Chat, error)
	AddMember(chatID, botID string) error
	RemoveMember(chatID, botID string) error
	SetOwner(chatID, botID string) error
	SetGroupInfo(chatID, title, description string) error
	DeleteChat(chatID string) error
	Messages(chatID string, after int64) ([]store.Entry, error)
	Entry(chatID string, seq int64) (store.Entry, error)
	MarkRead(chatID string) error
	SetPinned(chatID string, pinned bool) error
	Previews() (map[string]store.Preview, error)
	FilePath(f store.File) string
}

// Runtime is the host as the window uses it: *host.Host in this process,
// remoteHost on another machine.
type Runtime interface {
	Snapshot() (host.Snapshot, error)
	SendFiles(chatID, text string, files []team.Attachment, mentions []string, replyTo *store.Quote) error
	Stop(chatID string)
	Interrupt(chatID string) bool
	Continue(chatID string) error
	RunRoutine(r store.Routine) error
	Config(b store.Bot) (connect.Config, error)
	HasFallback() bool
	ClaudeCodeMissing() string
	Rules() []string
	Decide(id string, ok, always bool)
	Waiting(chatID string) []host.Waiting
	Tasks(chatID string) host.Tasks
	StopShell(id, reason string)
	BackgroundShell(id string)
	StopClaude(id, reason string)
	StopDelegation(id string)
	Plugins() []mcp.Status
	CheckPlugins() []mcp.Status
	PluginsError() string
	RestartPlugins()
	SaveServer(old string, s mcp.Server) error
	DeleteServer(name string) error
	CheckServer(s mcp.Server) host.ServerCheck
	Memory(botID string) host.Memory
	SaveMemory(botID, was, text string) error
}

var (
	_ Store   = (*store.Store)(nil)
	_ Runtime = (*host.Host)(nil)
)
