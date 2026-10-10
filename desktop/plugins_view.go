package main

import (
	"context"

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
	ui.Text(c, L("Servers in mcp.json (the format Claude Desktop and Cursor use) give every bot their tools, which it calls from codemode scripts.")).FontSize(12).LineHeight(1.5).TextColor(t.TextMuted)
	if err := a.team.Plugins.FileError(); err != nil {
		errorRow(c, err.Error())
	}
	servers := a.servers
	if servers == nil {
		servers = a.team.Plugins.Known()
	}
	ui.Column(c).Radius(cardRadius).Background(pal(c).surface).Border(1, pal(c).border).Padding(4).Gap(1).Children(func() {
		for _, s := range servers {
			ui.Row(c.Key("mcp-"+s.ID)).Height(40).Gap(8).AlignItems(ui.Center).Padding(0, 8).Radius(rowRadius).Children(func() {
				ui.Icon(c, iconPlug).FontSize(iconSize).TextColor(t.TextMuted).Shrink(0)
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, s.Server.Name).FontSize(13).FontWeight(600).SingleLine().MinWidth(0)
					line, color := L("Not connected yet"), t.TextMuted
					switch s.State {
					case "ready":
						line = L("%d tools", len(s.Tools))
					case "failed":
						line, color = s.Err, pal(c).red
					case "off":
						line = L("Off")
					}
					ui.Text(c, line).FontSize(11).TextColor(color).SingleLine().MinWidth(0)
				})
				switch s.State {
				case "ready":
					ui.Icon(c, iconCheck).FontSize(12).TextColor(pal(c).greenBar).Shrink(0)
				case "failed":
					ui.Icon(c, iconXCircle).FontSize(12).TextColor(pal(c).redBar).Shrink(0)
				}
			})
		}
		if len(servers) == 0 {
			ui.Text(c, L("No servers yet.")).FontSize(12).TextColor(t.TextMuted).Padding(8, 10)
		}
	})
	ui.Row(c).Gap(8).Children(func() {
		if a.openFolder != nil && a.mcpPath != "" {
			if smallButton(c.Key("open-mcp"), L("Open mcp.json"), secondary).Clicked() {
				ensureMCP(a.mcpPath)
				a.openFolder(a.mcpPath)
			}
		}
		label := L("Reconnect")
		if a.checking {
			label = L("Connecting…")
		}
		if smallButton(c.Key("check-mcp"), label, secondary).Clicked() && !a.checking {
			a.team.Plugins.Close() // read the file again and start fresh
			a.checkServers()
		}
	})
}
