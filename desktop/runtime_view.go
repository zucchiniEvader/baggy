package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/baggy/remote"
	"rsc.io/qr"
)

// runtimeFile keeps where the bots run: beside the database, as the window
// reads it before it opens one.
const runtimeFile = "runtime.json"

type runtimeConfig struct {
	// Link is a server's baggy://pair link; "" runs the bots here.
	Link string `json:"link,omitempty"`
}

// runtimeLink is the server the window shows, "" for this computer.
func runtimeLink(dir string) string {
	var cfg runtimeConfig
	b, err := os.ReadFile(filepath.Join(dir, runtimeFile))
	if err != nil {
		return ""
	}
	json.Unmarshal(b, &cfg)
	return strings.TrimSpace(cfg.Link)
}

func saveRuntimeLink(dir, link string) error {
	b, _ := json.MarshalIndent(runtimeConfig{Link: link}, "", "  ")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, runtimeFile), append(b, '\n'), 0o600)
}

// runtimeForm is Settings' Runtime section being changed.
type runtimeForm struct {
	server   bool   // a server, not this computer
	link     string // the link pasted
	checking bool
	err      string
	saved    bool // a change waits for a restart
}

// serverName is the server's name, from the link, else its address.
func (a *app) serverName() string {
	if a.remote == nil {
		return ""
	}
	if n := a.remote.c.Name; n != "" {
		return n
	}
	return strings.TrimPrefix(a.remote.c.Base, "http://")
}

// runtimeSection is Settings › General › Runtime: whether the bots run
// here or on a server of the tailnet, the window showing them either way.
func (a *app) runtimeSection(c *ui.Context) {
	if a.dataDir == "" {
		return
	}
	t := c.Theme()
	f := a.set.runtime
	if f == nil {
		f = &runtimeForm{server: a.remote != nil, link: a.remoteLink}
		a.set.runtime = f
	}
	here, there := L("This computer"), L("A server on Tailscale")
	sectionTitle(c, L("Runtime"))
	card(c, func() {
		settingRow(c, L("Bots run on"), L("On a server, its bots, chats and settings show here, and this computer keeps none of its own while connected."), func() {
			choice := here
			if f.server {
				choice = there
			}
			popup(c, &choice, []string{here, there}).Width(200)
			if (choice == there) != f.server {
				f.server, f.err = choice == there, ""
				if !f.server && a.remote != nil {
					f.link = ""
					a.applyRuntime(f, "")
				}
			}
		})
	}, func() {
		if !f.server {
			return
		}
		ui.Column(c).Padding(10, 14).Gap(8).Children(func() {
			ui.Text(c, L("Server link")).FontSize(13)
			ui.Text(c, L("baggyd prints it as it starts on the server. Both computers must be on the same tailnet.")).FontSize(11).LineHeight(1.4).TextColor(t.TextMuted)
			textField(c, &f.link, "baggy://pair?host=…").Font(monoFont)
			if a.remote != nil && strings.TrimSpace(f.link) == a.remoteLink {
				if why := a.remote.offline(); why != "" {
					ui.Text(c, L("Cannot reach %s: %s", a.serverName(), why)).FontSize(12).TextColor(pal(c).red)
				} else {
					ui.Text(c, L("Connected to %s.", a.serverName())).FontSize(12).TextColor(t.TextMuted)
				}
			}
			if f.err != "" {
				ui.Text(c, f.err).FontSize(12).TextColor(pal(c).red)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				label := L("Connect")
				if f.checking {
					label = L("Connecting…")
				}
				same := a.remote != nil && strings.TrimSpace(f.link) == a.remoteLink
				if primaryButton(c.Key("rt-connect"), label, f.checking || same || strings.TrimSpace(f.link) == "").Clicked() {
					a.checkRuntime(f)
				}
			})
		})
	})
	if f.saved {
		ui.Row(c).Padding(6, 2).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, L("Restart Baggy to switch.")).FontSize(12).TextColor(t.TextMuted).Grow(1).MinWidth(0)
			if a.relaunch != nil && smallButton(c.Key("rt-restart"), L("Restart Now"), secondary).Clicked() {
				a.relaunch()
			}
		})
	}
}

// checkRuntime asks the linked server who it is, in the background, and
// keeps the link when it answers.
func (a *app) checkRuntime(f *runtimeForm) {
	link := strings.TrimSpace(f.link)
	cl, err := remote.ParseLink(link)
	if err != nil {
		f.err = L("That is not a server link: %s", err.Error())
		return
	}
	f.checking, f.err = true, ""
	redraw := a.redrawSettings
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := cl.Hello(ctx)
		a.post(func() {
			f.checking = false
			if err != nil {
				f.err = L("The server did not answer: %s", err.Error())
			} else {
				a.applyRuntime(f, link)
			}
			if redraw != nil {
				redraw()
			}
		})
	}()
}

func (a *app) applyRuntime(f *runtimeForm, link string) {
	if err := saveRuntimeLink(a.dataDir, link); err != nil {
		f.err = err.Error()
		return
	}
	f.saved = link != a.remoteLink
}

// remotePhonePane is the Phone pane while the bots run on a server: the
// server answers phones, with the link this window took.
func (a *app) remotePhonePane(c *ui.Context) {
	t := c.Theme()
	p := pal(c)
	sectionTitle(c, L("Pair"))
	card(c, func() {
		ui.Column(c).Padding(20, 14).Gap(12).AlignItems(ui.Center).Children(func() {
			ph := a.phone
			if ph.qrFor != a.remoteLink {
				if code, err := qr.Encode(a.remoteLink, qr.M); err == nil {
					ph.qr, ph.qrFor = ui.NewBitmap(qrImage(code, 8)), a.remoteLink
				}
			}
			if ph.qr != nil {
				ui.Column(c).Padding(4).Radius(cardRadius).Border(1, p.border).Background(ui.Hex("#ffffff")).Children(func() {
					ui.Image(c, ph.qr).Size(184, 184).Fit(ui.Contain).Label(L("Pairing code"))
				})
			}
			ui.Text(c, L("Phones pair with %s, where the bots run. Scan with Baggy on your phone.", a.serverName())).FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, strings.TrimPrefix(a.remote.c.Base, "http://")).Font(monoFont).FontSize(12).TextColor(t.TextMuted)
		})
	})
}

// serverStatus is the sidebar's foot while the bots run on a server: its
// name, or that it cannot be reached.
func (a *app) serverStatus(c *ui.Context) {
	if a.remote == nil {
		return
	}
	if why := a.remote.offline(); why != "" {
		ui.Text(c, L("Cannot reach %s", a.serverName())).FontSize(11).TextColor(pal(c).red).SingleLine().Shrink(1).MinWidth(0).Tooltip(why)
		return
	}
	ui.Text(c, a.serverName()).FontSize(11).TextColor(c.Theme().TextMuted).SingleLine().Shrink(1).MinWidth(0).Tooltip(L("Bots run on %s", a.serverName()))
}
