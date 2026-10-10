package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// The app's controls. Every text field, button, row, pill and pop-up comes
// from here, so they look the same everywhere: see AGENTS.md's design
// before adding one.

// Fields and standard buttons are 30 high; the icon buttons of bars,
// compact, 26.
const (
	fieldHeight   = 30
	compactHeight = 26
	iconSize      = 14
)

// focusRing marks the control with the keyboard focus.
func focusRing(e ui.Element, t *ui.Theme) { e.Shadow(0, 0, 0, 3, t.Focus.Alpha(0.45)) }

// fieldBox draws a text field around what fn builds in it: filled with the
// pill's gray, borderless, as macOS's search fields.
func fieldBox(c *ui.Context, fn func(box ui.Element)) ui.Element {
	box := ui.Row(c).Height(fieldHeight).Padding(0, 8).Gap(6).Radius(rowRadius).Background(pal(c).pill).AlignItems(ui.Center)
	box.Children(func() { fn(box) })
	return box
}

// fieldInput is the input of a field, which rings the box while it has the
// focus.
func fieldInput(c *ui.Context, box ui.Element, value *string, placeholder string) ui.Element {
	in := ui.TextInputBase(c, value).Placeholder(placeholder).Label(placeholder).FontSize(13).Grow(1).MinWidth(0)
	if in.Focused() {
		focusRing(box, c.Theme())
	}
	return in
}

// textField is a single-line field; it returns the input, for Password,
// AutoFocus, Submitted and the like.
func textField(c *ui.Context, value *string, placeholder string) ui.Element {
	var in ui.Element
	fieldBox(c, func(box ui.Element) { in = fieldInput(c, box, value, placeholder) })
	return in
}

// textArea is a multi-line field, as a bot's description.
func textArea(c *ui.Context, value *string, placeholder string, minLines, maxLines int) ui.Element {
	var in ui.Element
	box := ui.Column(c).Padding(6, 8).Radius(rowRadius).Background(pal(c).pill)
	box.Children(func() {
		in = ui.TextAreaBase(c, value).Lines(minLines, maxLines).Placeholder(placeholder).Label(placeholder).FontSize(13).LineHeight(1.45)
		if in.Focused() {
			focusRing(box, c.Theme())
		}
	})
	return in
}

// searchField filters a list: a magnifying glass, the text, and a button
// clearing it, as Escape does. focus, when set, gives it the focus.
func searchField(c *ui.Context, query *string, placeholder string, focus *bool) ui.Element {
	t := c.Theme()
	var in ui.Element
	fieldBox(c, func(box ui.Element) {
		ui.Icon(c, iconSearch).FontSize(13).TextColor(t.TextMuted)
		in = fieldInput(c, box, query, placeholder)
		if *focus {
			in.Focus()
			*focus = false
		}
		if *query != "" {
			clear := ui.ButtonBase(c).Size(16, 16).Radius(8).Center().Background(t.TextMuted.Alpha(0.45)).Label(L("Clear")).FocusRing(false)
			clear.Children(func() { ui.Icon(c, iconClose).FontSize(10).TextColor(t.Background) })
			if clear.Clicked() || in.Shortcut(0, ui.KeyEscape) {
				*query = ""
			}
		}
	})
	return in
}

// Buttons come in three kinds: primary (the system's accent, for the
// action a dialog is for), secondary (the pill's gray), ghost (no fill
// until the pointer is over it: the icon buttons of bars). Disabled, a
// primary or secondary button is gray with a muted label, never the accent
// faded.
type buttonKind uint8

const (
	secondary buttonKind = iota
	primary
	ghost
	danger // a secondary button whose label is red, for a delete
)

func styleButton(b ui.Element, t *ui.Theme, kind buttonKind, disabled bool) {
	p := paletteFor(t)
	switch {
	case disabled && kind != ghost:
		b.Background(p.pill).TextColor(t.TextMuted)
	case disabled:
		b.TextColor(t.TextMuted.Alpha(0.5))
	case kind == primary:
		bg := t.Accent
		if b.Pressed() {
			bg = t.AccentPressed
		} else if b.Hovered() {
			bg = t.AccentHover
		}
		b.Background(bg).TextColor(t.AccentText)
	case kind == secondary || kind == danger:
		bg := p.pill
		if b.Pressed() {
			bg = p.pressed.Over(p.pill)
		} else if b.Hovered() {
			bg = p.hover.Over(p.pill)
		}
		b.Background(bg).TextColor(t.Text)
		if kind == danger {
			b.TextColor(p.red)
		}
	default:
		b.TextColor(t.TextMuted)
		if b.Pressed() {
			b.Background(p.pressed).TextColor(t.Text)
		} else if b.Hovered() {
			b.Background(p.hover).TextColor(t.Text)
		}
	}
}

