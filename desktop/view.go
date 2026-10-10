package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/images"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/team"
)

// root is the window: the chats beside the chat shown, and the sheets.
func (a *app) root(c *ui.Context) {
	p := applyTheme(c)
	// On macOS the window shows the sidebar's material where nothing is
	// drawn; elsewhere the sidebar's color is drawn there.
	c.Root().Background(a.sidebarBg(c))
	a.shortcuts(c)
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		a.sidebarPane(c)
		a.sidebarResizer(c)
		ui.Column(c).Grow(1).MinWidth(0).Background(p.appBg).Children(func() { a.main(c) })
		if ch := a.chat(a.sel); ch != nil {
			a.inspectorSlide(c, *ch)
		}
	})
	a.sheets(c)
}

func (a *app) main(c *ui.Context) {
	ch := a.chat(a.sel)
	if ch == nil {
		a.toolbar(c, nil)
		a.noChat(c)
		return
	}
	v := a.view(ch.ID)
	a.toolbar(c, ch)
	if !a.hasProvider() && len(v.rows) == 0 {
		a.setupView(c)
		return
	}
	if len(v.rows) == 0 && !a.busy[ch.ID] {
		a.welcome(c, *ch, v)
	} else {
		a.transcript(c, *ch, v)
	}
	a.approvalBar(c, *ch)
	a.composer(c, *ch, v)
}

// toolbar is the bar along the top of the chat, which drags the window.
func (a *app) toolbar(c *ui.Context, ch *store.Chat) {
	t := c.Theme()
	bar := c.TitleBar()
	padL := float32(16)
	if !a.sidebarShown {
		padL = max(bar.Left+8, 16)
	}
	ui.Row(c).Height(titleBarHeight).Shrink(0).Padding(0, 12+bar.Right, 0, padL).Gap(6).AlignItems(ui.Center).DragWindow().Children(func() {
		if !a.sidebarShown {
			a.sidebarToggle(c)
			a.newMenu(c)
		}
		if ch == nil {
			ui.Spacer(c)
			return
		}
		ui.Text(c, a.title(*ch)).FontSize(13).FontWeight(600).SingleLine().Shrink(1).MinWidth(0)
		// The status is the first to go: whole, never as a lone "…".
		if s := a.status(*ch); s != "" {
			ui.Text(c, s).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(1000).MinWidth(0)
		}
		ui.Spacer(c)
		if ch.Kind == store.DM {
			a.modelPill(c, ch.Owner)
		}
		a.tasksButton(c, *ch)
		tip := L("Show Info")
		if a.inspector {
			tip = L("Hide Info")
		}
		if iconButton(c, iconPanelRight, tip).Clicked() {
			a.inspector = !a.inspector
			a.layoutChanged()
		}
	})
}

// status is the line beside a chat's title: who is working, or a group's
// members.
func (a *app) status(ch store.Chat) string {
	if v := a.views[ch.ID]; v != nil && v.working != "" && a.busy[ch.ID] {
		if v.retrying != "" {
			return L("Retrying: %s", oneLine(v.retrying, 80))
		}
		return L("%s is working…", a.name(v.working))
	}
	if ch.Kind == store.Group {
		var names []string
		for _, id := range ch.Members {
			names = append(names, a.name(id))
		}
		return strings.Join(names, ", ")
	}
	return ""
}

// modelPill names the model a bot runs on and opens a menu of its
// provider's models.
func (a *app) modelPill(c *ui.Context, botID string) {
	b, ok := a.byID[botID]
	if !ok {
		return
	}
	cfg, err := teamConfig(a, b)
	if err != nil {
		return
	}
	var models []string
	if p, err := a.st.Provider(cfg.ID); err == nil {
		models = p.Models
	}
	var action func()
	pill(c, iconSparkle, cfg.Model).Label(L("Model: %s", cfg.Model)).Menu(func(m *ui.Menu) {
		for _, name := range models {
			if m.Item(name).Checked(name == cfg.Model).Chosen() && name != cfg.Model {
				name := name
				action = func() {
					b.Model = name
					a.do(a.st.UpdateBot(b))
				}
			}
		}
		if len(models) > 0 {
			m.Separator()
		}
		if m.Item(L("Edit Bot…")).Chosen() {
			action = func() { a.editBot(botID) }
		}
	})
	if action != nil {
		action()
	}
}

