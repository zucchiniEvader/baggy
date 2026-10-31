package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zucchiniEvader/baggy/host"
	"github.com/zucchiniEvader/baggy/mcp"
	"github.com/zucchiniEvader/baggy/memory"
	"github.com/zucchiniEvader/baggy/provider/connect"
	"github.com/zucchiniEvader/baggy/remote"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/team"
)

// remoteBackend is the store and host of a runtime on another machine of
// the tailnet (docs/remote.md). A frame reads often, so what a read gives is
// kept until a write or an event other than a streamed one; tasks, which
// change without events, are read again after a second. While the server
// cannot be reached, reads give nothing at once rather than wait on the
// frame, and writes fail.
type remoteBackend struct {
	c     *remote.Client
	files string // where avatars and attachments are kept once downloaded

	mu      sync.Mutex
	memo    map[string]memo
	online  bool
	why     string    // why it is offline
	retried time.Time // when a call last tried, or found it offline
}

type memo struct {
	ret remote.Return
	at  time.Time
}

// volatile are the reads kept only a second.
var volatile = map[string]bool{"Host.Tasks": true}

func newRemoteBackend(c *remote.Client, files string) *remoteBackend {
	if c.HTTP == nil {
		c.HTTP = &http.Client{Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
			ResponseHeaderTimeout: 2 * time.Minute,
		}}
	}
	return &remoteBackend{c: c, files: files, memo: map[string]memo{}, online: true}
}

// forget drops what the reads kept.
func (r *remoteBackend) forget() {
	r.mu.Lock()
	r.memo = map[string]memo{}
	r.mu.Unlock()
}

// setOnline records whether the server answers.
func (r *remoteBackend) setOnline(err error) {
	r.mu.Lock()
	r.online, r.why = err == nil, ""
	if err != nil {
		r.why, r.retried = err.Error(), time.Now()
	}
	r.memo = map[string]memo{}
	r.mu.Unlock()
}

// readTimeout bounds a read, which a frame waits on.
const readTimeout = 10 * time.Second

// retryEvery is how often a call tries while the server is offline; the
// others fail at once, so frames do not wait on it.
const retryEvery = 5 * time.Second

// try reports whether a call should go to the server now.
func (r *remoteBackend) try() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.online {
		return true
	}
	if time.Since(r.retried) < retryEvery {
		return false
	}
	r.retried = time.Now()
	return true
}

func (r *remoteBackend) callRaw(timeout time.Duration, method string, args []any) (remote.Return, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ret, err := r.c.CallRaw(ctx, method, args)
	if errors.Is(err, remote.ErrUnreachable) {
		r.setOnline(err)
	} else if err == nil {
		r.mu.Lock()
		back := !r.online
		r.online, r.why = true, ""
		r.mu.Unlock()
		if back {
			r.forget()
		}
	}
	return ret, err
}

// offline is why the server cannot be reached, "" while it can.
func (r *remoteBackend) offline() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.online {
		return ""
	}
	if r.why == "" {
		return "not connected"
	}
	return r.why
}

func (r *remoteBackend) errOffline() error {
	name := r.c.Name
	if name == "" {
		name = strings.TrimPrefix(r.c.Base, "http://")
	}
	return fmt.Errorf("%s cannot be reached", name)
}

// read calls a method that changes nothing, keeping what it gives.
func (r *remoteBackend) read(method string, args []any, out ...any) error {
	key, _ := json.Marshal(append([]any{method}, args...))
	r.mu.Lock()
	m, ok := r.memo[string(key)]
	r.mu.Unlock()
	if ok && (!volatile[method] || time.Since(m.at) < time.Second) {
		return m.ret.Decode(method, out...)
	}
	if !r.try() {
		return r.errOffline()
	}
	ret, err := r.callRaw(readTimeout, method, args)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.memo[string(key)] = memo{ret: ret, at: time.Now()}
	r.mu.Unlock()
	return ret.Decode(method, out...)
}

// write calls a method that changes something; every read is asked again.
func (r *remoteBackend) write(method string, args []any, out ...any) error {
	if !r.try() {
		return r.errOffline()
	}
	ret, err := r.callRaw(2*time.Minute, method, args)
	r.forget()
	if err != nil {
		return err
	}
	return ret.Decode(method, out...)
}

// follow runs the server's events into apply until ctx ends; connected
// tells the window each time the connection is made or lost.
func (r *remoteBackend) follow(ctx context.Context, apply func(team.Event), connected func(err error)) {
	r.c.Events(ctx, func(e team.Event) {
		if e.Kind != team.EventAgent {
			r.forget()
		}
		apply(e)
	}, func(err error) {
		r.setOnline(err)
		connected(err)
	})
}

