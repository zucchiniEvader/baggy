// Package mcp is a client of Model Context Protocol servers: JSON-RPC over a
// command's stdin and stdout, or over Streamable HTTP. It lists a server's
// tools and calls them; the user's servers are listed in mcp.json (Manager).
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ProtocolVersion is the MCP revision the client speaks.
const ProtocolVersion = "2025-06-18"

// Tool is a server's tool.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations struct {
		Title        string `json:"title"`
		ReadOnlyHint bool   `json:"readOnlyHint"`
	} `json:"annotations"`
}

// Content is one item of a tool result.
type Content struct {
	Type     string `json:"type"` // text, image, audio, resource, resource_link
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	URI      string `json:"uri,omitempty"`
}

// Result is a tools/call result.
type Result struct {
	Content           []Content       `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError"`
}

// Text joins a result's text items.
func (r Result) Text() string {
	var parts []string
	for _, c := range r.Content {
		switch c.Type {
		case "text":
			parts = append(parts, c.Text)
		case "resource_link", "resource":
			parts = append(parts, c.URI)
		case "image":
			parts = append(parts, "[image "+c.MimeType+"]")
		}
	}
	if len(parts) == 0 && len(r.StructuredContent) > 0 {
		return string(r.StructuredContent)
	}
	return strings.Join(parts, "\n")
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("%s (%d)", e.Message, e.Code) }

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// transport sends one request and returns its result, or a notification.
type transport interface {
	call(ctx context.Context, method string, params any) (json.RawMessage, error)
	notify(ctx context.Context, method string, params any) error
	close() error
}

// Client is a connected server.
type Client struct {
	t            transport
	Name         string // the server's own, from initialize
	Instructions string
	HasResources bool // it offers resources (its capabilities say so)
}

// Connect starts or reaches a server and initializes it.
func Connect(ctx context.Context, s Server) (*Client, error) {
	var t transport
	var err error
	if s.URL != "" {
		t = newHTTP(s)
	} else if s.Command != "" {
		t, err = newStdio(s)
	} else {
		err = errors.New("a server needs a command or a url")
	}
	if err != nil {
		return nil, err
	}
	raw, err := t.call(ctx, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "gobot", "version": "0.1"},
	})
	if err != nil {
		t.close()
		return nil, err
	}
	var init struct {
		ServerInfo   struct{ Name string } `json:"serverInfo"`
		Instructions string                `json:"instructions"`
		Capabilities struct {
			Resources json.RawMessage `json:"resources"`
		} `json:"capabilities"`
	}
	json.Unmarshal(raw, &init)
	if err := t.notify(ctx, "notifications/initialized", nil); err != nil {
		t.close()
		return nil, err
	}
	res := init.Capabilities.Resources
	return &Client{t: t, Name: init.ServerInfo.Name, Instructions: init.Instructions, HasResources: len(res) > 0 && string(res) != "null"}, nil
}

// Tools lists the server's tools, every page.
func (c *Client) Tools(ctx context.Context) ([]Tool, error) {
	var all []Tool
	cursor := ""
	for page := 0; page < 100; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.t.call(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var r struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		all = append(all, r.Tools...)
		if r.NextCursor == "" {
			break
		}
		cursor = r.NextCursor
	}
	return all, nil
}

// Call runs a tool.
func (c *Client) Call(ctx context.Context, name string, args json.RawMessage) (Result, error) {
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	raw, err := c.t.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return Result{}, err
	}
	var r Result
	err = json.Unmarshal(raw, &r)
	return r, err
}

func (c *Client) Close() error { return c.t.close() }

// ---- stdio ----

type stdio struct {
	cmd     *exec.Cmd
	in      io.WriteCloser
	mu      sync.Mutex // writes
	next    atomic.Int64
	pending sync.Map // id → chan message
	done    chan struct{}
	stderr  *tail
	err     error
}

// tail keeps the end of a command's stderr, for errors.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 4096 {
		t.buf = t.buf[len(t.buf)-4096:]
	}
	t.mu.Unlock()
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

func newStdio(s Server) (*stdio, error) {
	cmd := exec.Command(s.Command, s.Args...)
	cmd.Dir = workDir(s.Cwd)
	cmd.Env = os.Environ()
	if Env != nil {
		cmd.Env = Env()
	}
	for k, v := range s.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	st := &stdio{cmd: cmd, done: make(chan struct{}), stderr: &tail{}}
	cmd.Stderr = st.stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", s.Command, err)
	}
	st.in = in
	go st.read(out)
	return st, nil
}

// Env, when set, gives the environment server commands start with (the
// login shell's, so npx and uvx are found).
var Env func() []string