// noChat is the window without a chat: a mark, a line, and New Bot.
func (a *app) noChat(c *ui.Context) {
	ui.Column(c).Grow(1).Center().Gap(16).Children(func() {
		iconTile(c, iconBot, 40)
		ui.Text(c, L("No chat yet.")).FontSize(13).TextColor(c.Theme().TextMuted)
		if primaryButton(c.Key("first-bot"), L("New Bot…"), false).Clicked() {
			a.newBot()
		}
	})
}

// welcome is a chat with nothing in it yet: its bot, and a few things to
// try.
func (a *app) welcome(c *ui.Context, ch store.Chat, v *chatView) {
	t := c.Theme()
	p := pal(c)
	title, sub := a.title(ch), L("Write to the group. @Name asks a member first.")
	suggestions := []string{L("Introduce yourselves in one line each."), L("Plan a weekend trip together.")}
	if ch.Kind == store.DM {
		b := a.byID[ch.Owner]
		title, sub = L("Hi, I'm %s", b.Name), firstLine(b.Description)
		suggestions = []string{L("What can you do?"), L("Look at what is in your workspace."), L("Every weekday at 8, give me a short brief.")}
	}
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Grow(1).Center().Padding(48, 28).Children(func() {
			ui.Column(c).MaxWidth(420).FillWidth().Gap(16).AlignItems(ui.Center).Children(func() {
				a.chatAvatar(c, ch, 56, p.appBg)
				ui.Column(c).Gap(4).AlignItems(ui.Center).Children(func() {
					ui.Text(c, title).FontSize(17).Bold()
					if sub != "" {
						ui.Text(c, sub).FontSize(12).TextColor(t.TextMuted).TextAlign(ui.Center)
					}
				})
				ui.Column(c).FillWidth().Gap(1).Children(func() {
					for i, s := range suggestions {
						row := listRow(c.Key(i), rowPlain).Label(s)
						if row.Clicked() {
							v.draft = s
							a.send()
						}
						row.Children(func() {
							ui.Icon(c, iconSparkle).FontSize(12).TextColor(t.TextMuted).Shrink(0)
							ui.Text(c, s).FontSize(13).SingleLine().Grow(1).MinWidth(0)
						})
					}
				})
			})
		})
	})
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return oneLine(s, 100)
}

// shortThought is the length under which a reply's thinking goes unshown:
// a passing note is not worth a line of its own.
const shortThought = 160

// item is what one entry of the transcript list draws: a row, or a run of
// tool rows drawn as one card.
type item struct {
	rows  []int // indexes into the view's rows
	first bool  // a bot's row that starts its turn: beside its avatar, under its name
}

func items(rows []row) []item {
	var out []item
	for i := 0; i < len(rows); {
		first := false
		if rows[i].kind == rowAssistant || rows[i].kind == rowTool {
			first = true
			for k := i - 1; k >= 0; k-- {
				if rows[k].kind == rowNotice {
					continue
				}
				first = rows[k].kind == rowUser || rows[k].author != rows[i].author
				break
			}
		}
		if rows[i].kind == rowTool {
			j := i
			for j < len(rows) && rows[j].kind == rowTool && rows[j].author == rows[i].author {
				j++
			}
			idx := make([]int, 0, j-i)
			for k := i; k < j; k++ {
				idx = append(idx, k)
			}
			out = append(out, item{rows: idx, first: first})
			i = j
			continue
		}
		out = append(out, item{rows: []int{i}, first: first})
		i++
	}
	return out
}

