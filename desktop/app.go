package main

import (
	"strings"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/mcp"
	"github.com/zucchiniEvader/baggy/provider/connect"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/team"
)

// app is the window's state. It belongs to the main thread: team events
// reach it through post.
type app struct {
	st   *store.Store
	team *team.Team
	// post runs fn on the main thread and draws a frame; a window's Update.
	post func(fn func())

	bots  []store.Bot
	byID  map[string]store.Bot
	chats []store.Chat
	sel   string // the chat shown
	views map[string]*chatView
	busy  map[string]bool
	// runFrom is, by chat, the newest message as its run started: what the
	// run adds, which its notification tells, comes after it.
	runFrom map[string]int64

	side         listNav
	previews     map[string]store.Preview // the chats' last words
	aboutOpen    map[string]bool          // the inspector's descriptions opened past their first lines
	bitmaps      map[string]*ui.Bitmap    // pictures by path (avatars, attachments); nil while made or unreadable
	sidebarShown bool
	dragWidth    float32
	resizing     bool
	filter       string // the sidebar's search
	filterFocus  bool
	vibrancy     bool // the window shows the sidebar's material (macOS)
	inspector    bool
	tasksOpen    bool
	focused      bool           // the window has the focus
	unread       map[string]int // by chat
	notify       func(title, subtitle, body, chatID string)
	setBadge     func(n int) // the unread total on the app's icon
	badge        int
	sideWidth    float32

	botSheet   *botForm
	groupSheet *groupForm
	settings   bool // the Settings window is wanted (tests, which have no windows)
	set        settingsState
	confirm    *confirmation
	palette    *paletteState
	memSheet   *memoryForm
	routine    *routineSheet
	setup      *setup
	approvals  *approvals
	policy     atomic.Value // compactPolicy
	prefs      *prefs

	fallback       connect.Config                  // the provider when none is stored
	locale         string                          // the system's language
	readImage      func() []byte                   // the clipboard's image as PNG, or nil
	pickFiles      func(done func(paths []string)) // asks for files to attach; done runs on the main thread with the ones chosen
	pickImage      func(done func(path string))    // asks for a picture, as pickFiles
	pickFolder     func(done func(path string))    // asks for a folder, as pickFiles
	openFolder     func(path string)
	revealFile     func(path string) // shows a file in Finder
	onLanguage     func()            // the language changed: the menu bar is made again
	showSettings   func()            // opens or brings up the Settings window
	redrawMain     func()            // draws the main window again
	redrawSettings func()            // draws the Settings window again
	mcpPath        string            // the user's mcp.json
	servers        []mcp.Status      // as the last check left them
	checking       bool
}

// chatView is one chat's transcript as drawn and its composer.
type chatView struct {
	id     string
	rows   []row
	stream map[string]int // turn id to the row it streams into
	list   ui.ListState

	draft    string
	mentions []string // bot ids picked from the @ menu
	reply    *store.Quote
	atOpen   bool
	atSel    int
	atDraft  string            // the draft the @ menu last saw
	files    []team.Attachment // attached, sent with the next message
	focus    bool
	open     map[string]bool // disclosures opened: thinking, tool calls
	working  string          // the bot whose turn runs
	retrying string          // why the reply under way is being asked again
}

type confirmation struct {
	open    bool
	title   string
	message string
	action  string
	run     func()
}

func newApp(st *store.Store, tm *team.Team, post func(func())) *app {
	a := &app{st: st, team: tm, post: post, views: map[string]*chatView{}, busy: map[string]bool{}, runFrom: map[string]int64{}, unread: map[string]int{}, focused: true}
	a.approvals = newApprovals(a)
	a.prefs = loadPrefs(st)
	a.sideWidth, a.inspector, a.sidebarShown = a.prefs.sideWidth, a.prefs.inspector, true
	a.dragWidth = a.sideWidth
	a.reload()
	if len(a.chats) > 0 {
		a.selectChat(a.chats[0].ID)
	}
	return a
}

// reload reads bots and chats again.
func (a *app) reload() {
	a.bots, _ = a.st.Bots()
	a.byID = map[string]store.Bot{}
	for _, b := range a.bots {
		a.byID[b.ID] = b
	}
	a.chats, _ = a.st.Chats()
	a.previews, _ = a.st.Previews()
	a.countUnread()
	if a.sel != "" && a.chat(a.sel) == nil {
		a.sel = ""
		if len(a.chats) > 0 {
			a.sel = a.chats[0].ID
		}
	}
}

func (a *app) chat(id string) *store.Chat {
	for i := range a.chats {
		if a.chats[i].ID == id {
			return &a.chats[i]
		}
	}
	return nil
}

