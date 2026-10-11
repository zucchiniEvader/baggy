// Command desktop is gobot's window: the user's bots, their direct chats and
// groups, drawn by MyGo's native UI over the team runtime.
//
//	go tool mygo dev
//
// State lives in GOBOT_DATA (default: the user config folder's gobot). With
// no provider stored, bots run on the environment's (connect.FromEnv).
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/mcp"
	"github.com/zucchiniEvader/baggy/provider/connect"
	"github.com/zucchiniEvader/baggy/shellenv"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/team"
	"github.com/zucchiniEvader/baggy/terminal"
	"github.com/zucchiniEvader/baggy/tool"
)

func dataDir() string {
	if d := os.Getenv("GOBOT_DATA"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "Baggy")
}

// renameFolders moves the folders the app had under its old name, gobot,
// to the new ones, unless those are there already.
func renameFolders() {
	if os.Getenv("GOBOT_DATA") == "" {
		if base, err := os.UserConfigDir(); err == nil {
			moveFolder(filepath.Join(base, "gobot"), dataDir())
		}
	}
	if home, err := os.UserHomeDir(); err == nil && os.Getenv("GOBOT_WORKSPACES") == "" {
		moveFolder(filepath.Join(home, ".gobot"), filepath.Join(home, ".baggy"))
	}
}

func moveFolder(from, to string) {
	if _, err := os.Stat(to); err == nil {
		return
	}
	if _, err := os.Stat(from); err != nil {
		return
	}
	if err := os.Rename(from, to); err != nil {
		log.Printf("moving %s to %s: %v", from, to, err)
	}
}

// workspacesDir is where the bots' workspaces live: GOBOT_WORKSPACES, else
// ~/.baggy/workspaces; "" (beside the database) for a data folder of
// GOBOT_DATA, so a test copy of the app keeps to itself.
func workspacesDir() string {
	if d := os.Getenv("GOBOT_WORKSPACES"); d != "" {
		return d
	}
	if os.Getenv("GOBOT_DATA") != "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".baggy", "workspaces")
}

