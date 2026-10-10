package team

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"strings"

	"github.com/spike/gobot/mcp"
	"github.com/spike/gobot/tool"
)

// maxResourceText bounds the text read_mcp_resource returns.
const maxResourceText = 100_000

// resourceTools reach the resources of the plugins that offer them, named
// as Codex's and pi's: list_mcp_resources, list_mcp_resource_templates and
// read_mcp_resource. They only read. None when no plugin offers any.
func (t *Team) resourceTools(plugins []mcp.Status) []tool.Tool {
	var servers []string
	for _, p := range plugins {
		if p.State == "ready" && p.Resources {
			servers = append(servers, p.ID)
		}
	}
	if len(servers) == 0 || t.Plugins == nil {
		return nil
	}
	which := "One of: " + strings.Join(servers, ", ") + "."
	pick := func(server string) ([]string, error) {
		if server == "" {
			return servers, nil
		}
		for _, s := range servers {
			if s == server {
				return []string{s}, nil
			}
		}
		return nil, fmt.Errorf("no plugin %q offers resources. %s", server, which)
	}
	type listArgs struct {
		Server string `json:"server"`
		Cursor string `json:"cursor"`
	}
	listSchema := `{"type":"object","properties":{"server":{"type":"string","description":"The plugin's id; every plugin with resources when left out. ` + which + `"},"cursor":{"type":"string","description":"nextCursor from the page before; needs server"}},"additionalProperties":false}`
	list := func(name, desc string, page func(ctx context.Context, id, cursor string) (any, string, error)) tool.Tool {
		return Func{N: name, D: desc, S: listSchema, F: func(ctx context.Context, raw json.RawMessage) tool.Result {
			var a listArgs
			json.Unmarshal(raw, &a)
			if a.Cursor != "" && a.Server == "" {
				return errResult("a cursor belongs to one server: name it")
			}
			ids, err := pick(a.Server)
			if err != nil {
				return errResult("%v", err)
			}
			type pageOut struct {
				Server     string `json:"server"`
				Items      any    `json:"items"`
				NextCursor string `json:"nextCursor,omitempty"`
				Error      string `json:"error,omitempty"`
			}
			var out []pageOut
			for _, id := range ids {
				items, next, err := page(ctx, id, a.Cursor)
				po := pageOut{Server: id, Items: items, NextCursor: next}
				if err != nil {
					po.Items, po.Error = []any{}, err.Error()
				}
				out = append(out, po)
			}
			b, _ := json.MarshalIndent(out, "", " ")
			return tool.Result{Text: string(b)}
		}}
	}
	return []tool.Tool{
		list("list_mcp_resources", "List the resources plugins offer (files, records, pages: things to read rather than actions), a page at a time.",
			func(ctx context.Context, id, cursor string) (any, string, error) {
				return t.Plugins.Resources(ctx, id, cursor)
			}),
		list("list_mcp_resource_templates", "List the resource templates plugins offer: URI patterns such as file:///{path} to fill in and read with read_mcp_resource.",
			func(ctx context.Context, id, cursor string) (any, string, error) {
				return t.Plugins.ResourceTemplates(ctx, id, cursor)
			}),
		Func{N: "read_mcp_resource", D: "Read a plugin's resource by its URI. Text comes as text; a picture or other binary content is saved to a file whose path the result names.",
			S: `{"type":"object","properties":{"server":{"type":"string","description":"The plugin's id. ` + which + `"},"uri":{"type":"string"}},"required":["server","uri"],"additionalProperties":false}`,
			F: func(ctx context.Context, raw json.RawMessage) tool.Result {
				var a struct{ Server, URI string }
				json.Unmarshal(raw, &a)
				if _, err := pick(a.Server); err != nil {
					return errResult("%v", err)
				}
				cs, err := t.Plugins.ReadResource(ctx, a.Server, a.URI)
				if err != nil {
					return errResult("%v", err)
				}
				return tool.Result{Text: resourceText(cs)}
			}},
	}
}

// resourceText is what read_mcp_resource returns: each content's text, or
// the private file its bytes went to.
func resourceText(cs []mcp.ResourceContent) string {
	var b strings.Builder
	for i, c := range cs {
		if i > 0 {
			b.WriteString("\n\n")
		}
		if len(cs) > 1 {
			fmt.Fprintf(&b, "[%s]\n", c.URI)
		}
		if c.Blob == "" {
			b.WriteString(c.Text)
			continue
		}
		data, err := base64.StdEncoding.DecodeString(c.Blob)
		if err != nil {
			fmt.Fprintf(&b, "(%s: the content does not decode: %v)", c.URI, err)
			continue
		}
		ext := ""
		if exts, _ := mime.ExtensionsByType(c.MimeType); len(exts) > 0 {
			ext = exts[0]
		}
		f, err := os.CreateTemp("", "gobot-resource-*"+ext)
		if err == nil {
			_, err = f.Write(data)
			f.Close()
		}
		if err != nil {
			fmt.Fprintf(&b, "(%s: could not be saved: %v)", c.URI, err)
			continue
		}
		kind := c.MimeType
		if kind == "" {
			kind = "binary data"
		}
		fmt.Fprintf(&b, "(%s, %d bytes of %s, saved to %s)", c.URI, len(data), kind, f.Name())
	}
	s := b.String()
	if len(s) > maxResourceText {
		s = s[:maxResourceText] + "\n… (cut: the resource is longer)"
	}
	return s
}