// title is a chat's name: a DM's bot, a group's title.
func (a *app) title(c store.Chat) string {
	if c.Kind == store.DM {
		return a.name(c.Owner)
	}
	return c.Title
}

func (a *app) name(author string) string {
	if author == store.User {
		return L("You")
	}
	if b, ok := a.byID[author]; ok {
		return b.Name
	}
	return L("Deleted bot")
}

// view returns the chat's view, loading its transcript the first time.
func (a *app) view(id string) *chatView {
	if v := a.views[id]; v != nil {
		return v
	}
	v := &chatView{id: id, stream: map[string]int{}, open: map[string]bool{}}
	v.list.FollowEnd = true
	if es, err := a.st.Messages(id, 0); err == nil {
		v.rows = buildRows(es)
	} else {
		v.notice(err.Error())
	}
	a.views[id] = v
	return v
}

func (v *chatView) notice(text string) { v.rows = append(v.rows, row{kind: rowNotice, text: text}) }

func (a *app) selectChat(id string) {
	a.sel = id
	a.view(id).focus = true
	a.markRead()
}

// apply folds one team event into the state.
func (a *app) apply(e team.Event) {
	switch e.Kind {
	case team.EventRunStart:
		a.busy[e.ChatID] = true
		a.runFrom[e.ChatID] = e.Since // what this run adds comes after
		return
	case team.EventRunEnd:
		delete(a.busy, e.ChatID)
		a.countUnread()
		a.markRead()
		a.notifyEnd(e.ChatID)
		if v := a.views[e.ChatID]; v != nil {
			v.working, v.retrying = "", ""
			v.stream = map[string]int{}
		}
		return
	case team.EventChanged:
		a.reload()
		return
	}
	v := a.views[e.ChatID]
	if e.Kind == team.EventEntry {
		a.reload() // the chat moves up the sidebar
		if e.ChatID == a.sel {
			a.markRead()
		}
		a.notifyNotice(*e.Entry)
	}
	if v == nil {
		return // not opened: it loads from the store when it is
	}
	switch e.Kind {
	case team.EventTurn:
		v.working = e.BotID
	case team.EventAgent:
		v.applyAgent(e)
	case team.EventEntry:
		v.applyEntry(*e.Entry)
	case team.EventRemoved:
		v.remove(e.Entry.Seq)
	case team.EventError:
		v.notice(e.Err.Error())
	}
}

func (v *chatView) applyAgent(e team.Event) {
	ev := e.Agent
	switch ev.Kind {
	case agent.EventMessageStart:
		if ev.Message == nil { // a model reply is starting
			v.retrying = ""
			v.rows = append(v.rows, row{kind: rowAssistant, author: e.BotID, turn: e.Turn})
			v.stream[e.Turn] = len(v.rows) - 1
		}
	case agent.EventTextDelta, agent.EventThinkingDelta:
		i, ok := v.stream[e.Turn]
		if !ok || i >= len(v.rows) {
			return
		}
		if ev.Kind == agent.EventTextDelta {
			v.rows[i].text += ev.Delta
		} else {
			v.rows[i].thinking += ev.Delta
		}
	case agent.EventServerTool:
		if i, ok := v.stream[e.Turn]; ok && i < len(v.rows) {
			v.rows[i].searches = addServer(v.rows[i].searches, *ev.Call)
		}
	case agent.EventRetry: // the reply streaming failed and is asked again
		if i, ok := v.stream[e.Turn]; ok && i < len(v.rows) && v.rows[i].seq == 0 {
			v.rows = append(v.rows[:i], v.rows[i+1:]...)
			delete(v.stream, e.Turn)
		}
		v.retrying = ev.Delta
	case agent.EventToolUpdate:
		for i := len(v.rows) - 1; i >= 0; i-- {
			if r := &v.rows[i]; r.kind == rowTool && r.call.CallID == ev.Call.CallID {
				r.partial = ev.Result.Text
				break
			}
		}
	case agent.EventToolEnd:
		for i := len(v.rows) - 1; i >= 0; i-- {
			if r := &v.rows[i]; r.kind == rowTool && r.call.CallID == ev.Call.CallID {
				res := *ev.Result
				r.result, r.failed, r.pending = &res, res.IsError, false
				break
			}
		}
	}
}