// readFile reads a file a path of the store names, downloading an avatar or
// an attachment the first time.
func (r *remoteBackend) readFile(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil {
		return b, nil
	}
	rel, err := filepath.Rel(r.files, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, os.ErrNotExist
	}
	b, err := r.c.Download("/v1/" + filepath.ToSlash(rel))
	if err != nil {
		return nil, err
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, b, 0o644)
	return b, nil
}

// remoteStore is the server's store.
type remoteStore struct{ r *remoteBackend }

func (s remoteStore) str(method string, args ...any) string {
	var v string
	s.r.read("Store."+method, args, &v)
	return v
}

func (s remoteStore) Dir() string                   { return s.str("Dir") }
func (s remoteStore) Workspace(botID string) string { return s.str("Workspace", botID) }
func (s remoteStore) Workspaces() string            { return s.str("Workspaces") }
func (s remoteStore) WorkDir(b store.Bot) string    { return s.str("WorkDir", b) }
func (s remoteStore) Setting(key string) string     { return s.str("Setting", key) }
func (s remoteStore) SetSetting(key, value string) error {
	return s.r.write("Store.SetSetting", []any{key, value})
}
func (s remoteStore) SaveProvider(p store.Provider) (out store.Provider, err error) {
	return out, s.r.write("Store.SaveProvider", []any{p}, &out)
}
func (s remoteStore) Providers() (out []store.Provider, err error) {
	return out, s.r.read("Store.Providers", nil, &out)
}
func (s remoteStore) Provider(id string) (out store.Provider, err error) {
	return out, s.r.read("Store.Provider", []any{id}, &out)
}
func (s remoteStore) DeleteProvider(id string) error {
	return s.r.write("Store.DeleteProvider", []any{id})
}
func (s remoteStore) CreateBot(b store.Bot) (out store.Bot, err error) {
	return out, s.r.write("Store.CreateBot", []any{b}, &out)
}
func (s remoteStore) UpdateBot(b store.Bot) error { return s.r.write("Store.UpdateBot", []any{b}) }
func (s remoteStore) SaveAvatar(png []byte) (out string, err error) {
	return out, s.r.write("Store.SaveAvatar", []any{png}, &out)
}
func (s remoteStore) AvatarPath(name string) string {
	return filepath.Join(s.r.files, "avatars", filepath.Base(name))
}
func (s remoteStore) Bot(id string) (out store.Bot, err error) {
	return out, s.r.read("Store.Bot", []any{id}, &out)
}
func (s remoteStore) Bots() (out []store.Bot, err error) {
	return out, s.r.read("Store.Bots", nil, &out)
}
func (s remoteStore) DeleteBot(id string) error { return s.r.write("Store.DeleteBot", []any{id}) }
func (s remoteStore) SaveRoutine(r store.Routine) (out store.Routine, err error) {
	return out, s.r.write("Store.SaveRoutine", []any{r}, &out)
}
func (s remoteStore) Routine(id string) (out store.Routine, err error) {
	return out, s.r.read("Store.Routine", []any{id}, &out)
}
func (s remoteStore) Routines(botID string) (out []store.Routine, err error) {
	return out, s.r.read("Store.Routines", []any{botID}, &out)
}
func (s remoteStore) DeleteRoutine(id string) error {
	return s.r.write("Store.DeleteRoutine", []any{id})
}
func (s remoteStore) Chats() (out []store.Chat, err error) {
	return out, s.r.read("Store.Chats", nil, &out)
}
func (s remoteStore) DMOf(botID string) (out store.Chat, err error) {
	return out, s.r.read("Store.DMOf", []any{botID}, &out)
}
func (s remoteStore) CreateGroup(title, description string, members []string) (out store.Chat, err error) {
	return out, s.r.write("Store.CreateGroup", []any{title, description, members}, &out)
}
func (s remoteStore) AddMember(chatID, botID string) error {
	return s.r.write("Store.AddMember", []any{chatID, botID})
}
func (s remoteStore) RemoveMember(chatID, botID string) error {
	return s.r.write("Store.RemoveMember", []any{chatID, botID})
}
func (s remoteStore) SetOwner(chatID, botID string) error {
	return s.r.write("Store.SetOwner", []any{chatID, botID})
}
func (s remoteStore) SetGroupInfo(chatID, title, description string) error {
	return s.r.write("Store.SetGroupInfo", []any{chatID, title, description})
}
func (s remoteStore) DeleteChat(chatID string) error {
	return s.r.write("Store.DeleteChat", []any{chatID})
}
func (s remoteStore) Messages(chatID string, after int64) (out []store.Entry, err error) {
	return out, s.r.read("Store.Messages", []any{chatID, after}, &out)
}
func (s remoteStore) Entry(chatID string, seq int64) (out store.Entry, err error) {
	return out, s.r.read("Store.Entry", []any{chatID, seq}, &out)
}
func (s remoteStore) MarkRead(chatID string) error {
	return s.r.write("Store.MarkRead", []any{chatID})
}
func (s remoteStore) SetPinned(chatID string, pinned bool) error {
	return s.r.write("Store.SetPinned", []any{chatID, pinned})
}
func (s remoteStore) Previews() (out map[string]store.Preview, err error) {
	return out, s.r.read("Store.Previews", nil, &out)
}
func (s remoteStore) FilePath(f store.File) string {
	return filepath.Join(s.r.files, "files", filepath.Base(f.ID), filepath.Base(f.Name))
}