func (a *app) transcript(c *ui.Context, ch store.Chat, v *chatView) {
	rows := v.rows
	its := items(rows)
	v.list.Key = func(i int) any {
		r := rows[its[i].rows[0]]
		if r.seq != 0 {
			return fmt.Sprintf("%d-%d", r.seq, its[i].rows[0])
		}
		return fmt.Sprintf("live-%s-%d", r.turn, its[i].rows[0])
	}
	ui.List(c, &v.list, len(its), func(i int) {
		ui.Column(c).MaxWidth(contentWidth).FillWidth().Margin(0, ui.Auto).Padding(0, 28).Children(func() {
			it := its[i]
			r := rows[it.rows[0]]
			if r.kind != rowAssistant && r.kind != rowTool {
				a.drawRow(c, ch, v, r, false)
				return
			}
			// A bot's rows: its name starts each of its turns, and a reply's
			// time shows on the right while the pointer is over it.
			col := ui.Column(c).Gap(8)
			hovered := col.Hovered()
			col.Children(func() {
				if it.first {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Text(c, a.name(r.author)).FontSize(12).FontWeight(600).TextColor(c.Theme().TextMuted).SingleLine().Shrink(1).MinWidth(0)
						ui.Spacer(c)
						if r.kind == rowAssistant {
							msgTime(c, r.at, hovered)
						}
					})
				}
				if r.kind == rowTool {
					a.toolGroup(c, ch, v, it.rows)
					return
				}
				ui.Box(c).Children(func() {
					a.drawRow(c, ch, v, r, it.first)
					if !it.first && hovered && r.at != 0 {
						ui.Box(c).Absolute().Top(0).Right(0).Padding(0, 0, 0, 8).Background(pal(c).appBg).Children(func() { msgTime(c, r.at, true) })
					}
				})
			})
		})
	}).Grow(1).MinHeight(0).Padding(12, 0, 28).Gap(16).Children(func() {
		if a.busy[ch.ID] && v.working != "" && !a.streaming(v) {
			ui.Column(c).MaxWidth(contentWidth).FillWidth().Margin(0, ui.Auto).Padding(0, 28).Children(func() {
				thinking(c, L("%s is thinking…", a.name(v.working)))
			})
		}
	})
}

// streaming reports whether a reply is streaming into a row.
func (a *app) streaming(v *chatView) bool {
	for _, i := range v.stream {
		if i < len(v.rows) && (v.rows[i].text != "" || v.rows[i].thinking != "" || len(v.rows[i].searches) > 0) {
			return true
		}
	}
	return false
}

func (a *app) drawRow(c *ui.Context, ch store.Chat, v *chatView, r row, first bool) {
	t := c.Theme()
	switch r.kind {
	case rowUser:
		col := ui.Column(c).AlignItems(ui.End).Gap(4).Padding(0, 0, 0, 80)
		hovered := col.Hovered()
		col.Children(func() {
			if r.from != "" {
				ui.Text(c, L("From %s, via message_bot", a.name(r.from))).FontSize(11).TextColor(t.TextMuted)
			}
			if r.quote != nil {
				quoteView(c, a.name(r.quote.Author), r.quote.Text)
			}
			if r.text != "" || r.images > 0 {
				ui.Row(c).AlignItems(ui.End).Children(func() {
					// The time stands left of the bubble, out of the flow, so
					// showing it moves nothing.
					ui.Box(c).Width(0).Shrink(0).Children(func() {
						if hovered && r.at != 0 {
							ui.Row(c).Absolute().Right(0).Bottom(8).Width(140).Justify(ui.End).Padding(0, 8, 0, 0).Children(func() { msgTime(c, r.at, true) })
						}
					})
					a.userBubble(c, r)
				})
			}
			if len(r.files) > 0 {
				ui.Column(c).Gap(6).AlignItems(ui.End).MinWidth(0).Children(func() {
					for _, f := range r.files {
						a.fileView(c, f)
					}
				})
			}
		})
	case rowAssistant:
		col := ui.Column(c).Gap(8)
		a.rowMenu(col, r)
		col.Children(func() {
			for _, s := range r.searches {
				a.searchLine(c, s)
			}
			if len([]rune(strings.TrimSpace(r.thinking))) >= shortThought && a.prefs.showThinking {
				a.reasoning(c, v, r)
			}
			switch {
			case r.failed:
				a.failedReply(c, ch, v, r)
			case r.text != "":
				markdown(c, r.text)
			case !r.stopped && r.seq == 0 && len(r.searches) == 0 && r.thinking == "":
				thinking(c, L("%s is thinking…", a.name(r.author)))
			}
			if r.stopped {
				ui.Text(c, L("Stopped")).FontSize(12).TextColor(t.TextMuted)
			}
		})
	case rowNotice:
		ui.Text(c, r.text).Selectable().FontSize(11).TextColor(t.TextMuted).TextAlign(ui.Center).FillWidth()
	}
}

