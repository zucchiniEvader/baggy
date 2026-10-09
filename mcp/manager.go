package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Server is one entry of mcp.json: a command, or a remote server.
type Server struct {
	Name        string            `json:"-"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Cwd         string            `json:"cwd,omitempty"`
	Type        string            `json:"type,omitempty"` // "http" (or "streamable-http") for a remote one
	URL         string            `json:"url,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Description string            `json:"description,omitempty"`
	Disabled    bool              `json:"disabled,omitempty"`
	// Timeout: seconds, or milliseconds when 1000 or more; ten minutes unset.
	Timeout float64 `json:"timeout,omitempty"`
}

// CallTimeout is how long a call may take.
func (s Server) CallTimeout() time.Duration {
	switch {
	case s.Timeout <= 0:
		return 10 * time.Minute
	case s.Timeout >= 1000:
		return time.Duration(s.Timeout) * time.Millisecond
	}
	return time.Duration(s.Timeout * float64(time.Second))
}

// File is mcp.json: {"mcpServers": {"<name>": {…}}}.
type File struct {
	Servers map[string]Server `json:"mcpServers"`
}

// Parse reads mcp.json; also the "servers" spelling VS Code uses.
func Parse(b []byte) ([]Server, error) {
	var f struct {
		MCPServers map[string]Server `json:"mcpServers"`
		Servers    map[string]Server `json:"servers"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("mcp.json: %w", err)
	}
	all := f.MCPServers
	if all == nil {
		all = f.Servers
	}
	var out []Server
	for name, s := range all {
		s.Name = name
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

var unsafe = regexp.MustCompile(`[^a-z0-9]+`)

// ID is a server's name as it names tools: lowercase letters, digits and _.
func ID(name string) string {
	id := strings.Trim(unsafe.ReplaceAllString(strings.ToLower(name), "_"), "_")
	if id == "" {
		id = "server"
	}
	return id
}

// Status is how a server stands.
type Status struct {
	Server Server
	ID     string
	State  string // "off", "connecting", "ready", "failed"
	Err    string
	Tools  []Tool
}

type conn struct {
	mu     sync.Mutex
	server Server
	client *Client
	tools  []Tool
	err    error
	tried  time.Time
}

// Manager keeps the user's servers: connected on first use, their tools
// cached, the file read again when it changes.
type Manager struct {
	Path string

	mu    sync.Mutex
	mod   time.Time
	conns map[string]*conn // by ID
	order []string
	fail  error // the file does not read
}

func NewManager(path string) *Manager { return &Manager{Path: path, conns: map[string]*conn{}} }

// reload reads mcp.json again when it changed.
func (m *Manager) reload() {
	info, err := os.Stat(m.Path)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.closeAll()
		m.mod, m.fail = time.Time{}, nil
		return
	}
	if info.ModTime().Equal(m.mod) {
		return
	}
	m.mod = info.ModTime()
	b, err := os.ReadFile(m.Path)
	if err == nil {
		var servers []Server
		if servers, err = Parse(b); err == nil {
			m.fail = nil
			m.closeAll()
			for _, s := range servers {
				id := ID(s.Name)
				if _, dup := m.conns[id]; dup {
					continue
				}
				m.conns[id] = &conn{server: s}
				m.order = append(m.order, id)
			}
			return
		}
	}
	m.fail = err
}

func (m *Manager) closeAll() {
	for _, c := range m.conns {
		c.mu.Lock()
		if c.client != nil {
			go c.client.Close()
		}
		c.mu.Unlock()
	}
	m.conns, m.order = map[string]*conn{}, nil
}

// Close stops every server.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeAll()
}

// FileError is why mcp.json does not read, or nil.
func (m *Manager) FileError() error {
	m.reload()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.fail
}

func (m *Manager) get(id string) *conn {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.conns[id]
}

// retryAfter is how soon a server that failed to connect is tried again.
const retryAfter = 30 * time.Second

// ready connects a server if needed and lists its tools.
func (c *conn) ready(ctx context.Context) (*Client, []Tool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.server.Disabled {
		return nil, nil, errors.New("disabled in mcp.json")
	}
	if c.client != nil {
		return c.client, c.tools, nil
	}
	if c.err != nil && time.Since(c.tried) < retryAfter {
		return nil, nil, c.err
	}
	c.tried = time.Now()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cl, err := Connect(ctx, c.server)
	if err == nil {
		var tools []Tool
		if tools, err = cl.Tools(ctx); err == nil {
			c.client, c.tools, c.err = cl, tools, nil
			return cl, tools, nil
		}
		cl.Close()
	}
	c.err = err
	return nil, nil, err
}

// Statuses connects every server that is on and reports how each stands.
func (m *Manager) Statuses(ctx context.Context) []Status {
	m.reload()
	m.mu.Lock()
	ids := append([]string(nil), m.order...)
	m.mu.Unlock()
	out := make([]Status, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		c := m.get(id)
		if c == nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			st := Status{Server: c.server, ID: id, State: "ready"}
			if c.server.Disabled {
				st.State = "off"
			} else if _, tools, err := c.ready(ctx); err != nil {
				st.State, st.Err = "failed", err.Error()
			} else {
				st.Tools = tools
			}
			out[i] = st
		}()
	}
	wg.Wait()
	return out
}

// Known reports the servers' statuses without connecting any: ready ones
// with their tools, the rest as they last stood.
func (m *Manager) Known() []Status {
	m.reload()
	m.mu.Lock()
	ids := append([]string(nil), m.order...)
	m.mu.Unlock()
	var out []Status
	for _, id := range ids {
		c := m.get(id)
		if c == nil {
			continue
		}
		c.mu.Lock()
		st := Status{Server: c.server, ID: id, State: "connecting", Tools: c.tools}
		switch {
		case c.server.Disabled:
			st.State = "off"
		case c.client != nil:
			st.State = "ready"
		case c.err != nil:
			st.State, st.Err = "failed", c.err.Error()
		}
		c.mu.Unlock()
		out = append(out, st)
	}
	return out
}

// Call runs <server id>__<tool>.
func (m *Manager) Call(ctx context.Context, qualified string, args json.RawMessage) (Result, error) {
	id, name, ok := strings.Cut(qualified, "__")
	if !ok {
		return Result{}, fmt.Errorf("%q is not <server>__<tool>", qualified)
	}
	m.reload()
	c := m.get(id)
	if c == nil {
		return Result{}, fmt.Errorf("no MCP server %q", id)
	}
	cl, _, err := c.ready(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", c.server.Name, err)
	}
	ctx, cancel := context.WithTimeout(ctx, c.server.CallTimeout())
	defer cancel()
	r, err := cl.Call(ctx, name, args)
	if err != nil && ctx.Err() == nil && !isRPC(err) { // the connection broke: once more, fresh
		c.mu.Lock()
		c.client = nil
		c.mu.Unlock()
		if cl, _, err2 := c.ready(ctx); err2 == nil {
			r, err = cl.Call(ctx, name, args)
		}
	}
	return r, err
}

func isRPC(err error) bool {
	var e *rpcError
	return errors.As(err, &e)
}

// Lookup finds a qualified tool among the ready servers.
func (m *Manager) Lookup(qualified string) (Tool, bool) {
	id, name, _ := strings.Cut(qualified, "__")
	c := m.get(id)
	if c == nil {
		return Tool{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.tools {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}
