package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Resource is a server's resource, without MCP Apps' icons and _meta.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
	Size        int64  `json:"size,omitempty"`
}

// ResourceTemplate is a family of resources named by an RFC 6570 template.
type ResourceTemplate struct {
	URITemplate string `json:"uriTemplate"`
	Name        string `json:"name,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ResourceContent is what reading a resource gives: text, or a blob in
// base64.
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// Resources lists a page of the server's resources and the next page's
// cursor ("" after the last). MCP Apps' ui:// interfaces are left out.
func (c *Client) Resources(ctx context.Context, cursor string) ([]Resource, string, error) {
	var r struct {
		Resources  []Resource `json:"resources"`
		NextCursor string     `json:"nextCursor"`
	}
	if err := c.page(ctx, "resources/list", cursor, &r); err != nil {
		return nil, "", err
	}
	out := r.Resources[:0]
	for _, x := range r.Resources {
		if !strings.HasPrefix(x.URI, "ui://") {
			out = append(out, x)
		}
	}
	return out, r.NextCursor, nil
}

// ResourceTemplates lists a page of the server's resource templates.
func (c *Client) ResourceTemplates(ctx context.Context, cursor string) ([]ResourceTemplate, string, error) {
	var r struct {
		ResourceTemplates []ResourceTemplate `json:"resourceTemplates"`
		NextCursor        string             `json:"nextCursor"`
	}
	if err := c.page(ctx, "resources/templates/list", cursor, &r); err != nil {
		return nil, "", err
	}
	out := r.ResourceTemplates[:0]
	for _, x := range r.ResourceTemplates {
		if !strings.HasPrefix(x.URITemplate, "ui://") {
			out = append(out, x)
		}
	}
	return out, r.NextCursor, nil
}

// ReadResource reads a resource by its URI.
func (c *Client) ReadResource(ctx context.Context, uri string) ([]ResourceContent, error) {
	raw, err := c.t.call(ctx, "resources/read", map[string]any{"uri": uri})
	if err != nil {
		return nil, err
	}
	var r struct {
		Contents []ResourceContent `json:"contents"`
	}
	err = json.Unmarshal(raw, &r)
	return r.Contents, err
}

func (c *Client) page(ctx context.Context, method, cursor string, into any) error {
	params := map[string]any{}
	if cursor != "" {
		params["cursor"] = cursor
	}
	raw, err := c.t.call(ctx, method, params)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, into)
}

// resourceClient is the connected client of a server that offers resources.
func (m *Manager) resourceClient(ctx context.Context, id string) (*Client, error) {
	m.reload()
	c := m.get(id)
	if c == nil {
		return nil, fmt.Errorf("no MCP server %q", id)
	}
	cl, _, err := c.ready(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.server.Name, err)
	}
	if !cl.HasResources {
		return nil, fmt.Errorf("%s offers no resources", c.server.Name)
	}
	return cl, nil
}

// Resources lists a page of a server's resources.
func (m *Manager) Resources(ctx context.Context, id, cursor string) ([]Resource, string, error) {
	cl, err := m.resourceClient(ctx, id)
	if err != nil {
		return nil, "", err
	}
	return cl.Resources(ctx, cursor)
}

// ResourceTemplates lists a page of a server's resource templates.
func (m *Manager) ResourceTemplates(ctx context.Context, id, cursor string) ([]ResourceTemplate, string, error) {
	cl, err := m.resourceClient(ctx, id)
	if err != nil {
		return nil, "", err
	}
	return cl.ResourceTemplates(ctx, cursor)
}

// ReadResource reads a resource of a server.
func (m *Manager) ReadResource(ctx context.Context, id, uri string) ([]ResourceContent, error) {
	cl, err := m.resourceClient(ctx, id)
	if err != nil {
		return nil, err
	}
	return cl.ReadResource(ctx, uri)
}