func main() {
	mygo.App.SetName("Baggy")
	if !mygo.App.RequestSingleInstanceLock() {
		return // the running one shows its window
	}
	renameFolders()
	st, err := store.Open(dataDir())
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if root := workspacesDir(); root != "" {
		if err := st.UseWorkspaces(root); err != nil {
			log.Print(err) // the ones not moved stay where they were
		}
	}
	if _, err := st.Seed(); err != nil {
		log.Fatal(err)
	}
	fallback := connect.FromEnv("claude-sonnet-5-5")
	var a *app
	ph := newPhone(st)
	tm := newTeam(st, fallback, func(e team.Event) {
		a.post(func() { a.apply(e) })
		ph.srv.Publish(e)
	})
	ph.srv.Team = tm
	defer ph.stop()
	tm.Plugins = mcp.NewManager(filepath.Join(dataDir(), "mcp.json"))
	defer tm.Plugins.Close()
	tm.Codemode = true
	tm.Checks = tm.RunCheck
	mcp.Env = shellenv.Env
	tm.Terminals = terminal.NewManager()
	tm.Terminals.Env = shellenv.Env
	defer tm.Terminals.StopAll()
	tm.WakeOnExit()
	pending := make(chan func(), 1024) // events before the window is up
	a = newApp(st, tm, func(fn func()) { pending <- fn })
	a.fallback = fallback
	a.phone, ph.srv.Team = ph, tm
	ph.srv.Changed = func(string) { a.post(a.countUnread) }
	a.changed = func(chatID string) { ph.srv.Publish(team.Event{Kind: team.EventChanged, ChatID: chatID}) }
	a.locale = mygo.App.Locale()
	a.readImage = func() []byte { return mygo.Clipboard.ReadImage() }
	a.copyText = func(s string) { mygo.Clipboard.WriteText(s) }
	tm.Approve = a.approvals.approve
	if tm.Compaction != nil {
		tm.Compaction.Policy = a.compactionPolicy
	}
	tm.StartRoutines(context.Background())
	a.applyPrefs()
	// Updates: MyGo's update window in native UI, a check a day; the menu's
	// Check for Updates… and Settings' About check at once.
	updateLang := map[string]string{"English": "en", "中文": "zh-Hans"}[a.prefs.language]
	mygo.Use(native.New(updater.Options{Language: updateLang}))
	a.version = mygo.App.Version()
	ph.srv.Version = a.version
	ph.start()
	if mygo.Updater.Enabled() {
		a.checkUpdates = updater.CheckForUpdates
		on := updater.AutomaticChecks()
		a.autoUpdates = &on
	}

	mygo.App.WhenReady(func() {
		if runtime.GOOS == "darwin" {
			mygo.App.Dock.SetIcon(appIcon) // the bundle's own, also for a bare binary
		}
		mygo.App.SetMenu(buildMenu(a, func(fn func()) { a.post(fn) }))
		a.onLanguage = func() { mygo.App.SetMenu(buildMenu(a, func(fn func()) { a.post(fn) })) }
		var settingsWin *mygo.Window
		a.showSettings = func() {
			if settingsWin != nil {
				settingsWin.Show()
				settingsWin.Focus()
				settingsWin.Update(func() {})
				return
			}
			settingsWin = mygo.NewWindow(mygo.WindowOptions{
				Title:                L("Settings"),
				Width:                800,
				Height:               580,
				MinWidth:             640,
				MinHeight:            420,
				StateKey:             "settings",
				TitleBarStyle:        mygo.TitleBarHiddenInset,
				TitleBarHeight:       titleBarHeight,
				TrafficLightPosition: &mygo.Point{X: 14, Y: (titleBarHeight - 14) / 2},
				Vibrancy:             mygo.VibrancySidebar,
				Content:              ui.View(a.settingsRoot),
			})
			settingsWin.OnClosed(func() { settingsWin = nil })
			win := settingsWin
			a.redrawSettings = func() { win.Update(func() {}) }
		}
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:          "Baggy",
			Width:          1180,
			Height:         780,
			MinWidth:       720,
			MinHeight:      480,
			StateKey:       "main", // size, position, maximized: back where they were
			TitleBarStyle:  mygo.TitleBarHiddenInset,
			TitleBarHeight: titleBarHeight,
			// The traffic lights in the middle of the title bar's height.
			TrafficLightPosition: &mygo.Point{X: 14, Y: (titleBarHeight - 14) / 2},
			Vibrancy:             mygo.VibrancySidebar,
			Content:              ui.View(a.root),
		})
		a.vibrancy = runtime.GOOS == "darwin"
		a.redrawMain = func() { win.Update(func() {}) }
		a.post = win.Update
		a.openFolder = func(p string) { go mygo.Shell.OpenPath(p) }
		a.revealFile = func(p string) { go mygo.Shell.ShowItemInFolder(p) }
		a.mcpPath = filepath.Join(dataDir(), "mcp.json")
		win.OnFocus(func() { win.Update(func() { a.focused = true; a.markRead() }) })
		win.OnBlur(func() { win.Update(func() { a.focused = false }) })
		a.notify = func(title, subtitle, body, chatID string) {
			go mygo.NewNotification(mygo.NotificationOptions{ID: "chat-" + chatID + "-" + store.NewID("n"), Title: title, Subtitle: subtitle, Body: body, Group: chatID}).Show()
		}
		a.setBadge = mygo.App.SetBadgeCount
		a.showBadge()
		mygo.App.OnNotificationClick(func(id string) {
			win.Update(func() {
				if rest, ok := strings.CutPrefix(id, "chat-"); ok {
					if i := strings.LastIndex(rest, "-n-"); i >= 0 {
						rest = rest[:i]
					}
					a.selectChat(rest)
				}
				win.Show()
				mygo.App.Focus()
			})
		})
		// The file dialog waits for the user, so it is asked from a goroutine:
		// asked from a frame, it would wait on the very thread that draws.
		a.pickFiles = func(done func(paths []string)) {
			go func() {
				paths, _ := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: L("Attach Files"), Multiple: true})
				if len(paths) > 0 {
					win.Update(func() { done(paths) })
				}
			}()
		}
		a.pickFolder = func(done func(path string)) {
			go func() {
				paths, _ := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: L("Choose a Folder"), Directory: true, CreateDirectories: true})
				if len(paths) > 0 {
					win.Update(func() { done(paths[0]) })
				}
			}()
		}
		a.pickImage = func(done func(path string)) {
			go func() {
				paths, _ := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: L("Choose Picture"),
					Filters: []mygo.FileFilter{{Name: L("Images"), Extensions: []string{"png", "jpg", "jpeg", "gif", "webp", "heic"}}}})
				if len(paths) > 0 {
					win.Update(func() { done(paths[0]) })
				}
			}()
		}
		for len(pending) > 0 {
			win.Update(<-pending)
		}
		mygo.App.OnSecondInstance(func([]string, string) { win.Update(func() { win.Show(); win.Focus() }) })
		go func() { // running commands' cards and tasks show their output as it comes
			for range time.Tick(time.Second) {
				if len(tm.Terminals.Running("")) > 0 {
					win.Update(func() {})
				}
			}
		}()
		// Closing the window hides it while bots keep working; quitting
		// (⌘Q, the Dock, the tray, SIGTERM) closes every window, which must
		// then go, or the quit is cancelled.
		quitting := false
		mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { quitting = true })
		win.OnClose(func(e *mygo.CloseEvent) {
			if keepOpen(a.prefs.keepRunning, quitting) {
				e.PreventDefault()
				win.Hide()
			}
		})
		tray, err := mygo.NewTray(mygo.TrayOptions{
			Icon: trayIcon, IconIsTemplate: true, ToolTip: "Baggy",
			Menu: mygo.NewMenu([]*mygo.MenuItem{
				{Label: L("Open Baggy"), Click: func(*mygo.MenuItem, *mygo.Window) { win.Show(); mygo.App.Focus() }},
				mygo.Separator(),
				{Role: mygo.RoleQuit},
			}),
		})
		if err != nil {
			log.Println("tray:", err)
		}
		_ = tray
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// newTeam runs the bots with the shell and file tools in their workspaces.
func newTeam(st *store.Store, fallback connect.Config, emit func(team.Event)) *team.Team {
	return &team.Team{
		Store: st,
		Model: team.Models(st, fallback),
		Tools: func(b store.Bot, ws string) []tool.Tool {
			return []tool.Tool{tool.Shell{Dir: ws}, tool.ReadFile{Dir: ws}, tool.WriteFile{Dir: ws}, tool.EditFile{Dir: ws}, tool.Find{Dir: ws}, tool.Grep{Dir: ws}}
		},
		Emit:       emit,
		Retry:      agent.DefaultRetry,
		Memory:     os.Getenv("GOBOT_MEMORY") != "0",
		Compaction: &team.Compaction{NoFlush: os.Getenv("GOBOT_MEMORY_FLUSH") == "0"},
	}
}

