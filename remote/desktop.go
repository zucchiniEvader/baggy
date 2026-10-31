package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/team"
)

// StoreCalls are the store's methods a desktop may call.
var StoreCalls = []string{
	"Dir", "Workspace", "Workspaces", "WorkDir", "Setting", "SetSetting",
	"SaveProvider", "Providers", "Provider", "DeleteProvider",
	"CreateBot", "UpdateBot", "SaveAvatar", "Bot", "Bots", "DeleteBot",
	"SaveRoutine", "Routine", "Routines", "DeleteRoutine",
	"Chat", "Chats", "DMOf", "CreateGroup", "AddMember", "RemoveMember", "SetOwner", "SetGroupInfo", "DeleteChat",
	"Append", "Messages", "Entry", "MarkRead", "Unread", "SetPinned", "Previews",
}

// storeReads are the StoreCalls that change nothing; after any other, the
// server tells every phone and desktop that bots or chats changed.
var storeReads = []string{"Dir", "Workspace", "Workspaces", "WorkDir", "Setting", "Providers", "Provider", "Bot", "Bots",
	"Routine", "Routines", "Chat", "Chats", "DMOf", "Messages", "Entry", "Unread", "Previews"}

// HostCalls are the host's methods a desktop may call.
var HostCalls = []string{
	"Snapshot", "SendFiles", "Stop", "Interrupt", "Continue", "Busy", "RunRoutine", "Config", "HasFallback", "ClaudeCodeMissing",
	"Mode", "Rules", "Decide", "Waiting", "DenyAll",
	"Tasks", "StopShell", "BackgroundShell", "StopClaude", "StopDelegation",
	"Plugins", "CheckPlugins", "PluginsError", "RestartPlugins", "SaveServer", "DeleteServer", "CheckServer",
	"Memory", "SaveMemory",
}

// Call is a method call as it crosses: "Store.Bots" or "Host.Snapshot",
// each argument as JSON.
type Call struct {
	Method string            `json:"method"`
	Args   []json.RawMessage `json:"args"`
}

// Return is what a call gave: its results but the error, and the error's
// message.
type Return struct {
	Results []json.RawMessage `json:"results"`
	Error   string            `json:"error,omitempty"`
}

var errorType = reflect.TypeFor[error]()

// call runs one of StoreCalls or HostCalls.
func (s *Server) call(w http.ResponseWriter, r *http.Request) {
	var c Call
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&c); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	recv, name, _ := strings.Cut(c.Method, ".")
	var target reflect.Value
	switch {
	case recv == "Store" && contains(StoreCalls, name):
		target = reflect.ValueOf(s.Host.Store)
	case recv == "Host" && contains(HostCalls, name):
		target = reflect.ValueOf(s.Host)
	default:
		fail(w, http.StatusNotFound, fmt.Errorf("no method %s", c.Method))
		return
	}
	m := target.MethodByName(name)
	t := m.Type()
	if len(c.Args) != t.NumIn() {
		fail(w, http.StatusBadRequest, fmt.Errorf("%s takes %d arguments", c.Method, t.NumIn()))
		return
	}
	in := make([]reflect.Value, len(c.Args))
	for i, a := range c.Args {
		v := reflect.New(t.In(i))
		if err := json.Unmarshal(a, v.Interface()); err != nil {
			fail(w, http.StatusBadRequest, fmt.Errorf("%s, argument %d: %w", c.Method, i+1, err))
			return
		}
		in[i] = v.Elem()
	}
	ret := Return{Results: []json.RawMessage{}}
	for i, v := range m.Call(in) {
		if t.Out(i) == errorType {
			if !v.IsNil() {
				ret.Error = v.Interface().(error).Error()
			}
			continue
		}
		b, err := json.Marshal(v.Interface())
		if err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
		ret.Results = append(ret.Results, b)
	}
	if recv == "Store" && !contains(storeReads, name) {
		s.Publish(team.Event{Kind: team.EventChanged})
	}
	reply(w, ret)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// file serves an attached file's bytes.
func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	f := store.File{ID: r.PathValue("id"), Name: r.PathValue("name")}
	for _, part := range []string{f.ID, f.Name} {
		if part != filepath.Base(part) || strings.HasPrefix(part, ".") {
			fail(w, http.StatusBadRequest, errors.New("bad name"))
			return
		}
	}
	p := s.Host.Store.FilePath(f)
	if _, err := os.Stat(p); err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, p)
}

// FullEvent is a team.Event as a desktop gets it: all of it.
type FullEvent struct {
	Kind    team.EventKind `json:"kind"`
	ChatID  string         `json:"chat_id"`
	BotID   string         `json:"bot_id,omitempty"`
	Turn    string         `json:"turn,omitempty"`
	Since   int64          `json:"since,omitempty"`
	Agent   *agent.Event   `json:"agent,omitempty"`
	Entry   *store.Entry   `json:"entry,omitempty"`
	Error   string         `json:"error,omitempty"`
	Waiting bool           `json:"waiting,omitempty"`
}

// Full is e as a desktop gets it.
func Full(e team.Event) FullEvent {
	out := FullEvent{Kind: e.Kind, ChatID: e.ChatID, BotID: e.BotID, Turn: e.Turn, Since: e.Since, Entry: e.Entry, Waiting: e.Waiting}
	if e.Agent.Kind != "" || e.Agent.Call != nil {
		ev := e.Agent
		out.Agent = &ev
	}
	if e.Err != nil {
		out.Error = e.Err.Error()
	}
	return out
}

// Event is the team.Event fe was.
func (fe FullEvent) Event() team.Event {
	e := team.Event{Kind: fe.Kind, ChatID: fe.ChatID, BotID: fe.BotID, Turn: fe.Turn, Since: fe.Since, Entry: fe.Entry, Waiting: fe.Waiting}
	if fe.Agent != nil {
		e.Agent = *fe.Agent
	}
	if fe.Error != "" {
		e.Err = errors.New(fe.Error)
	}
	return e
}
