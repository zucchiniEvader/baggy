package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/egoist/mygo/ui"
)

// mcpTemplate starts an mcp.json for the user to fill in.
const mcpTemplate = `{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "~/Documents"],
      "disabled": true
    }
  }
}
`

// checkServers connects every server in the background and shows how each
// stands.
func (a *app) checkServers() {
	if a.team.Plugins == nil || a.checking {
		return
	}
	a.checking = true
	go func() {
		st := a.team.Plugins.Statuses(context.Background())
		a.post(func() { a.servers, a.checking = st, false })
	}()
}

// pluginsView is Settings' MCP Servers section.
func (a *app) pluginsView(c *ui.Context) {
	t := c.Theme()
	if a.team.Plugins == nil {
		return
	}
	ui.Text(c, L("MCP Servers")).Bold()
	ui.Text(c, L("Servers in mcp.json (the format Claude Desktop and Cursor use) give every bot their tools, which it calls from codemode scripts.")).FontSize(12).TextColor(t.TextMuted)
	if err := a.team.Plugins.FileError(); err != nil {
		ui.Text(c, err.Error()).FontSize(12).TextColor(t.Danger)
	}
	servers := a.servers
	if servers == nil {
		servers = a.team.Plugins.Known()
	}
	ui.Column(c).Gap(6).Children(func() {
		for _, s := range servers {
			ui.Row(c.Key("mcp-"+s.ID)).Gap(8).AlignItems(ui.Center).Padding(6, 10).Radius(8).Background(t.Surface).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, s.Server.Name).Bold().SingleLine()
					line, color := L("Not connected yet"), t.TextMuted
					switch s.State {
					case "ready":
						line = L("%d tools", len(s.Tools))
					case "failed":
						line, color = s.Err, t.Danger
					case "off":
						line = L("Off")
					}
					ui.Text(c, line).FontSize(11).TextColor(color).SingleLine()
				})
			})
		}
		if len(servers) == 0 {
			ui.Text(c, L("No servers yet.")).FontSize(12).TextColor(t.TextMuted)
		}
	})
	ui.Row(c).Gap(8).Children(func() {
		if a.openFolder != nil && a.mcpPath != "" {
			if ui.Button(c.Key("open-mcp"), L("Open mcp.json")).Clicked() {
				if _, err := os.Stat(a.mcpPath); err != nil {
					os.MkdirAll(filepath.Dir(a.mcpPath), 0o755)
					os.WriteFile(a.mcpPath, []byte(mcpTemplate), 0o644)
				}
				a.openFolder(a.mcpPath)
			}
		}
		label := L("Reconnect")
		if a.checking {
			label = L("Connecting…")
		}
		if ui.Button(c.Key("check-mcp"), label).Disabled(a.checking).Clicked() {
			a.team.Plugins.Close() // read the file again and start fresh
			a.checkServers()
		}
	})
}
