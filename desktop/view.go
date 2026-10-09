package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

const (
	maxBubble   = 620
	composerMax = 8
)

func (c *chat) view(ctx *ui.Context) {
	t := ctx.Theme()
	ui.Column(ctx).Fill().Background(t.Background).Children(func() {
		ui.List(ctx, &c.list, len(c.rows), func(i int) { c.drawRow(ctx, i) }).
			Grow(1).Padding(16, 20).Gap(10).Justify(ui.End).Children(func() {
			if len(c.rows) == 0 {
				ui.Text(ctx, "Ask for something.").TextColor(t.TextMuted).Padding(12)
			}
		})
		c.composer(ctx)
	})
}

func (c *chat) drawRow(ctx *ui.Context, i int) {
	t := ctx.Theme()
	r := c.rows[i]
	switch r.kind {
	case rowUser:
		ui.Row(ctx).Justify(ui.End).Children(func() {
			ui.Text(ctx, r.text).Selectable().Background(t.Accent).TextColor(t.AccentText).
				Padding(8, 12).Radius(12).MaxWidth(maxBubble)
		})
	case rowAssistant:
		ui.Column(ctx).Gap(4).MaxWidth(maxBubble).Children(func() {
			if r.thinking != "" {
				ui.Text(ctx, r.thinking).Selectable().Italic().FontSize(12).TextColor(t.TextMuted)
			}
			switch {
			case r.failed:
				ui.Text(ctx, r.text).Selectable().TextColor(t.Danger)
			case r.text != "":
				ui.Text(ctx, r.text).Selectable()
			case r.thinking == "" && !r.stopped:
				ui.Text(ctx, "…").TextColor(t.TextMuted)
			}
			if r.stopped {
				ui.Text(ctx, "Stopped").FontSize(12).TextColor(t.TextMuted)
			}
		})
	case rowTool:
		c.drawTool(ctx, r)
	case rowNotice:
		ui.Text(ctx, r.text).FontSize(12).TextColor(t.TextMuted).Padding(4, 0)
	}
}

func (c *chat) drawTool(ctx *ui.Context, r row) {
	t := ctx.Theme()
	status, color := "running", t.TextMuted
	switch {
	case r.result != nil && r.failed:
		status, color = "failed", t.Danger
	case r.result != nil:
		status, color = "done", t.Success
	}
	ui.Column(ctx).Gap(4).Padding(8, 12).Radius(8).Background(t.Surface).MaxWidth(maxBubble).Children(func() {
		ui.Row(ctx).Gap(8).Children(func() {
			ui.Text(ctx, r.call.Name).Bold().FontSize(12)
			ui.Text(ctx, status).FontSize(12).TextColor(color)
		})
		ui.Text(ctx, oneLine(string(r.call.Args), 200)).Selectable().FontSize(12).TextColor(t.TextMuted)
		if r.result != nil {
			ui.Text(ctx, tail(r.result.Text, 12)).Selectable().FontSize(12)
		}
	})
}

func (c *chat) composer(ctx *ui.Context) {
	t := ctx.Theme()
	ui.Row(ctx).Gap(8).Padding(12, 16).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		placeholder := "Message"
		if c.running {
			placeholder = "Steer the agent"
		}
		in := ui.TextAreaBase(ctx.Key("draft"), &c.draft).Lines(1, composerMax).Placeholder(placeholder).Label(placeholder).Grow(1).
			Padding(8, 12).Radius(8).Background(t.Surface)
		composing := in.Composing()
		in.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEnter && ev.Mods == 0 && !composing {
				c.send() // Shift+Enter starts a line
				return true
			}
			return false
		})
		if c.focus {
			in.Focus()
			c.focus = false
		}
		if c.running {
			if ui.Button(ctx.Key("stop"), "Stop").Clicked() {
				c.stop()
			}
		}
		if ui.PrimaryButton(ctx.Key("send"), sendLabel(c)).Disabled(strings.TrimSpace(c.draft) == "").Clicked() {
			c.send()
		}
	})
}

func sendLabel(c *chat) string {
	if c.running && c.queued > 0 {
		return fmt.Sprintf("Steer (%d queued)", c.queued)
	}
	if c.running {
		return "Steer"
	}
	return "Send"
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
