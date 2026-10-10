package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary play a stdio server.
func TestMain(m *testing.M) {
	if os.Getenv("GOBOT_FAKE_MCP") == "1" {
		serve(os.Stdin, os.Stdout)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// answer is a fake server's reply to a request.
func answer(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		return map[string]any{"protocolVersion": ProtocolVersion, "serverInfo": map[string]string{"name": "fake"}, "instructions": "Use echo.",
			"capabilities": map[string]any{"tools": map[string]any{}, "resources": map[string]any{}}}, nil
	case "resources/list":
		var p struct{ Cursor string }
		json.Unmarshal(params, &p)
		if p.Cursor == "" {
			return map[string]any{"resources": []map[string]any{{"uri": "file:///notes.md", "name": "notes", "mimeType": "text/markdown"}, {"uri": "ui://widget", "name": "an app"}}, "nextCursor": "2"}, nil
		}
		return map[string]any{"resources": []map[string]any{{"uri": "file:///logo.png", "name": "logo", "mimeType": "image/png"}}}, nil
	case "resources/templates/list":
		return map[string]any{"resourceTemplates": []map[string]any{{"uriTemplate": "file:///{path}", "name": "files"}}}, nil
	case "resources/read":
		var p struct{ URI string }
		json.Unmarshal(params, &p)
		if p.URI == "file:///logo.png" {
			return map[string]any{"contents": []map[string]any{{"uri": p.URI, "mimeType": "image/png", "blob": "iVBORw0KGgo="}}}, nil
		}
		return map[string]any{"contents": []map[string]any{{"uri": p.URI, "mimeType": "text/markdown", "text": "# Notes"}}}, nil
	case "tools/list":
		var p struct{ Cursor string }
		json.Unmarshal(params, &p)
		if p.Cursor == "" { // two pages
			return map[string]any{"tools": []map[string]any{{"name": "echo", "description": "Echoes", "inputSchema": map[string]any{"type": "object"}, "annotations": map[string]any{"readOnlyHint": true}}}, "nextCursor": "2"}, nil
		}
		return map[string]any{"tools": []map[string]any{{"name": "fail", "inputSchema": map[string]any{"type": "object"}}}}, nil
	case "tools/call":
		var p struct {
			Name      string
			Arguments map[string]any
		}
		json.Unmarshal(params, &p)
		if p.Name == "fail" {
			return map[string]any{"content": []map[string]any{{"type": "text", "text": "it broke"}}, "isError": true}, nil
		}
		b, _ := json.Marshal(p.Arguments)
		return map[string]any{"content": []map[string]any{{"type": "text", "text": "echo " + string(b)}}, "structuredContent": p.Arguments}, nil
	}
	return nil, &rpcError{Code: -32601, Message: "no " + method}
}

func serve(in io.Reader, out io.Writer) {
	sc := bufio.NewScanner(in)
	fmt.Fprintln(out, "not json: a log line") // servers do this
	for sc.Scan() {
		var m struct {
			ID     json.RawMessage
			Method string
			Params json.RawMessage
		}
		json.Unmarshal(sc.Bytes(), &m)
		if len(m.ID) == 0 {
			continue
		}
		r, e := answer(m.Method, m.Params)
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": r, "error": e})
		if e == nil {
			b, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": r})
		}
		fmt.Fprintln(out, string(b))
	}
}

func stdioServer(t *testing.T) Server {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Server{Name: "Fake Tools", Command: exe, Env: map[string]string{"GOBOT_FAKE_MCP": "1"}}
}

func check(t *testing.T, s Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Connect(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Name != "fake" || c.Instructions != "Use echo." {
		t.Fatalf("init: %+v", c)
	}
	tools, err := c.Tools(ctx)
	if err != nil || len(tools) != 2 || !tools[0].Annotations.ReadOnlyHint {
		t.Fatalf("tools %+v %v", tools, err)
	}
	r, err := c.Call(ctx, "echo", json.RawMessage(`{"x":1}`))
	if err != nil || r.Text() != `echo {"x":1}` || string(r.StructuredContent) != `{"x":1}` {
		t.Fatalf("call %+v %v", r, err)
	}
	if r, _ := c.Call(ctx, "fail", nil); !r.IsError {
		t.Fatal("error result lost")
	}
	if !c.HasResources {
		t.Fatal("resources not offered")
	}
	rs, next, err := c.Resources(ctx, "")
	if err != nil || len(rs) != 1 || rs[0].URI != "file:///notes.md" || next != "2" {
		t.Fatalf("resources %+v %q %v", rs, next, err)
	}
	if rs, next, _ := c.Resources(ctx, next); len(rs) != 1 || next != "" {
		t.Fatalf("page 2 %+v %q", rs, next)
	}
	if ts, _, err := c.ResourceTemplates(ctx, ""); err != nil || len(ts) != 1 || ts[0].URITemplate != "file:///{path}" {
		t.Fatalf("templates %+v %v", ts, err)
	}
	if cs, err := c.ReadResource(ctx, "file:///notes.md"); err != nil || len(cs) != 1 || cs[0].Text != "# Notes" {
		t.Fatalf("read %+v %v", cs, err)
	}
}

func TestStdio(t *testing.T) { check(t, stdioServer(t)) }

func TestStreamableHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			return
		}
		if r.Header.Get("authorization") != "Bearer secret" {
			w.WriteHeader(401)
			return
		}
		var m struct {
			ID     json.RawMessage
			Method string
			Params json.RawMessage
		}
		json.NewDecoder(r.Body).Decode(&m)
		if m.Method == "initialize" {
			w.Header().Set("mcp-session-id", "s1")
		} else if r.Header.Get("mcp-session-id") != "s1" {
			w.WriteHeader(400)
			return
		}
		if len(m.ID) == 0 {
			w.WriteHeader(202)
			return
		}
		res, _ := answer(m.Method, m.Params)
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": res})
		if m.Method == "tools/call" { // this one streams
			w.Header().Set("content-type", "text/event-stream")
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\nevent: message\ndata: %s\n\n", b)
			return
		}
		w.Header().Set("content-type", "application/json")
		w.Write(b)
	}))
	defer srv.Close()
	t.Setenv("MCP_TOKEN", "secret")
	check(t, Server{Name: "remote", Type: "http", URL: srv.URL, Headers: map[string]string{"Authorization": "Bearer ${MCP_TOKEN}"}})
	_, err := Connect(context.Background(), Server{URL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "sign-in") {
		t.Fatalf("unauthorized: %v", err)
	}
}

