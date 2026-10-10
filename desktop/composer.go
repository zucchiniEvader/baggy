package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/team"
)

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

// attach adds a file to the next message.
func (v *chatView) attach(path string) error {
	name := filepath.Base(path)
	info, err := os.Stat(path)
	switch {
	case err != nil:
		return err
	case info.IsDir():
		return errf("%s is a folder: attach its files", name)
	case info.Size() > team.MaxFile:
		return errf("%s is larger than %d MB", name, team.MaxFile>>20)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return v.attachBytes(b, name)
}

func (v *chatView) attachBytes(b []byte, name string) error {
	if len(b) > team.MaxFile {
		return errf("%s is larger than %d MB", name, team.MaxFile>>20)
	}
	v.files = append(v.files, team.Attachment{Name: name, Data: b})
	return nil
}

// fileChip is an attached file: its icon, name and size, and remove when
// it is still a draft's.
func fileChip(c *ui.Context, name string, size int64, remove *bool) ui.Element {
	t := c.Theme()
	chip := ui.Row(c).Height(26).Padding(0, 4, 0, 8).Gap(6).Radius(rowRadius).Background(pal(c).pill).AlignItems(ui.Center).Label(name)
	chip.Children(func() {
		ic := iconFile
		if isPicture(name) {
			ic = iconImage
		}
		ui.Icon(c, ic).FontSize(12).TextColor(t.TextMuted).Shrink(0)
		ui.Text(c, name).FontSize(12).SingleLine().Shrink(1).MinWidth(0).MaxWidth(220)
		ui.Text(c, sizeText(size)).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(0)
		if remove != nil {
			if iconButton(c.Key("x"), iconClose, L("Remove")).Size(20, 20).Clicked() {
				*remove = true
			}
		} else {
			ui.Box(c).Width(4)
		}
	})
	return chip
}

func isPicture(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".heic", ".heif":
		return true
	}
	return false
}

// sizeText is a file's size as Finder says it.
func sizeText(n int64) string {
	switch {
	case n < 1000:
		return L("%d bytes", n)
	case n < 1000*1000:
		return fmt.Sprintf("%.0f KB", float64(n)/1000)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1000*1000))
}