// textButton is a button with a label, fieldHeight high.
func textButton(c *ui.Context, label string, kind buttonKind, disabled bool) ui.Element {
	b := ui.ButtonBase(c).Height(fieldHeight).Padding(0, 14).Radius(rowRadius).Center().Label(label).Disabled(disabled).Shrink(0)
	styleButton(b, c.Theme(), kind, disabled)
	b.Children(func() { ui.Text(c, label).FontSize(13).FontWeight(600).SingleLine() })
	return b
}

// button is a secondary button, as a dialog's Cancel.
func button(c *ui.Context, label string) ui.Element { return textButton(c, label, secondary, false) }

// primaryButton is the button of the action a dialog is for, last in its
// row.
func primaryButton(c *ui.Context, label string, disabled bool) ui.Element {
	return textButton(c, label, primary, disabled)
}

// smallButton is a compact secondary button, as on a card.
func smallButton(c *ui.Context, label string, kind buttonKind) ui.Element {
	b := ui.ButtonBase(c).Height(compactHeight).Padding(0, 10).Radius(rowRadius).Center().Label(label).Shrink(0)
	styleButton(b, c.Theme(), kind, false)
	b.Children(func() { ui.Text(c, label).FontSize(12).FontWeight(600).SingleLine() })
	return b
}

// iconButton is a compact ghost button showing an icon alone, as in a title
// bar.
func iconButton(c *ui.Context, svg *ui.SVG, tip string) ui.Element {
	b := ui.ButtonBase(c).Size(compactHeight, compactHeight).Radius(rowRadius).Center().Label(tip).Tooltip(tip).Shrink(0)
	styleButton(b, c.Theme(), ghost, false)
	b.Children(func() { ui.Icon(c, svg).FontSize(iconSize) })
	return b
}

// roundButton is a round icon button, as the composer's send and stop.
func roundButton(c *ui.Context, svg *ui.SVG, tip string, kind buttonKind, disabled bool) ui.Element {
	b := ui.ButtonBase(c).Size(compactHeight, compactHeight).Radius(compactHeight / 2).Center().Label(tip).Tooltip(tip).Disabled(disabled).Shrink(0)
	styleButton(b, c.Theme(), kind, disabled)
	b.Children(func() { ui.Icon(c, svg).FontSize(iconSize) })
	return b
}

// rowState is how a row of a list is chosen.
type rowState uint8

const (
	rowPlain rowState = iota
	// rowChosen is chosen with the pointer, or in a list without the
	// focus: the neutral gray.
	rowChosen
	// rowKeyboard is chosen by the keys while its list has the focus: the
	// accent. Only the keyboard's choice takes it, so that clicking from one
	// row to the next, which moves the focus, does not flash it.
	rowKeyboard
)

// styleRow colors a row for its state and the pointer.
func styleRow(r ui.Element, t *ui.Theme, st rowState) {
	p := paletteFor(t)
	switch {
	case st == rowKeyboard:
		r.Background(t.Accent).TextColor(t.AccentText)
	case st == rowChosen || r.Pressed():
		r.Background(p.selected)
	case r.Hovered():
		r.Background(p.hover)
	}
}

// listRow is a button row, rowHeight high. Give it children.
func listRow(c *ui.Context, st rowState) ui.Element {
	r := ui.ButtonBase(c).Height(rowHeight).Padding(0, 8).Gap(6).Radius(rowRadius).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false)
	styleRow(r, c.Theme(), st)
	return r
}

