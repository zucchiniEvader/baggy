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
	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/mcp"
	"github.com/spike/gobot/provider/connect"
	"github.com/spike/gobot/shellenv"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/team"
	"github.com/spike/gobot/terminal"
	"github.com/spike/gobot/tool"
)

func dataDir() string {
	if d := os.Getenv("GOBOT_DATA"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "gobot")
}

func main() {
	mygo.App.SetName("gobot")
	if !mygo.App.RequestSingleInstanceLock() {
		return // the running one shows its window
	}
	st, err := store.Open(dataDir())
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if _, err := st.Seed(); err != nil {
		log.Fatal(err)
	}
	fallback := connect.FromEnv("claude-sonnet-5-5")
	var a *app
	tm := newTeam(st, fallback, func(e team.Event) { a.post(func() { a.apply(e) }) })
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
	a.locale = mygo.App.Locale()
	a.readImage = func() []byte { return mygo.Clipboard.ReadImage() }
	tm.Approve = a.approvals.approve
	tm.StartRoutines(context.Background())
	a.applyPrefs()

	mygo.App.WhenReady(func() {
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
		}
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:          "gobot",
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
		a.mcpPath = filepath.Join(dataDir(), "mcp.json")
		win.OnFocus(func() { win.Update(func() { a.focused = true; a.markRead() }) })
		win.OnBlur(func() { win.Update(func() { a.focused = false }) })
		a.notify = func(title, subtitle, body, chatID string) {
			go mygo.NewNotification(mygo.NotificationOptions{ID: "chat-" + chatID + "-" + store.NewID("n"), Title: title, Subtitle: subtitle, Body: body, Group: chatID}).Show()
		}
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
		a.pickImage = func() []string {
			paths, _ := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: L("Attach Images"), Multiple: true,
				Filters: []mygo.FileFilter{{Name: L("Images"), Extensions: []string{"png", "jpg", "jpeg", "gif", "webp"}}}})
			return paths
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
		win.OnClose(func(e *mygo.CloseEvent) {
			if a.prefs.keepRunning {
				e.PreventDefault()
				win.Hide()
			}
		})
		tray, err := mygo.NewTray(mygo.TrayOptions{
			Icon: trayIcon, IconIsTemplate: true, ToolTip: "gobot",
			Menu: mygo.NewMenu([]*mygo.MenuItem{
				{Label: L("Open gobot"), Click: func(*mygo.MenuItem, *mygo.Window) { win.Show(); mygo.App.Focus() }},
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
			{Label: L("New Bot…"), Accelerator: "CmdOrCtrl+N", Click: do(a.newBot)},
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