// applyEntry shows a stored entry: a reply replaces the row it streamed into.
func (v *chatView) applyEntry(e store.Entry) {
	if e.Msg.Role == agent.RoleTool {
		fillResults(v.rows, e.Msg)
		return
	}
	rows := entryRows(e)
	if i, ok := v.stream[e.Turn]; ok && e.Msg.Role == agent.RoleAssistant && i < len(v.rows) && v.rows[i].seq == 0 {
		delete(v.stream, e.Turn)
		v.rows = append(v.rows[:i], append(rows, v.rows[i+1:]...)...)
		return
	}
	v.rows = append(v.rows, rows...)
}

// remove takes out the rows of an entry (a passed turn).
func (v *chatView) remove(seq int64) {
	out := v.rows[:0]
	for _, r := range v.rows {
		if r.seq != seq {
			out = append(out, r)
		}
	}
	v.rows = out
}

// ---- actions ----

// send sends the draft of the chat shown.
func (a *app) send() { a.sendDraft(false) }

// interruptSend sends the draft and cuts the bot's step short, so it reads
// the message at once.
func (a *app) interruptSend() { a.sendDraft(true) }

func (a *app) sendDraft(interrupt bool) {
	v := a.views[a.sel]
	if v == nil {
		return
	}
	text := strings.TrimSpace(v.draft)
	if text == "" && len(v.files) == 0 {
		return
	}
	var mentions []string
	for _, id := range v.mentions {
		if b, ok := a.byID[id]; ok && strings.Contains(text, "@"+b.Name) {
			mentions = append(mentions, id)
		}
	}
	mentions = append(mentions, a.typedMentions(text, mentions)...)
	if err := a.team.SendFiles(a.sel, text, v.files, mentions, v.reply); err != nil {
		v.notice(err.Error())
		return
	}
	v.draft, v.mentions, v.reply, v.files, v.atOpen = "", nil, nil, nil, false
	v.focus = true
	if interrupt {
		a.approvals.denyAll(a.sel)
		a.team.Interrupt(a.sel)
	}
}

// typedMentions are the bots named @Name in text that only one bot answers
// to and were not picked from the menu.
func (a *app) typedMentions(text string, picked []string) []string {
	var out []string
	has := map[string]bool{}
	for _, id := range picked {
		has[id] = true
	}
	count := map[string]int{}
	for _, b := range a.bots {
		count[strings.ToLower(b.Name)]++
	}
	for _, b := range a.bots {
		if !has[b.ID] && count[strings.ToLower(b.Name)] == 1 && strings.Contains(strings.ToLower(text), "@"+strings.ToLower(b.Name)) {
			out = append(out, b.ID)
			has[b.ID] = true
		}
	}
	return out
}

func (a *app) stop() {
	a.team.Stop(a.sel)
	a.approvals.denyAll(a.sel)
}

// replyTo starts a reply to the row's entry.
func (a *app) replyTo(r row) {
	v := a.views[a.sel]
	if v == nil || r.seq == 0 {
		return
	}
	if e, err := a.st.Entry(a.sel, r.seq); err == nil {
		v.reply = team.QuoteOf(e)
		v.focus = true
	}
}

// members of the chat shown, or every bot for a DM: who the @ menu offers.
func (a *app) mentionable(c store.Chat) []store.Bot {
	if c.Kind == store.DM {
		var out []store.Bot
		for _, b := range a.bots {
			if b.ID != c.Owner {
				out = append(out, b)
			}
		}
		return out
	}
	var out []store.Bot
	for _, id := range c.Members {
		if b, ok := a.byID[id]; ok {
			out = append(out, b)
		}
	}
	return out
}

func (a *app) ask(title, message, action string, run func()) {
	a.confirm = &confirmation{open: true, title: title, message: message, action: action, run: run}
}

func (a *app) deleteChat(c store.Chat) {
	if c.Kind == store.DM {
		name := a.name(c.Owner)
		a.ask(L("Delete %s?", name), L("This deletes the bot, its direct chat, and its seat in every group. Its workspace folder is kept."), L("Delete Bot"), func() {
			a.team.Stop(c.ID)
			a.do(a.st.DeleteBot(c.Owner))
		})
		return
	}
	a.ask(L("Delete %s?", c.Title), L("This deletes the group and its messages. The bots stay."), L("Delete Group"), func() {
		a.team.Stop(c.ID)
		a.do(a.st.DeleteChat(c.ID))
	})
}

// do reloads after a change, showing a failure in the chat shown.
func (a *app) do(err error) {
	if err != nil {
		if v := a.views[a.sel]; v != nil {
			v.notice(err.Error())
		}
	}
	a.reload()
}

// toolResult is how a tool row's result reads.
func toolStatus(r row) string {
	switch {
	case r.pending:
		return L("waiting for approval")
	case r.result != nil && r.failed:
		return L("failed")
	case r.result != nil:
		return L("done")
	}
	return L("running")
}