func TestManager(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	s := stdioServer(t)
	f := map[string]any{"mcpServers": map[string]any{
		"Fake Tools": map[string]any{"command": s.Command, "env": s.Env},
		"Broken":     map[string]any{"command": filepath.Join(dir, "nope")},
		"Off":        map[string]any{"command": "x", "disabled": true},
	}}
	b, _ := json.Marshal(f)
	os.WriteFile(path, b, 0o644)
	m := NewManager(path)
	defer m.Close()
	st := m.Statuses(context.Background())
	if len(st) != 3 || st[0].State != "failed" || st[1].ID != "fake_tools" || len(st[1].Tools) != 2 || st[2].State != "off" {
		t.Fatalf("statuses %+v", st)
	}
	r, err := m.Call(context.Background(), "fake_tools__echo", json.RawMessage(`{"y":2}`))
	if err != nil || !strings.Contains(r.Text(), `"y":2`) {
		t.Fatalf("call %+v %v", r, err)
	}
	if tl, ok := m.Lookup("fake_tools__echo"); !ok || !tl.Annotations.ReadOnlyHint {
		t.Fatal("lookup")
	}
	os.WriteFile(path, []byte("{bad"), 0o644)
	os.Chtimes(path, time.Now().Add(time.Second), time.Now().Add(time.Second))
	if m.FileError() == nil {
		t.Fatal("bad file read")
	}
	if s := (Server{Timeout: 30}).CallTimeout(); s != 30*time.Second {
		t.Fatal(s)
	}
	if s := (Server{Timeout: 1500}).CallTimeout(); s != 1500*time.Millisecond {
		t.Fatal(s)
	}
}