// userBubble is the words of a user's message on a neutral bubble.
func (a *app) userBubble(c *ui.Context, r row) {
	t := c.Theme()
	bubble := ui.Column(c).Padding(8, 12).Radius(cardRadius).Background(pal(c).userBubble).Gap(4).MinWidth(0)
	a.rowMenu(bubble, r)
	bubble.Children(func() {
		if r.text != "" {
			ui.Text(c, r.text).FontSize(13).LineHeight(1.55).Selectable()
		}
		if r.images > 0 {
			ui.Row(c).Gap(6).AlignItems(ui.Center).TextColor(t.TextMuted).Children(func() {
				ui.Icon(c, iconPaperclip).FontSize(12)
				ui.Text(c, L("%d image(s) attached", r.images)).FontSize(12)
			})
		}
	})
}

// failedReply is a reply that failed: what went wrong in words a person
// reads, and what to do about it. Only the chat's last row offers Retry.
func (a *app) failedReply(c *ui.Context, ch store.Chat, v *chatView, r row) {
	text := r.text
	noModel := strings.Contains(text, team.ErrNoProvider.Error())
	if noModel {
		text = L("No model is set up yet. Add a provider in Settings, then retry.")
	}
	last := len(v.rows) > 0 && v.rows[len(v.rows)-1].seq == r.seq && r.seq != 0
	ui.Column(c).Gap(8).Children(func() {
		errorRow(c, text)
		if !last || a.busy[ch.ID] {
			return
		}
		ui.Row(c).Gap(6).Children(func() {
			if noModel || !a.hasProvider() {
				if smallButton(c.Key("open-settings"), L("Open Settings"), secondary).Clicked() {
					a.openSettings(paneModels)
				}
			}
			if smallButton(c.Key("retry"), L("Retry"), secondary).Disabled(!a.hasProvider()).Clicked() {
				a.do(a.team.Continue(ch.ID))
			}
		})
	})
}

// errorRow says what went wrong in red words on a gray row, not a red block.
func errorRow(c *ui.Context, text string) {
	p := pal(c)
	ui.Row(c).Gap(8).Padding(8, 12).Radius(cardRadius).Background(p.pill).AlignItems(ui.Start).Children(func() {
		ui.Icon(c, iconAlert).FontSize(iconSize).TextColor(p.redBar).Shrink(0).Margin(2, 0, 0)
		ui.Text(c, text).FontSize(13).LineHeight(1.45).TextColor(p.red).Grow(1).MinWidth(0).Selectable()
	})
}

func (a *app) searchLine(c *ui.Context, s search) {
	t := c.Theme()
	color := t.TextMuted
	if s.err != "" {
		color = pal(c).red
	}
	ui.Row(c).Gap(6).AlignItems(ui.Center).TextColor(color).Children(func() {
		switch {
		case !s.done:
			ui.Spinner(c).Size(12, 12).Shrink(0)
		case s.tool == "web_fetch":
			ui.Icon(c, iconFile).FontSize(12).Shrink(0)
		default:
			ui.Icon(c, iconGlobe).FontSize(12).Shrink(0)
		}
		ui.Text(c, searchText(s)).FontSize(12).SingleLine().Shrink(1).MinWidth(0).Selectable()
	})
}

// reasoning is a reply's thinking, closed under a line that opens it.
func (a *app) reasoning(c *ui.Context, v *chatView, r row) {
	t := c.Theme()
	key := fmt.Sprintf("think-%d-%s", r.seq, r.turn)
	open := v.open[key]
	ui.Column(c).Gap(6).Children(func() {
		head := ui.ButtonBase(c.Key(key)).Height(22).Gap(6).AlignItems(ui.Center).AlignSelf(ui.Start).Label(L("Thinking")).TextColor(t.TextMuted).FocusRing(false)
		if head.Clicked() {
			v.open[key] = !open
		}
		head.Children(func() {
			disclosure(c, open)
			ui.Icon(c, iconBrain).FontSize(12)
			ui.Text(c, L("Thinking")).FontSize(12).FontWeight(600)
		})
		if open {
			ui.Box(c).Margin(0, 0, 0, 5).Padding(0, 0, 0, 12).BorderWidth(0, 0, 0, 1).BorderColor(pal(c).border).Children(func() {
				ui.Text(c, strings.TrimSpace(r.thinking)).FontSize(12).LineHeight(1.55).TextColor(t.TextMuted).Selectable()
			})
		}
	})
}