// remoteHost is the server's host.
type remoteHost struct{ r *remoteBackend }

func (h remoteHost) Snapshot() (out host.Snapshot, err error) {
	return out, h.r.read("Host.Snapshot", nil, &out)
}
func (h remoteHost) SendFiles(chatID, text string, files []team.Attachment, mentions []string, replyTo *store.Quote) error {
	return h.r.write("Host.SendFiles", []any{chatID, text, files, mentions, replyTo})
}
func (h remoteHost) Stop(chatID string) { h.r.write("Host.Stop", []any{chatID}) }
func (h remoteHost) Interrupt(chatID string) (ok bool) {
	h.r.write("Host.Interrupt", []any{chatID}, &ok)
	return ok
}
func (h remoteHost) Continue(chatID string) error { return h.r.write("Host.Continue", []any{chatID}) }
func (h remoteHost) RunRoutine(r store.Routine) error {
	return h.r.write("Host.RunRoutine", []any{r})
}
func (h remoteHost) Config(b store.Bot) (out connect.Config, err error) {
	return out, h.r.read("Host.Config", []any{b}, &out)
}
func (h remoteHost) HasFallback() (ok bool) {
	h.r.read("Host.HasFallback", nil, &ok)
	return ok
}
func (h remoteHost) ClaudeCodeMissing() (why string) {
	if err := h.r.read("Host.ClaudeCodeMissing", nil, &why); err != nil {
		return err.Error()
	}
	return why
}
func (h remoteHost) Rules() (out []string) {
	h.r.read("Host.Rules", nil, &out)
	return out
}
func (h remoteHost) Decide(id string, ok, always bool) {
	h.r.write("Host.Decide", []any{id, ok, always})
}
func (h remoteHost) Waiting(chatID string) (out []host.Waiting) {
	h.r.read("Host.Waiting", []any{chatID}, &out)
	return out
}
func (h remoteHost) Tasks(chatID string) (out host.Tasks) {
	h.r.read("Host.Tasks", []any{chatID}, &out)
	return out
}
func (h remoteHost) StopShell(id, reason string) { h.r.write("Host.StopShell", []any{id, reason}) }
func (h remoteHost) BackgroundShell(id string)   { h.r.write("Host.BackgroundShell", []any{id}) }
func (h remoteHost) StopClaude(id, reason string) {
	h.r.write("Host.StopClaude", []any{id, reason})
}
func (h remoteHost) StopDelegation(id string) { h.r.write("Host.StopDelegation", []any{id}) }
func (h remoteHost) Plugins() (out []mcp.Status) {
	h.r.read("Host.Plugins", nil, &out)
	return out
}
func (h remoteHost) CheckPlugins() (out []mcp.Status) {
	h.r.write("Host.CheckPlugins", nil, &out)
	return out
}
func (h remoteHost) PluginsError() (out string) {
	h.r.read("Host.PluginsError", nil, &out)
	return out
}
func (h remoteHost) RestartPlugins() { h.r.write("Host.RestartPlugins", nil) }
func (h remoteHost) SaveServer(old string, s mcp.Server) error {
	return h.r.write("Host.SaveServer", []any{old, s})
}
func (h remoteHost) DeleteServer(name string) error {
	return h.r.write("Host.DeleteServer", []any{name})
}
func (h remoteHost) CheckServer(s mcp.Server) (out host.ServerCheck) {
	if err := h.r.write("Host.CheckServer", []any{s}, &out); err != nil {
		out.Error = err.Error()
	}
	return out
}
func (h remoteHost) Memory(botID string) (out host.Memory) {
	h.r.read("Host.Memory", []any{botID}, &out)
	return out
}
func (h remoteHost) SaveMemory(botID, was, text string) error {
	err := h.r.write("Host.SaveMemory", []any{botID, was, text})
	if err != nil && err.Error() == memory.ErrChanged.Error() {
		return memory.ErrChanged
	}
	return err
}

var (
	_ Store   = remoteStore{}
	_ Runtime = remoteHost{}
)