func (a *app) composer(c *ui.Context, ch store.Chat, v *chatView) {
	t := c.Theme()
	p := pal(c)
	busy := a.busy[ch.ID]
	empty := strings.TrimSpace(v.draft) == "" && len(v.files) == 0
	ui.Column(c).Shrink(0).MaxWidth(contentWidth).FillWidth().Margin(0, ui.Auto).Padding(0, 28, 16).Gap(8).Children(func() {
		card := ui.Column(c).Radius(12).Background(p.surface).Border(1, p.border).Padding(10, 8, 8, 12).Gap(6)
		for _, path := range card.DroppedFiles() {
			if err := v.attach(path); err != nil {
				v.notice(err.Error())
			}
		}
		if card.FileDragOver() {
			focusRing(card, t)
		}
		card.Children(func() {
			if q := v.reply; q != nil {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconReply).FontSize(12).TextColor(t.TextMuted).Shrink(0)
					ui.Column(c).Grow(1).MinWidth(0).Children(func() { quoteView(c, L("Replying to %s", a.name(q.Author)), q.Text) })
					if iconButton(c.Key("unreply"), iconClose, L("Cancel reply")).Clicked() {
						v.reply = nil
					}
				})
			}
			if len(v.files) > 0 {
				drop := -1
				ui.Row(c).Wrap().Gap(6).Children(func() {
					for i, f := range v.files {
						removed := false
						fileChip(c.Key(fmt.Sprint("file", i)), f.Name, int64(len(f.Data)), &removed)
						if removed {
							drop = i
						}
					}
				})
				if drop >= 0 {
					v.files = append(v.files[:drop:drop], v.files[drop+1:]...)
				}
			}
			placeholder := L("Message %s", a.title(ch))
			if busy && ch.Kind == store.DM {
				placeholder = L("Steer %s", a.title(ch))
			}
			in := ui.TextAreaBase(c.Key("draft-"+ch.ID), &v.draft).Placeholder(placeholder).Label(L("Message")).
				FontSize(13).LineHeight(1.5).Lines(2, 10).Padding(0, 4, 0, 0)
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
				case ev.Key == ui.KeyEnter && ev.Mods == ui.Cmd && busy && ch.Kind == store.DM:
					a.interruptSend()
					return true
				case ev.Key == ui.KeyEscape && v.reply != nil:
					v.reply = nil
					return true
				case ev.Key == ui.KeyV && ev.Mods == ui.Cmd && a.readImage != nil:
					if img := a.readImage(); len(img) > 0 {
						if err := v.attachBytes(img, pastedName(v.files)); err != nil {
							v.notice(err.Error())
						}
						return true // an image on the clipboard: no text to paste
					}
				}
				return false
			})
			ui.PopoverBase(c, in, &v.atOpen, func(panel ui.Element) {
				panel.Margin(6, 0, 0, 0).Padding(4).Radius(cardRadius).Background(p.surface).Border(1, p.border).MinWidth(220)
				ui.Column(c).Gap(1).Children(func() {
					for i, b := range choices {
						st := rowPlain
						if i == v.atIndex() {
							st = rowKeyboard // the popup owns the keyboard
						}
						item := listRow(c.Key("at-"+b.ID), st).Label(b.Name)
						if item.Clicked() {
							v.pick(b)
						}
						item.Children(func() {
							a.avatar(c, b.ID, 18)
							ui.Text(c, "@"+b.Name).FontSize(13).FontWeight(600).Shrink(0)
							ui.Text(c, firstLine(b.Description)).FontSize(11).SingleLine().Shrink(1).MinWidth(0).MaxWidth(220)
						})
					}
				})
			})
			if v.focus {
				in.Focus()
				v.focus = false
			}
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				if a.pickFiles != nil {
					if iconButton(c.Key("attach"), iconPaperclip, L("Attach files")).Clicked() {
						a.pickFiles(func(paths []string) {
							for _, path := range paths {
								if err := v.attach(path); err != nil {
									v.notice(err.Error())
								}
							}
							v.focus = true
						})
					}
				}
				hint := L("↩ Send · ⇧↩ New line")
				if busy && ch.Kind == store.DM {
					hint = L("↩ Steer · ⌘↩ Interrupt")
				}
				ui.Text(c, hint).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
				ui.Spacer(c)
				if busy && ch.Kind == store.DM && !empty {
					if smallButton(c.Key("interrupt"), L("Interrupt"), secondary).Tooltip(L("Send now, cutting the current step short (Cmd+Enter)")).Clicked() {
						a.interruptSend()
					}
				}
				switch {
				case busy && empty:
					if roundButton(c.Key("stop"), iconStop, L("Stop"), secondary, false).Clicked() {
						a.stop()
					}
				default:
					label := L("Send")
					if busy && ch.Kind == store.DM {
						label = L("Steer")
					}
					if roundButton(c.Key("send"), iconArrowUp, label, primary, empty).Clicked() {
						a.send()
					}
				}
			})
		})
	})
}

// pastedName names a pasted image, apart from those already attached.
func pastedName(fs []team.Attachment) string {
	n := 0
	for _, f := range fs {
		if strings.HasPrefix(f.Name, "pasted-image") {
			n++
		}
	}
	if n == 0 {
		return "pasted-image.png"
	}
	return fmt.Sprintf("pasted-image-%d.png", n+1)
}

// atIndex is the @ menu's highlighted choice.
func (v *chatView) atIndex() int { return max(0, v.atSel) }

func (v *chatView) atMove(d, n int) { v.atSel = max(0, min(n-1, v.atSel+d)) }
