package main

import (
	"runtime"

	"github.com/egoist/mygo/ui"
)

// palette is the app's colors in the light or the dark, as AGENTS.md lists
// them: a warm gray window behind white surfaces, states drawn by a
// neutral gray over whatever is under them, and a few colors with a fixed
// meaning.
type palette struct {
	appBg      ui.Color // the window, behind the chat
	surface    ui.Color // cards, the composer
	header     ui.Color
	sidebar    ui.Color // where there is no vibrancy
	text       ui.Color
	muted      ui.Color
	border     ui.Color
	codeBg     ui.Color
	userBubble ui.Color
	pill       ui.Color // pills, field fills
	hover      ui.Color
	pressed    ui.Color
	selected   ui.Color // chosen with the pointer, or without the focus

	red, redBar     ui.Color // errors, deleting
	green, greenBar ui.Color // added, done
	orange          ui.Color // references, types, warnings
	teal            ui.Color // tests
}

// gray is the neutral gray of states, at alpha a, which reads the same
// over any background, light or dark.
func gray(a float32) ui.Color { return ui.RGBA(127, 127, 127, a) }

var (
	lightPalette = palette{
		appBg:      ui.Hex("#f8f8f6"),
		surface:    ui.Hex("#ffffff"),
		header:     ui.Hex("#ffffff"),
		sidebar:    ui.Hex("#ececea"),
		text:       ui.Hex("#111111"),
		muted:      ui.RGBA(17, 17, 17, 0.48),
		border:     ui.RGBA(17, 17, 17, 0.10),
		codeBg:     ui.Hex("#f0f0ee"),
		userBubble: ui.Hex("#ececea"),
		pill:       gray(0.11),
		hover:      gray(0.08),
		pressed:    gray(0.16),
		selected:   gray(0.20),
		red:        ui.Hex("#b8312f"),
		redBar:     ui.Hex("#ff2e3f"),
		green:      ui.Hex("#1f7a44"),
		greenBar:   ui.Hex("#0dbe4e"),
		orange:     ui.Hex("#c56e0e"),
		teal:       ui.Hex("#13867a"),
	}
	darkPalette = palette{
		appBg:      ui.Hex("#141414"),
		surface:    ui.Hex("#1c1c1c"),
		header:     ui.Hex("#232323"),
		sidebar:    ui.Hex("#1f1f1f"),
		text:       ui.Hex("#c8c8c8"),
		muted:      ui.RGBA(230, 230, 230, 0.48),
		border:     ui.RGBA(230, 230, 230, 0.10),
		codeBg:     ui.Hex("#202020"),
		userBubble: ui.Hex("#262626"),
		pill:       gray(0.16),
		hover:      gray(0.08),
		pressed:    gray(0.16),
		selected:   gray(0.20),
		red:        ui.Hex("#ee7e7e"),
		redBar:     ui.Hex("#ff6762"),
		green:      ui.Hex("#6fd094"),
		greenBar:   ui.Hex("#5ecc71"),
		orange:     ui.Hex("#eb9a3d"),
		teal:       ui.Hex("#4ec9b0"),
	}
)

func paletteFor(t *ui.Theme) *palette {
	if t.Dark {
		return &darkPalette
	}
	return &lightPalette
}

// pal is the frame's palette.
func pal(c *ui.Context) *palette { return paletteFor(c.Theme()) }

// applyTheme makes the frame's theme the palette's, so that what MyGo
// draws, as dialogs, menus and alerts, matches the rest. The accent stays
// the system's.
func applyTheme(c *ui.Context) *palette {
	t := *c.Theme()
	p := paletteFor(&t)
	t.Background = p.appBg
	t.Surface = p.surface
	t.SurfaceHover = p.hover.Over(p.surface)
	t.SurfacePressed = p.pressed.Over(p.surface)
	t.Border = p.border
	t.Text = p.text
	t.TextMuted = p.muted
	t.Danger = p.red
	t.Success = p.green
	t.Warning = p.orange
	t.Radius = rowRadius
	t.FontSize = 13
	c.SetTheme(&t)
	return p
}

const (
	// titleBarHeight centers the bars' controls on the traffic lights.
	titleBarHeight = 40
	rowHeight      = 28
	rowRadius      = 6
	cardRadius     = 10
	// contentWidth is the widest the conversation reads comfortably.
	contentWidth = 760
	monoFont     = "SF Mono, Menlo, monospace"

	sidebarDefault  = 260
	sidebarMin      = 200
	sidebarMax      = 420
	sidebarCollapse = 120
	sidebarPad      = 8
	inspectorWidth  = 280
)

// sidebarBg is the sidebar's background: none on macOS, where the window's
// vibrancy shows through, else a gray a step from the chat's.
func (a *app) sidebarBg(c *ui.Context) ui.Color {
	if runtime.GOOS == "darwin" && a.vibrancy {
		return ui.Transparent
	}
	return pal(c).sidebar
}
