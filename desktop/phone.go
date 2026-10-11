package main

import (
	"fmt"
	"image"
	"image/draw"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/baggy/remote"
	"github.com/zucchiniEvader/baggy/store"
	"rsc.io/qr"
)

// phone is the phone app's way in: the remote server, listening while the
// Phone pane's switch is on, behind a token the pairing code carries.
type phone struct {
	st   *store.Store
	srv  *remote.Server
	on   bool
	port int
	err  string // why the server could not start
	host string // the address the code holds

	qr    *ui.Bitmap
	qrFor string // the URL qr shows
}

func newPhone(st *store.Store) *phone {
	name, _ := os.Hostname()
	name = strings.TrimSuffix(name, ".local")
	ph := &phone{st: st, port: remote.DefaultPort, on: st.Setting("pref.phone") == "1"}
	if p, err := strconv.Atoi(st.Setting("phone.port")); err == nil && p > 0 {
		ph.port = p
	}
	ph.srv = &remote.Server{Store: st, Token: st.Setting("phone.token"), Name: name}
	return ph
}

// start listens when the switch is on.
func (ph *phone) start() {
	ph.err = ""
	if !ph.on {
		return
	}
	if ph.srv.Token == "" {
		ph.srv.Token = remote.NewToken()
		ph.st.SetSetting("phone.token", ph.srv.Token)
	}
	if err := ph.srv.Start(fmt.Sprintf(":%d", ph.port)); err != nil {
		ph.err = err.Error()
		log.Print("phone: ", err)
	}
}

func (ph *phone) stop() { ph.srv.Stop() }

func (ph *phone) setOn(on bool) {
	ph.on = on
	ph.st.SetSetting("pref.phone", map[bool]string{true: "1", false: "0"}[on])
	ph.stop()
	ph.start()
}

// reset makes a new token: phones paired before are let go and must scan
// the new code.
func (ph *phone) reset() {
	ph.stop()
	ph.srv.Token = ""
	ph.st.SetSetting("phone.token", "")
	ph.start()
}

// code is the pairing code for the chosen address, made again when the
// URL changes.
func (ph *phone) code(addrs []string) *ui.Bitmap {
	if ph.host == "" || !slices.Contains(addrs, ph.host) {
		ph.host = ""
		if len(addrs) > 0 {
			ph.host = addrs[0]
		}
	}
	if ph.host == "" {
		return nil
	}
	u := remote.PairURL(ph.host, ph.port, ph.srv.Token, ph.srv.Name)
	if u != ph.qrFor {
		code, err := qr.Encode(u, qr.M)
		if err != nil {
			return nil
		}
		ph.qr, ph.qrFor = ui.NewBitmap(qrImage(code, 8)), u
	}
	return ph.qr
}

// qrImage draws code with scale pixels a module and the 4-module quiet zone.
func qrImage(code *qr.Code, scale int) image.Image {
	side := (code.Size + 8) * scale
	img := image.NewGray(image.Rect(0, 0, side, side))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				draw.Draw(img, image.Rect((x+4)*scale, (y+4)*scale, (x+5)*scale, (y+5)*scale), image.Black, image.Point{}, draw.Src)
			}
		}
	}
	return img
}

func (a *app) phonePane(c *ui.Context) {
	t := c.Theme()
	p := pal(c)
	ph := a.phone
	sectionTitle(c, L("Connection"))
	card(c, func() {
		on := ph.on
		if switchRow(c, L("Connect a phone"), L("Baggy on your iPhone reaches this computer over the local network while the app is running. Away from home, put both on Tailscale or a similar network."), &on) {
			ph.setOn(on)
		}
	})
	if !ph.on {
		return
	}
	if ph.err != "" {
		ui.Text(c, L("Could not listen on port %d: %s", ph.port, ph.err)).FontSize(12).TextColor(p.red).Padding(4, 2)
		return
	}
	addrs := remote.Addresses()
	sectionTitle(c, L("Pair"))
	card(c, func() {
		ui.Column(c).Padding(20, 14).Gap(12).AlignItems(ui.Center).Children(func() {
			b := ph.code(addrs)
			if b == nil {
				ui.Text(c, L("This computer is on no network.")).FontSize(12).TextColor(t.TextMuted)
				return
			}
			// A code reads dark on light, in either appearance.
			ui.Column(c).Padding(4).Radius(cardRadius).Border(1, p.border).Background(ui.Hex("#ffffff")).Children(func() {
				ui.Image(c, b).Size(184, 184).Fit(ui.Contain).Label(L("Pairing code"))
			})
			ui.Text(c, L("Scan with Baggy on your phone.")).FontSize(12).TextColor(t.TextMuted)
			if len(addrs) > 1 {
				choice := ph.host
				popup(c, &choice, addrs).Width(180)
				ph.host = choice
			} else {
				ui.Text(c, fmt.Sprintf("%s:%d", ph.host, ph.port)).Font(monoFont).FontSize(12).TextColor(t.TextMuted)
			}
		})
	}, func() {
		settingRow(c, L("Reset pairing"), L("Phones paired before are disconnected and have to scan the new code."), func() {
			if button(c, L("Reset")).Clicked() {
				ph.reset()
			}
		})
	})
}
