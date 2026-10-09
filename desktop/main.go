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
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:    "gobot",
			Width:    1040,
			Height:   700,
			StateKey: "main", // size, position, maximized: back where they were
			Content:  ui.View(a.root),
		})
		a.post = win.Update
		a.openFolder = func(p string) { go mygo.Shell.OpenPath(p) }
		a.mcpPath = filepath.Join(dataDir(), "mcp.json")
		a.pickImage = func() []string {
			paths, _ := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: L("Attach Images"), Multiple: true,
				Filters: []mygo.FileFilter{{Name: L("Images"), Extensions: []string{"png", "jpg", "jpeg", "gif", "webp"}}}})
			return paths
		}
		for len(pending) > 0 {
			win.Update(<-pending)
		}
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
