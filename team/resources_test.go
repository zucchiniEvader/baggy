package team

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spike/gobot/mcp"
	"github.com/spike/gobot/tool"
)

// resourceServer is a Streamable HTTP MCP server with two resources.
func resourceServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			ID     json.RawMessage
			Method string
			Params struct{ URI, Cursor string }
		}
		json.NewDecoder(r.Body).Decode(&m)
		if len(m.ID) == 0 {
			w.WriteHeader(202)
			return
		}
		var res any
		switch m.Method {
		case "initialize":
			res = map[string]any{"protocolVersion": mcp.ProtocolVersion, "serverInfo": map[string]string{"name": "docs"}, "capabilities": map[string]any{"resources": map[string]any{}}}
		case "tools/list":
			res = map[string]any{"tools": []any{}}
		case "resources/list":
			res = map[string]any{"resources": []map[string]any{{"uri": "doc://readme", "name": "readme"}, {"uri": "doc://logo", "name": "logo", "mimeType": "image/png"}}}
		case "resources/templates/list":
			res = map[string]any{"resourceTemplates": []map[string]any{{"uriTemplate": "doc://{name}"}}}
		case "resources/read":
			if m.Params.URI == "doc://logo" {
				res = map[string]any{"contents": []map[string]any{{"uri": m.Params.URI, "mimeType": "image/png", "blob": "iVBORw0KGgo="}}}
			} else {
				res = map[string]any{"contents": []map[string]any{{"uri": m.Params.URI, "text": "Read me first."}}}
			}
		}
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": res})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestResourceTools(t *testing.T) {
	srv := resourceServer(t)
	path := filepath.Join(t.TempDir(), "mcp.json")
	b, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"Docs": map[string]any{"type": "http", "url": srv.URL}}})
	os.WriteFile(path, b, 0o644)
	m := mcp.NewManager(path)
	defer m.Close()
	tm := &Team{Plugins: m}
	ctx := context.Background()
	plugins := tm.plugins(ctx)
	if len(plugins) != 1 || !plugins[0].Resources {
		t.Fatalf("plugins %+v", plugins)
	}
	if !strings.Contains(PluginsPrompt(plugins), "offers resources") {
		t.Fatal("the prompt does not say it")
	}
	tools := map[string]tool.Tool{}
	for _, tl := range tm.resourceTools(plugins) {
		tools[tl.Name()] = tl
	}
	run := func(name, args string) tool.Result {
		return tools[name].Execute(ctx, json.RawMessage(args))
	}
	if r := run("list_mcp_resources", `{}`); r.IsError || !strings.Contains(r.Text, `"server": "docs"`) || !strings.Contains(r.Text, "doc://readme") {
		t.Fatalf("list %+v", r)
	}
	if r := run("list_mcp_resource_templates", `{"server":"docs"}`); !strings.Contains(r.Text, "doc://{name}") {
		t.Fatalf("templates %+v", r)
	}
	if r := run("list_mcp_resources", `{"cursor":"2"}`); !r.IsError {
		t.Fatal("a cursor with no server")
	}
	if r := run("read_mcp_resource", `{"server":"docs","uri":"doc://readme"}`); r.Text != "Read me first." {
		t.Fatalf("read %+v", r)
	}
	r := run("read_mcp_resource", `{"server":"docs","uri":"doc://logo"}`)
	i := strings.Index(r.Text, "saved to ")
	if i < 0 {
		t.Fatalf("blob %+v", r)
	}
	saved := strings.TrimSuffix(r.Text[i+len("saved to "):], ")")
	defer os.Remove(saved)
	if data, err := os.ReadFile(saved); err != nil || string(data[1:4]) != "PNG" || !strings.HasSuffix(saved, ".png") {
		t.Fatalf("saved %q %v", saved, err)
	}
	if r := run("read_mcp_resource", `{"server":"nope","uri":"x"}`); !r.IsError {
		t.Fatal("an unknown server read")
	}
	if tm.resourceTools(nil) != nil {
		t.Fatal("tools with no plugins")
	}
}