// rowMenu gives a message Reply and the editing commands.
func (a *app) rowMenu(e ui.Element, r row) {
	e.Selectable().ContextMenu(func(m *ui.Menu) {
		if m.Item(L("Reply")).Disabled(r.seq == 0).Chosen() {
			a.replyTo(r)
		}
		m.Separator()
		m.EditItems()
	})
}

func quoteView(c *ui.Context, who, text string) {
	t := c.Theme()
	ui.Column(c).Padding(0, 0, 0, 10).BorderWidth(0, 0, 0, 2).BorderColor(pal(c).pill).MinWidth(0).Children(func() {
		ui.Text(c, who).FontSize(11).FontWeight(600).TextColor(t.TextMuted)
		ui.Text(c, oneLine(text, 160)).FontSize(12).TextColor(t.TextMuted).SingleLine().MinWidth(0)
	})
}

// toolGroup is a card of a run of tool calls, each a line that opens on its
// arguments and result.
func (a *app) toolGroup(c *ui.Context, ch store.Chat, v *chatView, idx []int) {
	t := c.Theme()
	p := pal(c)
	ui.Column(c).Radius(cardRadius).Background(p.surface).Border(1, p.border).Padding(4).Gap(1).Clip().Children(func() {
		for _, i := range idx {
			r := v.rows[i]
			key := "tool-" + r.call.CallID
			ui.Column(c.Key(key)).Children(func() {
				open := v.open[key]
				line := listRow(c, rowPlain).Label(toolLabel(r.call.Name))
				if line.Clicked() {
					v.open[key] = !open
				}
				line.Children(func() {
					disclosure(c, open)
					ui.Icon(c, toolIcon(r.call.Name)).FontSize(iconSize).TextColor(t.TextMuted).Shrink(0)
					ui.Text(c, toolLabel(r.call.Name)).FontSize(13).Shrink(0)
					if s := toolSummary(r.call); s != "" {
						ui.Text(c, s).Font(monoFont).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
					}
					ui.Spacer(c)
					switch {
					case r.pending:
						ui.Text(c, L("waiting for approval")).FontSize(11).TextColor(p.orange).Shrink(0)
					case r.result != nil && r.failed:
						ui.Icon(c, iconXCircle).FontSize(12).TextColor(p.redBar).Shrink(0)
					case r.result != nil:
						ui.Icon(c, iconCheck).FontSize(12).TextColor(p.greenBar).Shrink(0)
					case a.busy[ch.ID]:
						ui.Spinner(c).Size(12, 12).Shrink(0)
					default:
						ui.Icon(c, iconCircle).FontSize(12).TextColor(t.TextMuted).Shrink(0)
					}
				})
				live := ""
				if r.result == nil && !r.pending {
					if sess := a.sessionOf(ch.ID, r.call); sess != nil {
						live = strings.TrimSpace(sess.Last(8))
					} else if r.partial != "" {
						live = lastLines(r.partial, 8)
					}
				}
				if r.pending || open || live != "" {
					ui.Column(c).Padding(4, 8, 8, 26).Gap(6).Children(func() {
						if open {
							if code, ok := scriptOf(r.call); ok {
								ui.Text(c, L("Script")).FontSize(11).FontWeight(600).TextColor(t.TextMuted)
								codeBlock(c, code)
							} else {
								ui.Text(c, L("Arguments")).FontSize(11).FontWeight(600).TextColor(t.TextMuted)
								codeBlock(c, prettyJSON(r.call.Args))
							}
						}
						if live != "" {
							codeBlock(c, live)
						}
						if r.pending {
							a.approvalButtons(c, ch.ID, r.call.CallID)
						}
						if open && r.result != nil {
							ui.Text(c, L("Result")).FontSize(11).FontWeight(600).TextColor(t.TextMuted)
							codeBlock(c, tail(r.result.Text, 60))
						}
					})
				}
			})
		}
	})
}

