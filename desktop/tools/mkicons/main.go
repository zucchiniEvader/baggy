// Command mkicons makes the desktop app's icons from resources/icon-source.png:
//
//	cd desktop/tools/mkicons && go run .
//
// resources/icon.png, the art in a macOS-shaped 824 rounded square on a
// transparent 1024 canvas (mygo build makes the bundle's icons from it), and
// assets/tray.png, the bag as a 32 px menu-bar template silhouette, its
// eyes, mouth and folds cut out and grown so they read at 16 points.
package main

func main() {
	const src = "../../resources/icon-source.png"
	makeApp(src, "../../resources/icon.png")
	makeTray(src, "../../assets/tray.png", 32, 14)
}
