package main

import (
	"hash/fnv"
	"math"
	"os"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/images"
	"github.com/spike/gobot/store"
)

// Avatars, after ../lorca/desktop/avatar.go: a bot's icon in white on a
// gradient of its color, or its own picture filling the circle; a group's
// members packed into the same square.

var white = ui.RGB(255, 255, 255)

// botColors are the colors a bot's avatar can take, by name.
var botColors = []struct {
	name  string
	color ui.Color
}{
	{"blue", ui.Hex("#007aff")},
	{"indigo", ui.Hex("#5856d6")},
	{"purple", ui.Hex("#af52de")},
	{"pink", ui.Hex("#ff2d55")},
	{"red", ui.Hex("#ff3b30")},
	{"orange", ui.Hex("#ff9500")},
	{"green", ui.Hex("#34c759")},
	{"teal", ui.Hex("#30b0c7")},
	{"brown", ui.Hex("#a2845e")},
	{"graphite", ui.Hex("#8e8e93")},
}

// colorName is a bot's color: the one it was given, else one its id picks,
// so bots differ from the start.
func colorName(b store.Bot) string {
	for _, c := range botColors {
		if c.name == b.Color {
			return c.name
		}
	}
	h := fnv.New32a()
	h.Write([]byte(b.ID))
	return botColors[h.Sum32()%uint32(len(botColors))].name
}

func colorOf(name string) ui.Color {
	for _, c := range botColors {
		if c.name == name {
			return c.color
		}
	}
	return botColors[0].color
}

// look is what an avatar shows: an icon on a color, or a picture.
type look struct {
	icon    *ui.SVG
	color   ui.Color
	picture string // a file; "" for the icon
}

func (a *app) lookOf(b store.Bot) look {
	l := look{icon: botIcon(b.Icon), color: colorOf(colorName(b))}
	if b.Avatar != "" {
		l.picture = a.st.AvatarPath(b.Avatar)
	}
	return l
}

// avatar is a bot's avatar, size across.
func (a *app) avatar(c *ui.Context, botID string, size float32) ui.Element {
	return a.avatarOf(c, a.lookOf(a.byID[botID]), size).Label(a.name(botID))
}

func (a *app) avatarOf(c *ui.Context, l look, size float32) ui.Element {
	disc := ui.Box(c).Size(size, size).Radius(size / 2).Clip().Center().Shrink(0)
	if b := a.picture(l.picture); b != nil {
		disc.Children(func() { ui.Image(c, b).Size(size, size).Fit(ui.Cover) })
		return disc
	}
	disc.Gradient(l.color.Mix(white, 0.28), l.color, 180)
	disc.Children(func() {
		ui.Icon(c, l.icon).FontSize(float32(math.Round(float64(size * 0.52)))).TextColor(white)
	})
	return disc
}

// chatAvatar is a chat's avatar in a square slot: a DM's bot, or up to four
// of a group's members, the front ones ringed in backdrop.
func (a *app) chatAvatar(c *ui.Context, ch store.Chat, slot float32, backdrop ui.Color) ui.Element {
	if ch.Kind == store.DM {
		return a.avatar(c, ch.Owner, slot)
	}
	members := ch.Members[:min(len(ch.Members), 4)]
	if len(members) == 0 {
		return iconTile(c, iconUsers, slot)
	}
	size := float32(math.Round(float64(slot) * 0.6))
	free := slot - size
	var at [][2]float32
	switch len(members) {
	case 1:
		return a.avatar(c, members[0], slot)
	case 2:
		at = [][2]float32{{0, 0}, {free, free}}
	case 3:
		at = [][2]float32{{0, 0}, {free, 0}, {free / 2, free}}
	default:
		at = [][2]float32{{0, 0}, {free, 0}, {0, free}, {free, free}}
	}
	box := ui.Box(c).Size(slot, slot).Shrink(0).Label(a.title(ch))
	box.Children(func() {
		for i, id := range members {
			cell := ui.Box(c).Absolute().Left(at[i][0]).Top(at[i][1]).Size(size, size).Radius(size / 2)
			if i > 0 {
				cell.Shadow(0, 0, 0, 1.5, backdrop)
			}
			cell.Children(func() { a.avatar(c, id, size) })
		}
	})
	return box
}

// picture is a bitmap of a file, read off the frame the first time it is
// asked for; nil until then, and when it cannot be read.
func (a *app) picture(path string) *ui.Bitmap {
	return a.bitmap(path, func(data []byte) (*ui.Bitmap, error) { return ui.DecodeBitmap(data) })
}

// bitmap is the bitmap make makes of a file, made off the frame once and
// kept by path; nil while it is made, and when it cannot be.
func (a *app) bitmap(path string, make func([]byte) (*ui.Bitmap, error)) *ui.Bitmap {
	if path == "" {
		return nil
	}
	if b, ok := a.bitmaps[path]; ok {
		return b
	}
	if a.bitmaps == nil {
		a.bitmaps = map[string]*ui.Bitmap{}
	}
	a.bitmaps[path] = nil
	go func() {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		b, err := make(data)
		if err != nil {
			return
		}
		a.post(func() { a.bitmaps[path] = b })
	}()
	return nil
}

// avatarSide is the longest side of a stored avatar picture.
const avatarSide = 512

// setPicture makes the picture at path the form's avatar: a square PNG
// kept in the store.
func (a *app) setPicture(f *botForm, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	png, err := images.Square(data, path, avatarSide)
	if err != nil {
		return errf("That file could not be read as an image.")
	}
	name, err := a.st.SaveAvatar(png)
	if err != nil {
		return err
	}
	f.avatar = name
	return nil
}