// workDir is where a command starts: its cwd with ~ read as the home
// folder, the home folder when it names none.
func workDir(cwd string) string {
	home, _ := os.UserHomeDir()
	switch {
	case cwd == "", cwd == "~":
		return home
	case strings.HasPrefix(cwd, "~/"):
		return filepath.Join(home, cwd[2:])
	}
	return cwd
}

func (s *stdio) read(out io.Reader) {
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<16), 64<<20)
	for sc.Scan() {
		var m message
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue // a server's stray log line
		}
		switch {
		case m.Method != "" && len(m.ID) > 0: // a request from the server: we offer nothing
			s.send(message{JSONRPC: "2.0", ID: m.ID, Error: &rpcError{Code: -32601, Message: "method not found"}})
		case m.Method != "": // a notification
		default:
			if ch, ok := s.pending.LoadAndDelete(string(m.ID)); ok {
				ch.(chan message) <- m
			}
		}
	}
	s.err = errors.New("the server exited")
	if e := s.stderr.String(); e != "" {
		s.err = fmt.Errorf("the server exited: %s", lastLine(e))
	}
	close(s.done)
}

func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func (s *stdio) send(m message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.in.Write(append(b, '\n'))
	return err
}

func (s *stdio) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := json.RawMessage(fmt.Sprint(s.next.Add(1)))
	ch := make(chan message, 1)
	s.pending.Store(string(id), ch)
	defer s.pending.Delete(string(id))
	if err := s.send(message{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		return nil, err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return nil, m.Error
		}
		return m.Result, nil
	case <-s.done:
		return nil, s.err
	case <-ctx.Done():
		s.send(message{JSONRPC: "2.0", Method: "notifications/cancelled", Params: map[string]any{"requestId": id}})
		return nil, ctx.Err()
	}
}

func (s *stdio) notify(ctx context.Context, method string, params any) error {
	return s.send(message{JSONRPC: "2.0", Method: method, Params: params})
}

func (s *stdio) close() error {
	s.in.Close()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		s.cmd.Process.Kill()
	}
	return s.cmd.Wait()
}

// ---- Streamable HTTP ----

type httpT struct {
	url     string
	headers map[string]string
	client  *http.Client
	next    atomic.Int64
	mu      sync.Mutex
	session string
}

func newHTTP(s Server) *httpT {
	return &httpT{url: s.URL, headers: s.Headers, client: &http.Client{}}
}

func (h *httpT) post(ctx context.Context, m message) (*http.Response, error) {
	b, _ := json.Marshal(m)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json, text/event-stream")
	req.Header.Set("mcp-protocol-version", ProtocolVersion)
	for k, v := range h.headers {
		req.Header.Set(k, os.ExpandEnv(v))
	}
	h.mu.Lock()
	if h.session != "" {
		req.Header.Set("mcp-session-id", h.session)
	}
	h.mu.Unlock()
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	if sid := resp.Header.Get("mcp-session-id"); sid != "" {
		h.mu.Lock()
		h.session = sid
		h.mu.Unlock()
	}
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return nil, fmt.Errorf("%d: the server wants a sign-in (set its headers in mcp.json): %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return nil, fmt.Errorf("%d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

func (h *httpT) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := json.RawMessage(fmt.Sprint(h.next.Add(1)))
	resp, err := h.post(ctx, message{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	found := func(m message) (json.RawMessage, bool, error) {
		if string(m.ID) != string(id) || m.Method != "" {
			return nil, false, nil
		}
		if m.Error != nil {
			return nil, true, m.Error
		}
		return m.Result, true, nil
	}
	if strings.HasPrefix(resp.Header.Get("content-type"), "text/event-stream") {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<16), 64<<20)
		for sc.Scan() {
			data, ok := strings.CutPrefix(sc.Text(), "data:")
			if !ok {
				continue
			}
			var m message
			if json.Unmarshal([]byte(strings.TrimSpace(data)), &m) != nil {
				continue
			}
			if r, ok, err := found(m); ok {
				return r, err
			}
		}
		return nil, errors.New("the stream ended without a response")
	}
	var m message
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	r, _, err := found(m)
	return r, err
}

func (h *httpT) notify(ctx context.Context, method string, params any) error {
	resp, err := h.post(ctx, message{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (h *httpT) close() error {
	h.mu.Lock()
	sid := h.session
	h.mu.Unlock()
	if sid != "" { // end the session
		req, _ := http.NewRequest(http.MethodDelete, h.url, nil)
		req.Header.Set("mcp-session-id", sid)
		if resp, err := h.client.Do(req); err == nil {
			resp.Body.Close()
		}
	}
	return nil
}
