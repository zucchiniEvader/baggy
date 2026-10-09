package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/store"
)

// maxImage is the largest image the composer attaches.
const maxImage = 5 << 20

// atQuery is the @word the draft ends with, and where it starts; ok is false
// when the draft does not end in one.
func atQuery(draft string) (query string, at int, ok bool) {
	i := strings.LastIndexByte(draft, '@')
	if i < 0 {
		return "", 0, false
	}
	if i > 0 && !unicode.IsSpace(rune(draft[i-1])) {
		return "", 0, false
	}
	q := draft[i+1:]
	if strings.ContainsFunc(q, unicode.IsSpace) {
		return "", 0, false
	}
	return q, i, true
}

// atChoices are the bots the @ menu offers for the draft.
func (a *app) atChoices(ch store.Chat, v *chatView) []store.Bot {
	q, _, ok := atQuery(v.draft)
	if !ok {
		return nil
	}
	var out []store.Bot
	for _, b := range a.mentionable(ch) {
		if strings.HasPrefix(strings.ToLower(b.Name), strings.ToLower(q)) {
			out = append(out, b)
		}
	}
	return out
}

// pick puts @Name in the draft for the @word it ends with.
func (v *chatView) pick(b store.Bot) {
	_, at, ok := atQuery(v.draft)
	if !ok {
		return
	}
	v.draft = v.draft[:at] + "@" + b.Name + " "
	v.mentions = append(v.mentions, b.ID)
	v.atOpen, v.atSel = false, 0
	v.focus = true
}

// attach adds an image file to the next message.
func (v *chatView) attach(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return v.attachBytes(b, filepath.Base(path))
}

func (v *chatView) attachBytes(b []byte, name string) error {
	if len(b) > maxImage {
		return errf("%s is larger than 5 MB", name)
	}
	mt := http.DetectContentType(b)
	switch mt {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return errf("%s is not an image (PNG, JPEG, GIF or WebP)", name)
	}
	v.images = append(v.images, agent.Part{Kind: agent.PartImage, MediaType: mt, Data: b})
	return nil
}

func (a *app) composer(c *ui.Context, ch store.Chat, v *chatView) {
	t := c.Theme()
	box := ui.Column(c).Padding(10, 16, 12, 16).Gap(6).BorderWidth(1, 0, 0, 0).BorderColor(t.Border)
	for _, p := range box.DroppedFiles() {
		if err := v.attach(p); err != nil {
			v.notice(err.Error())
		}
	}
	if box.FileDragOver() {
		box.Background(t.SurfaceHover)
	}
	box.Children(func() {
		if q := v.reply; q != nil {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					quoteView(c, L("Replying to %s", a.name(q.Author)), q.Text)
				})
				if ui.Button(c.Key("unreply"), "✕").Label(L("Cancel reply")).Clicked() {
					v.reply = nil
				}
			})
		}
		if n := len(v.images); n > 0 {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, L("%d image(s) attached", n)).FontSize(12).TextColor(t.TextMuted).Grow(1)
				if ui.Button(c.Key("unattach"), L("Remove")).Clicked() {
					v.images = nil
				}
			})
		}
		ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
			placeholder := L("Message %s", a.title(ch))
			if a.busy[ch.ID] && ch.Kind == store.DM {
				placeholder = L("Steer %s", a.title(ch))
			}
			in := ui.TextAreaBase(c.Key("draft-"+ch.ID), &v.draft).Lines(1, composerMax).Placeholder(placeholder).Label(L("Message")).Grow(1).
				Padding(8, 12).Radius(8).Background(t.Surface)
			choices := a.atChoices(ch, v)
			if len(choices) == 0 {
				v.atOpen = false
			} else if v.draft != v.atDraft {
				v.atOpen = true // typing reopens it after Escape
			}
			v.atDraft = v.draft
			composing := in.Composing()
			in.HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind != ui.InputKeyDown || composing {
					return false
				}
				if v.atOpen && len(choices) > 0 {
					switch ev.Key {
					case ui.KeyEnter, ui.KeyTab:
						v.pick(choices[min(v.atIndex(), len(choices)-1)])
						return true
					case ui.KeyDown:
						v.atMove(1, len(choices))
						return true
					case ui.KeyUp:
						v.atMove(-1, len(choices))
						return true
					case ui.KeyEscape:
						v.atOpen = false
						return true
					}
				}
				switch {
				case ev.Key == ui.KeyEnter && ev.Mods == 0:
					a.send() // Shift+Enter starts a line
					return true
				case ev.Key == ui.KeyEnter && ev.Mods == ui.Cmd && a.busy[ch.ID] && ch.Kind == store.DM:
					a.interruptSend()
					return true
				case ev.Key == ui.KeyEscape && v.reply != nil:
					v.reply = nil
					return true
				case ev.Key == ui.KeyV && ev.Mods == ui.Cmd && a.readImage != nil:
					if img := a.readImage(); len(img) > 0 {
						if err := v.attachBytes(img, L("The pasted image")); err != nil {
							v.notice(err.Error())
						}
						return true // an image on the clipboard: no text to paste
					}
				}
				return false
			})
			ui.Popover(c, in, &v.atOpen, func() {
				ui.Column(c).Gap(2).MinWidth(180).Children(func() {
					for i, b := range choices {
						item := ui.Row(c.Key("at-"+b.ID)).Padding(4, 8).Radius(6).Gap(8).Label(b.Name)
						if i == v.atIndex() {
							item.Background(t.Accent).TextColor(t.AccentText) // the popup owns the keyboard
						}
						if item.Clicked() {
							v.pick(b)
						}
						item.Children(func() {
							ui.Text(c, "@"+b.Name).Bold()
							ui.Text(c, firstLine(b.Description)).FontSize(11).SingleLine().MaxWidth(220)
						})
					}
				})
			})
			if v.focus {
				in.Focus()
				v.focus = false
			}
			if a.pickImage != nil {
				if ui.Button(c.Key("attach"), "＋").Label(L("Attach image")).Tooltip(L("Attach an image")).Clicked() {
					for _, p := range a.pickImage() {
						if err := v.attach(p); err != nil {
							v.notice(err.Error())
						}
					}
				}
			}
			label := L("Send")
			empty := strings.TrimSpace(v.draft) == "" && len(v.images) == 0
			if a.busy[ch.ID] && ch.Kind == store.DM {
				label = L("Steer")
				if ui.Button(c.Key("interrupt"), L("Interrupt")).Tooltip(L("Send now, cutting the current step short (Cmd+Enter)")).Disabled(empty).Clicked() {
					a.interruptSend()
				}
			}
			if ui.PrimaryButton(c.Key("send"), label).Tooltip(L("Steer waits for the current step")).Disabled(empty).Clicked() {
				a.send()
			}
		})
	})
}

// atIndex is the @ menu's highlighted choice.
func (v *chatView) atIndex() int { return max(0, v.atSel) }

func (v *chatView) atMove(d, n int) { v.atSel = max(0, min(n-1, v.atSel+d)) }