// buildMenu is the menu bar; its actions run on the window's thread.
func buildMenu(a *app, post func(func())) *mygo.Menu {
	do := func(fn func()) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) { post(fn) }
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu, Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			{Label: L("Check for Updates…"), Click: func(*mygo.MenuItem, *mygo.Window) { updater.CheckForUpdates() }},
			mygo.Separator(),
			{Label: L("Settings…"), Accelerator: "CmdOrCtrl+,", Click: do(func() { a.openSettings("") })},
			mygo.Separator(),
			{Role: mygo.RoleServices},
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Label: L("File"), Submenu: []*mygo.MenuItem{
			{Label: L("New Bot…"), Accelerator: "CmdOrCtrl+N", Click: do(a.newFromTemplate)},
			{Label: L("New Group…"), Accelerator: "CmdOrCtrl+Shift+N", Click: do(a.newGroup)},
			mygo.Separator(),
			{Role: mygo.RoleClose},
		}},
		{Role: mygo.RoleEditMenu},
		{Label: L("View"), Submenu: []*mygo.MenuItem{
			{Label: L("Toggle Sidebar"), Accelerator: "CmdOrCtrl+Shift+B", Click: do(a.toggleSidebar)},
			{Label: L("Toggle Info"), Accelerator: "CmdOrCtrl+Alt+I", Click: do(func() { a.inspector = !a.inspector; a.layoutChanged() })},
			{Label: L("Search Chats"), Accelerator: "CmdOrCtrl+F", Click: do(func() { a.sidebarShown, a.filterFocus = true, true })},
			{Label: L("Command Palette"), Accelerator: "CmdOrCtrl+K", Click: do(a.openPalette)},
			mygo.Separator(),
			{Label: L("Stop"), Accelerator: "CmdOrCtrl+.", Click: do(a.stop)},
			mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen},
		}},
		{Role: mygo.RoleWindowMenu},
	})
}

// keepOpen reports whether closing the main window only hides it: while
// bots should keep running, unless the app is quitting.
func keepOpen(keepRunning, quitting bool) bool { return keepRunning && !quitting }