// toolSummary is a call's arguments in a few words: the command, the path,
// the query.
func toolSummary(call agent.Part) string {
	if code, ok := scriptOf(call); ok {
		return oneLine(code, 120)
	}
	var a map[string]any
	if json.Unmarshal(call.Args, &a) != nil {
		return ""
	}
	for _, k := range []string{"command", "path", "pattern", "query", "bot", "text", "action", "name"} {
		if s, ok := a[k].(string); ok && s != "" {
			return oneLine(s, 120)
		}
	}
	return ""
}

func prettyJSON(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(b)
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// tail keeps the last n lines.
func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return fmt.Sprintf("… %d lines above\n%s", len(lines)-n, strings.Join(lines[len(lines)-n:], "\n"))
}

// scriptOf is a codemode call's script.
func scriptOf(call agent.Part) (string, bool) {
	if call.Name != "codemode" {
		return "", false
	}
	var a struct{ Code string }
	if json.Unmarshal(call.Args, &a) != nil || a.Code == "" {
		return "", false
	}
	return strings.TrimSpace(a.Code), true
}

// thumbSide is the longest side of an attached picture in the transcript.
const thumbSide = 240

// fileView is an attached file in the transcript: a picture's thumbnail,
// or a chip; a click opens the file.
func (a *app) fileView(c *ui.Context, f store.File) {
	var el ui.Element
	if b := a.thumb(f); b != nil {
		w, h := b.Size()
		k := min(1, float32(thumbSide)/float32(max(w, h)))
		el = ui.Box(c.Key(f.ID)).Size(float32(w)*k, float32(h)*k).Radius(cardRadius).Clip().Label(f.Name)
		el.Children(func() { ui.Image(c, b).Fill().Fit(ui.Cover) })
	} else {
		el = fileChip(c.Key(f.ID), f.Name, f.Size, nil)
	}
	el.Cursor(ui.CursorPointer).Tooltip(strings.TrimSpace(f.Name + "\n" + f.Note))
	path := a.st.FilePath(f)
	if el.Clicked() && a.openFolder != nil {
		a.openFolder(path)
	}
	el.ContextMenu(func(m *ui.Menu) {
		if m.Item(L("Open")).Chosen() && a.openFolder != nil {
			a.openFolder(path)
		}
		if m.Item(L("Show in Finder")).Chosen() && a.revealFile != nil {
			a.revealFile(path)
		}
	})
}

// thumb is an attached picture's thumbnail, made off the frame the first
// time it shows at twice its size, for a Retina screen; nil until then,
// and for a file that is not a picture.
func (a *app) thumb(f store.File) *ui.Bitmap {
	if !f.Image {
		return nil
	}
	return a.bitmap(a.st.FilePath(f), func(data []byte) (*ui.Bitmap, error) {
		img, err := images.Thumbnail(data, f.Name, 2*thumbSide)
		if err != nil {
			return nil, err
		}
		return ui.NewBitmap(img), nil
	})
}

// msgTime is when a message came, as the conversation shows it while the
// pointer is over it: the time today, else the day with it; the whole date
// in its tooltip.
func msgTime(c *ui.Context, at int64, shown bool) {
	if at == 0 || !shown {
		return
	}
	t := time.UnixMilli(at)
	ui.Text(c, clock(time.Now(), t)).FontSize(11).TextColor(c.Theme().TextMuted).SingleLine().Shrink(0).Tooltip(t.Format("2006-01-02 15:04:05"))
}

func clock(now, t time.Time) string {
	hm := t.Format("15:04")
	switch days := daysBetween(t, now); {
	case days <= 0:
		return hm
	case days == 1:
		return L("Yesterday") + " " + hm
	case t.Year() == now.Year():
		return fmt.Sprintf("%d/%d %s", t.Month(), t.Day(), hm)
	}
	return fmt.Sprintf("%d/%d/%d %s", t.Year(), t.Month(), t.Day(), hm)
}

// lastLines is the end of a text, at most n lines.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