// pill is a small label in a capsule that opens a menu, as the model's
// name.
func pill(c *ui.Context, svg *ui.SVG, label string) ui.Element {
	t := c.Theme()
	p := pal(c)
	b := ui.ButtonBase(c).Height(compactHeight-4).Gap(5).Padding(0, 9).Radius((compactHeight - 4) / 2).AlignItems(ui.Center).
		Background(p.pill).TextColor(t.TextMuted).Shrink(1).MinWidth(0).MaxWidth(240).FocusRing(false)
	if b.Hovered() {
		b.Background(p.hover.Over(p.pill)).TextColor(t.Text)
	}
	b.Children(func() {
		if svg != nil {
			ui.Icon(c, svg).FontSize(12).Shrink(0)
		}
		ui.Text(c, label).FontSize(12).SingleLine().Shrink(1).MinWidth(0)
		ui.Icon(c, iconChevrons).FontSize(11).Shrink(0)
	})
	return b
}

// popup is a field-sized button showing the choice of options that opens
// the native menu of them, as an NSPopUpButton: the one way the app offers
// a choice from a list.
func popup(c *ui.Context, value *string, options []string) ui.Element {
	t := c.Theme()
	p := pal(c)
	b := ui.ButtonBase(c).Height(fieldHeight).Padding(0, 8, 0, 10).Gap(6).Radius(rowRadius).AlignItems(ui.Center).Background(p.pill).Label(*value).MinWidth(0)
	if b.Hovered() {
		b.Background(p.hover.Over(p.pill))
	}
	b.Children(func() {
		ui.Text(c, *value).FontSize(13).SingleLine().Grow(1).MinWidth(0)
		ui.Icon(c, iconChevrons).FontSize(11).TextColor(t.TextMuted).Shrink(0)
	})
	b.Menu(func(m *ui.Menu) {
		for _, o := range options {
			if m.Item(o).Checked(o == *value).Chosen() {
				*value = o
			}
		}
	})
	return b
}

// badge is a count in a capsule: monospaced, bold, 10.
func badge(c *ui.Context, text string) ui.Element {
	p := pal(c)
	return ui.Text(c, text).Font(monoFont).FontSize(10).Bold().TextColor(p.muted).Padding(1, 6).Radius(8).Background(p.pill).Shrink(0)
}

// thinking shows that a bot is working, in a slow pulse.
func thinking(c *ui.Context, label string) {
	e := ui.Text(c, label).FontSize(13).TextColor(c.Theme().TextMuted)
	e.Opacity(0.45 + 0.55*e.Loop("pulse", 1600*time.Millisecond, ui.Bounce(ui.EaseInOut)))
}

// disclosure is the arrow of what opens: down when open, right when closed.
func disclosure(c *ui.Context, open bool) {
	ic := ui.Icon(c, iconChevronDown).FontSize(12).TextColor(c.Theme().TextMuted).Shrink(0)
	angle := float32(-90)
	if open {
		angle = 0
	}
	ic.Rotate(ic.AnimateWith("rot", angle, 150*time.Millisecond, ui.EaseInOut))
}

// sectionTitle is a muted caption over a group, as in the inspector.
func sectionTitle(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(11).FontWeight(600).TextColor(c.Theme().TextMuted).Padding(10, 0, 4)
}

// dialogTitle is a dialog's title over a muted line.
func dialogTitle(c *ui.Context, title, subtitle string) {
	ui.Column(c).Gap(4).Children(func() {
		ui.Text(c, title).FontSize(17).Bold()
		if subtitle != "" {
			ui.Text(c, subtitle).FontSize(12).LineHeight(1.5).TextColor(c.Theme().TextMuted)
		}
	})
}

// dialogButtons is a dialog's Cancel and its primary action, bottom right.
// It reports whether the action was clicked.
func dialogButtons(c *ui.Context, open *bool, action string, disabled bool) bool {
	clicked := false
	ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
		if button(c, L("Cancel")).Clicked() {
			*open = false
		}
		clicked = primaryButton(c, action, disabled).Clicked()
	})
	return clicked
}

func codeBlock(c *ui.Context, s string) {
	ui.Box(c).Padding(8, 10).Radius(rowRadius).Background(pal(c).codeBg).MinWidth(0).Children(func() {
		ui.Text(c, s).Font(monoFont).FontSize(12).LineHeight(1.45).Selectable()
	})
}

// iconTile is an icon in a gray rounded square: a bot's look, an empty
// state's mark.
func iconTile(c *ui.Context, svg *ui.SVG, size float32) ui.Element {
	return ui.Box(c).Size(size, size).Radius(size / 4).Center().Background(pal(c).pill).Shrink(0).Children(func() {
		ui.Icon(c, svg).FontSize(size * 0.5).TextColor(c.Theme().TextMuted)
	})
}
