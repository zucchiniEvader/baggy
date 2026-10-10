package main

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/mcp"
)

// mcpTemplate starts an mcp.json with no servers; Settings adds them.
const mcpTemplate = `{
  "mcpServers": {}
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

var serverKinds = []string{"Command", "Remote (HTTP)"}

// serverForm is a server of mcp.json being edited.
type serverForm struct {
	old                  string // its name in the file; "" for a new one
	stored               mcp.Server
	name, kind           string
	command, args, env   string // args one per line, env KEY=value lines
	cwd, url, headers    string // headers Name: value lines
	description, timeout string
	enabled              bool
	testing, testOK      bool
	test, err            string
}

func serverFormOf(s mcp.Server) *serverForm {
	f := &serverForm{old: s.Name, stored: s, name: s.Name, kind: serverKinds[0], command: s.Command, args: strings.Join(s.Args, "\n"),
		env: pairs(s.Env, "="), cwd: s.Cwd, url: s.URL, headers: pairs(s.Headers, ": "), description: s.Description, enabled: !s.Disabled}
	if s.URL != "" {
		f.kind = serverKinds[1]
	}
	if s.Timeout > 0 {
		f.timeout = strconv.FormatFloat(s.CallTimeout().Seconds(), 'f', -1, 64)
	}
	return f
}

func pairs(m map[string]string, sep string) string {
	var lines []string
	for _, k := range slices.Sorted(maps.Keys(m)) {
		lines = append(lines, k+sep+m[k])
	}
	return strings.Join(lines, "\n")
}

// parsePairs reads KEY=value (or Name: value) lines; blank lines are
// skipped.
func parsePairs(s, sep, what string) (map[string]string, error) {
	var m map[string]string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, sep)
		if !ok || strings.TrimSpace(k) == "" {
			return nil, errf("%s: write each as %s", what, "KEY"+sep+"value")
		}
		if m == nil {
			m = map[string]string{}
		}
		m[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return m, nil
}

// server is the form as an entry of mcp.json.
func (f *serverForm) server() (mcp.Server, error) {
	s := mcp.Server{Name: strings.TrimSpace(f.name), Description: strings.TrimSpace(f.description), Disabled: !f.enabled}
	if f.kind == serverKinds[1] {
		s.Type, s.URL = "http", strings.TrimSpace(f.url)
		h, err := parsePairs(f.headers, ":", L("Headers"))
		if err != nil {
			return s, err
		}
		s.Headers = h
	} else {
		s.Command, s.Cwd = strings.TrimSpace(f.command), strings.TrimSpace(f.cwd)
		for _, line := range strings.Split(f.args, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				s.Args = append(s.Args, line)
			}
		}
		env, err := parsePairs(f.env, "=", L("Environment"))
		if err != nil {
			return s, err
		}
		s.Env = env
	}
	if t := strings.TrimSpace(f.timeout); t != "" {
		secs, err := strconv.ParseFloat(t, 64)
		if err != nil || secs <= 0 {
			return s, errf("The timeout is a number of seconds.")
		}
		s.Timeout = secs
		if secs >= 1000 { // the file reads 1000 and more as milliseconds
			s.Timeout = secs * 1000
		}
	}
	return s, nil
}

// dirty reports whether the form differs from what the file has.
func (f *serverForm) dirty() bool {
	if f.old == "" {
		return true
	}
	g := serverFormOf(f.stored)
	return f.name != g.name || f.kind != g.kind || f.command != g.command || f.args != g.args || f.env != g.env || f.cwd != g.cwd ||
		f.url != g.url || f.headers != g.headers || f.description != g.description || f.timeout != g.timeout || f.enabled != g.enabled
}

// pluginsView is Settings' MCP Servers pane: the servers of mcp.json as a
// list, or one of them as a page that edits it.
func (a *app) pluginsView(c *ui.Context) {
	if a.team.Plugins == nil {
		return
	}
	servers := a.servers
	if servers == nil {
		servers = a.team.Plugins.Known()
		a.checkServers() // how they stand, in a moment
	}
	var chosen *mcp.Status
	for i := range servers {
		if servers[i].Server.Name == a.set.srvSel {
			chosen = &servers[i]
		}
	}
	switch {
	case a.set.srvSel == "new":
		if a.set.srv == nil || a.set.srv.old != "" {
			a.set.srv = &serverForm{kind: serverKinds[0], enabled: true}
		}
	case chosen == nil:
		a.set.srvSel, a.set.srv = "", nil
	case a.set.srv == nil || a.set.srv.old != chosen.Server.Name:
		a.set.srv = serverFormOf(chosen.Server)
	}
	if a.set.srv == nil {
		a.serverList(c, servers)
		return
	}
	f := a.set.srv
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).MaxWidth(620).FillWidth().Margin(0, ui.Auto).Padding(4, 28, 16).Children(func() { a.serverDetail(c, f, chosen) })
	})
	ui.Column(c).Shrink(0).BorderWidth(1, 0, 0, 0).BorderColor(pal(c).border).Children(func() {
		ui.Column(c).MaxWidth(620).FillWidth().Margin(0, ui.Auto).Padding(0, 28, 12).Children(func() { a.serverActions(c, f) })
	})
}

// serverLine is how a server stands, in a few words.
func serverLine(s mcp.Status) (string, bool) {
	switch s.State {
	case "ready":
		line := L("%d tools", len(s.Tools))
		if s.Resources {
			line += L(", resources")
		}
		return line, false
	case "failed":
		return s.Err, true
	case "off":
		return L("Off"), false
	}
	return L("Not connected yet"), false
}

// serverList is the pane's first page: every server as a row that opens it.
func (a *app) serverList(c *ui.Context, servers []mcp.Status) {
	t := c.Theme()
	p := pal(c)
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).MaxWidth(620).FillWidth().Margin(0, ui.Auto).Padding(4, 28, 28).Gap(6).Children(func() {
			ui.Text(c, L("MCP servers give every bot their tools, which it calls from codemode scripts. They are kept in mcp.json, the format Claude Desktop and Cursor use.")).FontSize(12).LineHeight(1.5).TextColor(t.TextMuted).Padding(0, 0, 8)
			if err := a.team.Plugins.FileError(); err != nil {
				errorRow(c, err.Error())
			}
			if len(servers) == 0 {
				ui.Column(c).Radius(cardRadius).Background(p.surface).Border(1, p.border).Padding(32, 16).Gap(12).AlignItems(ui.Center).Children(func() {
					iconTile(c, iconPlug, 40)
					ui.Text(c, L("No servers yet.")).FontSize(12).TextColor(t.TextMuted)
					if smallButton(c.Key("first-server"), L("Add Server…"), primary).Clicked() {
						a.set.srvSel = "new"
					}
				})
			} else {
				a.serverRows(c, servers)
			}
			ui.Row(c).Gap(8).Padding(6, 0, 0).Children(func() {
				if a.openFolder != nil && a.mcpPath != "" {
					if smallButton(c.Key("open-mcp"), L("Open mcp.json"), ghost).Clicked() {
						ensureMCP(a.mcpPath)
						a.openFolder(a.mcpPath)
					}
				}
				ui.Spacer(c)
				label := L("Reconnect")
				if a.checking {
					label = L("Connecting…")
				}
				if smallButton(c.Key("check-mcp"), label, secondary).Clicked() && !a.checking {
					a.team.Plugins.Close() // read the file again and start fresh
					a.checkServers()
				}
				if len(servers) > 0 && smallButton(c.Key("add-server"), L("Add Server…"), secondary).Clicked() {
					a.set.srvSel = "new"
				}
			})
		})
	})
}

func (a *app) serverRows(c *ui.Context, servers []mcp.Status) {
	t := c.Theme()
	p := pal(c)
	box := ui.Column(c.Key("servers")).Radius(cardRadius).Background(p.surface).Border(1, p.border).Clip().
		Focusable().FocusRing(false).Label(L("Servers")).Role(ui.RoleList)
	focused := box.FocusWithin()
	if !focused {
		a.set.srvNav.keyboard = false
	}
	at := -1
	for i, s := range servers {
		if s.Server.Name == a.set.srvHi {
			at = i
		}
	}
	a.set.srvNav.keys(box, at, len(servers), func(i int) { a.set.srvHi = servers[i].Server.Name })
	if box.Shortcut(0, ui.KeyEnter) && a.set.srvHi != "" {
		a.set.srvSel = a.set.srvHi
	}
	var action func()
	box.Children(func() {
		for i, s := range servers {
			name := s.Server.Name
			st := a.set.srvNav.state(name == a.set.srvHi && focused && a.set.srvNav.keyboard, focused)
			row := ui.Row(c.Key("srv-"+s.ID)).Height(52).Padding(0, 12).Gap(12).AlignItems(ui.Center).Label(name).Role(ui.RoleListItem)
			if i > 0 {
				row.BorderWidth(1, 0, 0, 0).BorderColor(p.border)
			}
			styleRow(row, t, st)
			if row.Clicked() {
				a.set.srvHi = name
				action = func() { a.set.srvSel = name }
			}
			row.ContextMenu(func(m *ui.Menu) {
				if m.Item(L("Edit…")).Chosen() {
					action = func() { a.set.srvSel = name }
				}
				turn := L("Turn Off")
				if s.Server.Disabled {
					turn = L("Turn On")
				}
				if m.Item(turn).Chosen() {
					action = func() {
						sv := s.Server
						sv.Disabled = !sv.Disabled
						a.saveServerFile(name, sv)
					}
				}
				m.Separator()
				if m.Item(L("Delete")).Chosen() {
					action = func() { a.askDeleteServer(name) }
				}
			})
			row.Children(func() {
				iconTile(c, iconPlug, 30)
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, name).FontSize(13).FontWeight(600).SingleLine().MinWidth(0)
					line, failed := serverLine(s)
					sub := t.TextMuted
					switch {
					case st == rowKeyboard:
						sub = t.AccentText
					case failed:
						sub = p.red
					}
					ui.Text(c, line).FontSize(11).TextColor(sub).SingleLine().MinWidth(0).Tooltip(line)
				})
				switch s.State {
				case "ready":
					ui.Icon(c, iconCheck).FontSize(12).TextColor(p.greenBar).Shrink(0)
				case "failed":
					ui.Icon(c, iconXCircle).FontSize(12).TextColor(p.redBar).Shrink(0)
				}
				ic := ui.Icon(c, iconChevronRight).FontSize(12).Shrink(0)
				if st != rowKeyboard {
					ic.TextColor(t.TextMuted)
				}
			})
		}
	})
	if action != nil {
		action()
	}
}

// serverDetail is the chosen server: how it is reached, then its options.
func (a *app) serverDetail(c *ui.Context, f *serverForm, st *mcp.Status) {
	t := c.Theme()
	ui.Column(c).Gap(6).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Padding(0, 0, 6).Children(func() {
			iconTile(c, iconPlug, 36)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				name := f.name
				if strings.TrimSpace(name) == "" {
					name = L("New Server")
				}
				ui.Text(c, name).FontSize(15).Bold().SingleLine().MinWidth(0)
				line, failed := L(f.kind), false
				if st != nil {
					line, failed = serverLine(*st)
				}
				color := t.TextMuted
				if failed {
					color = pal(c).red
				}
				ui.Text(c, line).FontSize(12).TextColor(color).SingleLine().MinWidth(0).Tooltip(line)
			})
		})
		sectionTitle(c, L("Connection"))
		kinds := []string{L(serverKinds[0]), L(serverKinds[1])}
		kind := L(f.kind)
		rows := []func(){
			func() {
				fieldRow(c, L("Name"), L("Tools are named after it: tools.<name>__<tool>."), func() { textField(c.Key("s-name"), &f.name, "github") })
			},
			func() {
				fieldRow(c, L("Kind"), "", func() {
					popup(c, &kind, kinds)
					for i, k := range kinds {
						if k == kind {
							f.kind = serverKinds[i]
						}
					}
				})
			},
		}
		if f.kind == serverKinds[1] {
			rows = append(rows,
				func() {
					fieldRow(c, L("URL"), L("A Streamable HTTP endpoint."), func() { textField(c.Key("s-url"), &f.url, "https://example.com/mcp") })
				},
				func() {
					fieldRow(c, L("Headers"), L("One per line, as Authorization: Bearer ${TOKEN}; ${NAME} reads the environment."), func() {
						textArea(c.Key("s-headers"), &f.headers, "Authorization: Bearer …", 2, 6)
					})
				})
		} else {
			rows = append(rows,
				func() {
					fieldRow(c, L("Command"), L("Found on your login shell's PATH."), func() { textField(c.Key("s-cmd"), &f.command, "npx") })
				},
				func() {
					fieldRow(c, L("Arguments"), L("One per line."), func() {
						textArea(c.Key("s-args"), &f.args, "-y\n@modelcontextprotocol/server-filesystem", 2, 8)
					})
				},
				func() {
					fieldRow(c, L("Environment"), L("One per line, as KEY=value."), func() { textArea(c.Key("s-env"), &f.env, "GITHUB_TOKEN=…", 2, 6) })
				},
				func() {
					fieldRow(c, L("Folder"), L("Where it starts; empty is your home folder."), func() { textField(c.Key("s-cwd"), &f.cwd, "~/Projects") })
				})
		}
		card(c, rows...)
		sectionTitle(c, L("Options"))
		card(c,
			func() { switchRow(c, L("On"), L("Bots can use its tools."), &f.enabled) },
			func() {
				fieldRow(c, L("Description"), L("Tells the bots what it is for."), func() { textArea(c.Key("s-desc"), &f.description, "", 1, 4) })
			},
			func() {
				fieldRow(c, L("Timeout"), L("Seconds a call may take; empty is ten minutes."), func() { textField(c.Key("s-timeout"), &f.timeout, "600").MaxWidth(120) })
			},
		)
		if st != nil && len(st.Tools) > 0 {
			sectionTitle(c, L("Tools"))
			card(c, func() {
				ui.Column(c).Padding(8, 14).Gap(4).Children(func() {
					for _, tl := range st.Tools {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Text(c, tl.Name).Font(monoFont).FontSize(12).Shrink(0)
							ui.Text(c, firstLine(tl.Description)).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
						})
					}
				})
			})
		}
		if f.err != "" {
			ui.Box(c).Padding(8, 0, 0).Children(func() { errorRow(c, f.err) })
		}
	})
}

// serverActions are the page's buttons, kept in view under it.
func (a *app) serverActions(c *ui.Context, f *serverForm) {
	t := c.Theme()
	ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Padding(12, 0, 0).Children(func() {
		if f.old != "" && smallButton(c.Key("s-delete"), L("Delete"), danger).Clicked() {
			a.askDeleteServer(f.old)
		}
		label := L("Test Connection")
		if f.testing {
			label = L("Testing…")
		}
		if smallButton(c.Key("s-test"), label, secondary).Clicked() && !f.testing {
			a.testServer(f)
		}
		switch {
		case f.testing:
			ui.Spinner(c).Size(12, 12)
		case f.test != "" && f.testOK:
			ui.Icon(c, iconCheck).FontSize(12).TextColor(pal(c).greenBar).Shrink(0)
			ui.Text(c, f.test).FontSize(12).TextColor(pal(c).green).SingleLine().Shrink(1).MinWidth(0)
		case f.test != "":
			ui.Icon(c, iconXCircle).FontSize(12).TextColor(pal(c).redBar).Shrink(0)
			ui.Text(c, f.test).FontSize(12).TextColor(pal(c).red).SingleLine().Shrink(1).MinWidth(0).Tooltip(f.test)
		}
		ui.Spacer(c)
		if f.old == "" {
			if button(c, L("Cancel")).Clicked() {
				a.set.srvSel, a.set.srv = "", nil
			}
			if primaryButton(c.Key("s-save"), L("Create"), strings.TrimSpace(f.name) == "").Clicked() {
				a.saveServer(f)
			}
			return
		}
		if !f.dirty() {
			ui.Text(c, L("Saved")).FontSize(12).TextColor(t.TextMuted)
			return
		}
		if button(c, L("Revert")).Clicked() {
			a.set.srv = serverFormOf(f.stored)
		}
		if primaryButton(c.Key("s-save"), L("Save"), false).Clicked() {
			a.saveServer(f)
		}
	})
}

// saveServer writes the form to mcp.json and shows the server's page.
func (a *app) saveServer(f *serverForm) {
	s, err := f.server()
	if err == nil {
		err = mcp.SaveServer(a.mcpPath, f.old, s)
	}
	if err != nil {
		f.err = err.Error()
		return
	}
	f.err = ""
	a.serversChanged()
	a.set.srvSel, a.set.srvHi, a.set.srv = s.Name, s.Name, nil
	a.servers = a.team.Plugins.Known()
}

// saveServerFile writes one server as it is, as turning it on or off.
func (a *app) saveServerFile(name string, s mcp.Server) {
	a.do(mcp.SaveServer(a.mcpPath, name, s))
	a.serversChanged()
}

// serversChanged reconnects to what mcp.json now says, in the background.
func (a *app) serversChanged() {
	a.servers = nil
	a.checkServers()
}

func (a *app) askDeleteServer(name string) {
	a.set.confirm = &confirmation{open: true, title: L("Delete %s?", name), message: L("Bots can no longer use its tools."), action: L("Delete"), run: func() {
		a.do(mcp.DeleteServer(a.mcpPath, name))
		a.serversChanged()
		a.set.srvSel, a.set.srv = "", nil
	}}
}

// testServer connects to the form's server, in the background, and says
// how it went.
func (a *app) testServer(f *serverForm) {
	s, err := f.server()
	if err != nil {
		f.test, f.testOK = err.Error(), false
		return
	}
	s.Disabled = false
	f.testing, f.test = true, ""
	redraw := a.redrawSettings
	go func() {
		ok, msg := checkServer(s)
		a.post(func() {
			f.testing, f.testOK, f.test = false, ok, msg
			if redraw != nil {
				redraw()
			}
		})
	}()
}

// checkServer starts or reaches a server and lists its tools, a minute at
// most.
func checkServer(s mcp.Server) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cl, err := mcp.Connect(ctx, s)
	if err != nil {
		return false, oneLine(err.Error(), 200)
	}
	defer cl.Close()
	tools, err := cl.Tools(ctx)
	if err != nil {
		return false, oneLine(err.Error(), 200)
	}
	msg := L("Connected: %d tools.", len(tools))
	if cl.HasResources {
		msg = L("Connected: %d tools and resources.", len(tools))
	}
	return true, msg
}
