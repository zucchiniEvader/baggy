package main

import (
	_ "embed"
	"sync"

	"github.com/egoist/mygo/ui"
)

//go:embed assets/tray.png
var trayIcon []byte

// appIcon is the app's icon (resources/icon.png, which mygo build also
// makes the bundle's): the Dock's while developing, and the About pane's.
//
//go:embed resources/icon.png
var appIcon []byte

var appBitmap = sync.OnceValue(func() *ui.Bitmap {
	b, err := ui.DecodeBitmap(appIcon)
	if err != nil {
		return nil
	}
	return b
})
