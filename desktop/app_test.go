package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/spike/gobot/mcp"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/provider/connect"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/team"
	"github.com/spike/gobot/tool"
)

// script answers as the bot the system prompt names.
type script func(bot string, msgs []agent.Message) agent.Message

var whoRe = regexp.MustCompile(`You are (\S+) \(id`)

func (s script) Call(ctx context.Context, system string, msgs []agent.Message, tools []tool.Tool, emit func(agent.Event)) agent.Message {
	m := s(whoRe.FindStringSubmatch(system)[1], msgs)
	m.Role = agent.RoleAssistant
	if m.StopReason == "" {
		m.StopReason = agent.StopEnd
	}
	if t := messageText(m); t != "" {
		emit(agent.Event{Kind: agent.EventTextDelta, Delta: t})
	}
	return m
}

func say(s string) agent.Message {
	return agent.Message{Parts: []agent.Part{{Kind: agent.PartText, Text: s}}}
}

func lastText(msgs []agent.Message) string { return messageText(msgs[len(msgs)-1]) }

type echoTool struct{}

func (echoTool) Name() string            { return "shell" }
func (echoTool) Description() string     { return "runs" }
func (echoTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (echoTool) Execute(_ context.Context, a json.RawMessage) tool.Result {
	return tool.Result{Text: "ran " + string(a)}
}

// harness is an app over a temporary store, its events run on the test
// goroutine as the window would run them.
type harness struct {
	t     *testing.T
	st    *store.Store
	a     *app
	tt    *ui.Tester
	posts chan func()
}

func newHarness(t *testing.T, m agent.Model) *harness {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.Seed()
	h := &harness{t: t, st: st, posts: make(chan func(), 1024)}
	tm := &team.Team{Store: st, Model: func(store.Bot) (agent.Model, error) { return m, nil },
		Tools: func(store.Bot, string) []tool.Tool { return []tool.Tool{echoTool{}} }}
	h.a = newApp(st, tm, func(fn func()) { h.posts <- fn })
	h.a.fallback = connect.Config{APIKey: "test"} // a provider, so the chat shows rather than the setup
	tm.Emit = func(e team.Event) { h.a.post(func() { h.a.apply(e) }) }
	tm.Approve = h.a.approvals.approve
	h.a.applyPrefs()
	h.tt = ui.NewTester(h.a.root, 1000, 700)
	return h
}

// pump runs posted functions and draws until cond holds.
func (h *harness) pump(what string, cond func() bool) {
	h.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		h.tt.Frame()
		if cond() {
			return
		}
		select {
		case fn := <-h.posts:
			fn()
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			h.t.Fatalf("timed out waiting for %s; texts %q", what, h.tt.Texts())
		}
	}
}

func (h *harness) idle() bool { return len(h.a.busy) == 0 && len(h.posts) == 0 }

func (h *harness) send(text string) {
	h.t.Helper()
	h.a.view(h.a.sel).focus = true
	h.tt.Frame()
	h.tt.Type(text)
	h.tt.Key(0, ui.KeyEnter)
}

func TestSendShowsReplyAndRestores(t *testing.T) {
	h := newHarness(t, script(func(bot string, msgs []agent.Message) agent.Message {
		return say("**Hi** from " + bot + ", re: " + lastText(msgs))
	}))
	if !h.tt.HasText("Chef") {
		t.Fatalf("no Chef: %q", h.tt.Texts())
	}
	h.send("hello")
	h.pump("the reply", func() bool { return h.tt.HasText(" from Chef, re: hello") && h.idle() })
	if !h.tt.HasText("Hi") { // bold, a span of its own
		t.Fatalf("markdown: %q", h.tt.Texts())
	}

	// A new app over the same store shows the same transcript.
	b := newApp(h.st, h.a.team, func(fn func()) { fn() })
	b.fallback = h.a.fallback
	b.applyPrefs()
	tt := ui.NewTester(b.root, 1000, 700)
	if !tt.HasText("hello") || !tt.HasText(" from Chef, re: hello") {
		t.Fatalf("not restored: %q", tt.Texts())
	}
}

func TestGroupShowsAuthorsAndPasses(t *testing.T) {
	h := newHarness(t, script(func(bot string, msgs []agent.Message) agent.Message {
		for _, m := range msgs {
			if m.Role == agent.RoleAssistant {
				return say("PASS")
			}
		}
		return say(bot + " here")
	}))
	scout, _ := h.st.CreateBot(store.Bot{Name: "Scout", Description: "Finds things."})
	chef := h.a.bots[0]
	g, _ := h.st.CreateGroup("Launch", "ship it", []string{chef.ID, scout.ID})
	h.a.reload()
	h.a.selectChat(g.ID)
	h.send("go")
	h.pump("both replies", func() bool { return h.tt.HasText("Chef here") && h.tt.HasText("Scout here") && h.idle() })
	if h.tt.HasText("PASS") {
		t.Fatal("a PASS shows")
	}
	if !h.tt.HasText("Scout") || !h.tt.HasText("Launch") {
		t.Fatalf("names: %q", h.tt.Texts())
	}
}

func TestMentionMenuPicksBot(t *testing.T) {
	h := newHarness(t, script(func(bot string, msgs []agent.Message) agent.Message { return say("ok") }))
	scout, _ := h.st.CreateBot(store.Bot{Name: "Scout"})
	h.a.reload()
	h.a.view(h.a.sel).focus = true
	h.tt.Frame()
	h.tt.Type("ask @Sc")
	h.tt.Frame()
	if !h.tt.HasText("@Scout") {
		t.Fatalf("no @ menu (draft %q, open %v): %q", h.a.views[h.a.sel].draft, h.a.views[h.a.sel].atOpen, h.tt.Texts())
	}
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	v := h.a.views[h.a.sel]
	if v.draft != "ask @Scout " || len(v.mentions) != 1 || v.mentions[0] != scout.ID {
		t.Fatalf("draft %q mentions %v", v.draft, v.mentions)
	}
}

func TestSidebarAccentOnlyFromKeyboard(t *testing.T) {
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("ok") }))
	h.st.CreateBot(store.Bot{Name: "Scout"})
	h.a.reload()
	h.tt.Frame()
	if err := h.tt.Click("Scout"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.a.side.keyboard || h.a.title(*h.a.chat(h.a.sel)) != "Scout" {
		t.Fatalf("click: keyboard=%v sel=%s", h.a.side.keyboard, h.a.sel)
	}
	h.tt.Frame()
	h.tt.Key(0, ui.KeyDown)
	h.tt.Frame()
	if !h.a.side.keyboard {
		t.Fatalf("keys did not set keyboard; focused chats %v message %v", h.tt.Focused("Chats"), h.tt.Focused("Message"))
	}
	h.tt.Click("Message")
	h.tt.Frame()
	if h.a.side.keyboard {
		t.Fatal("keyboard kept after the list lost the focus")
	}
}

func TestShellApproval(t *testing.T) {
	h := newHarness(t, script(func(bot string, msgs []agent.Message) agent.Message {
		if msgs[len(msgs)-1].Role == agent.RoleTool {
			return say("result: " + msgs[len(msgs)-1].Parts[0].Text)
		}
		return agent.Message{StopReason: agent.StopToolUse, Parts: []agent.Part{{Kind: agent.PartToolCall, CallID: "c1", Name: "shell", Args: json.RawMessage(`{"command":"ls"}`)}}}
	}))
	h.a.prefs.commands = ModeAsk
	h.a.applyPrefs()
	h.send("list files")
	h.pump("the approval card", func() bool { return h.tt.HasText("Allow") })
	if err := h.tt.Click("Allow"); err != nil {
		t.Fatal(err)
	}
	h.pump("the result", func() bool { return h.tt.HasText(`result: ran {"command":"ls"}`) && h.idle() })

	h.send("again")
	h.pump("the card", func() bool { return h.tt.HasText("Deny") })
	h.tt.Click("Deny")
	h.pump("the refusal", func() bool { return h.tt.HasText("result: "+Declined) && h.idle() })
}

func TestCreateBotSheet(t *testing.T) {
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("ok") }))
	h.a.newBot()
	h.tt.Frame()
	h.tt.Type("Scout")
	h.tt.Frame()
	if err := h.tt.Click("Create"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.a.botSheet != nil || len(h.a.bots) != 2 || h.a.title(*h.a.chat(h.a.sel)) != "Scout" {
		t.Fatalf("sheet %v bots %d", h.a.botSheet, len(h.a.bots))
	}
}

func TestPaletteRunsCommand(t *testing.T) {
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("ok") }))
	h.tt.Frame()
	h.tt.Key(ui.Cmd, ui.KeyK)
	h.tt.Frame()
	h.tt.Type("settings")
	h.tt.Frame()
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	if !h.a.settings || h.a.palette != nil {
		t.Fatalf("settings %v palette %v", h.a.settings, h.a.palette) // no windows here: wanted, not shown
	}
}

func TestChinese(t *testing.T) {
	defer setLanguage("English", "")
	setLanguage("Auto", "zh-CN")
	if L("Settings") == "Settings" {
		t.Fatal("no Chinese for Settings")
	}
	setLanguage("English", "zh-CN")
	if L("Settings") != "Settings" {
		t.Fatal("English not chosen")
	}
}

func TestMarkdownBlocks(t *testing.T) {
	bs := parseMarkdown("# Title\n\nSome **bold** and `code` [link](https://x.y).\n\n- a\n- [x] b\n\n```go\nfmt.Println()\n```\n\n| h1 | h2 |\n|----|----|\n| c1 | c2 |\n")
	kinds := []blockKind{}
	for _, b := range bs {
		kinds = append(kinds, b.kind)
	}
	want := []blockKind{blockHeading, blockParagraph, blockList, blockCode, blockTable}
	if len(kinds) != len(want) {
		t.Fatalf("kinds %v", kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds %v", kinds)
		}
	}
	p := bs[1].spans
	if len(p) < 5 || !p[1].bold || !p[3].code || p[5].link != "https://x.y" {
		t.Fatalf("spans %+v", p)
	}
	if c := bs[2].checked; c[0] != nil || c[1] == nil || !*c[1] {
		t.Fatalf("tasks %+v", c)
	}
	if bs[3].text != "fmt.Println()" || len(bs[4].rows) != 2 {
		t.Fatalf("code %q rows %d", bs[3].text, len(bs[4].rows))
	}
}

func TestAttachedFilesGoWithTheMessage(t *testing.T) {
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("read it") }))
	v := h.a.view(h.a.sel)
	if err := v.attachBytes([]byte("a,b\n1,2\n"), "data.csv"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if !h.tt.HasText("data.csv") || !h.tt.HasText("8 bytes") {
		t.Fatalf("no chip: %q", h.tt.Texts())
	}
	h.send("sum it")
	h.pump("the reply", func() bool { return h.tt.HasText("read it") && h.idle() })
	es, _ := h.st.Messages(h.a.sel, 0)
	if fs := es[0].Meta.Files; len(fs) != 1 || fs[0].Name != "data.csv" || len(v.files) != 0 {
		t.Fatalf("files %+v, draft %d", fs, len(v.files))
	}
	if !h.tt.HasText("data.csv") { // the sent message's chip
		t.Fatalf("no sent chip: %q", h.tt.Texts())
	}
	if pastedName(nil) != "pasted-image.png" || pastedName([]team.Attachment{{Name: "pasted-image.png"}}) != "pasted-image-2.png" {
		t.Fatal("pasted names")
	}
}

// GOBOT_RENDER=<folder> go test -run TestRender writes PNGs of a sample group.
func TestRender(t *testing.T) {
	dir := os.Getenv("GOBOT_RENDER")
	if dir == "" {
		t.Skip("set GOBOT_RENDER to a folder")
	}
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("ok") }))
	scout, _ := h.st.CreateBot(store.Bot{Name: "Scout", Description: "Finds things.", Icon: "search", Color: "blue"})
	chef := h.a.bots[0]
	g, _ := h.st.CreateGroup("Launch", "Ship the beta", []string{chef.ID, scout.ID})
	add := func(author string, m agent.Message) { h.st.Append(g.ID, author, "", store.Meta{}, m) }
	add(store.User, agent.Message{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: "@Scout find the three open bugs, then Chef plans."}}})
	add(scout.ID, agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopToolUse, Parts: []agent.Part{{Kind: agent.PartThinking, Text: "Search the tracker."}, {Kind: agent.PartText, Text: "Looking."}, {Kind: agent.PartToolCall, CallID: "c", Name: "shell", Args: json.RawMessage(`{"command":"gh issue list"}`)}}})
	add(scout.ID, agent.Message{Role: agent.RoleTool, Parts: []agent.Part{{Kind: agent.PartToolResult, CallID: "c", Text: "#12 crash\n#14 typo\n#15 slow start"}}})
	add(scout.ID, agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopEnd, Parts: []agent.Part{{Kind: agent.PartText, Text: "Three bugs:\n\n1. **#12** crash on start\n2. `#14` typo\n3. #15 slow start"}}})
	add(chef.ID, agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopEnd, Parts: []agent.Part{{Kind: agent.PartText, Text: "Plan: fix #12 first. I own the release."}}})
	h.st.Append(g.ID, store.User, "", store.Meta{Files: []store.File{{ID: "file-1", Name: "crash-report.log", Size: 48213}, {ID: "file-2", Name: "screenshot.png", Size: 812000}}},
		agent.Message{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: "Logs from this morning."}}})
	dm, _ := h.st.DMOf(scout.ID)
	h.st.SetPinned(dm.ID, true)
	h.a.reload()
	h.a.selectChat(g.ID)
	h.a.inspector = true
	for name, dark := range map[string]bool{"light": false, "dark": true} {
		h.tt.SetDark(dark)
		h.tt.Frame()
		f, err := os.Create(filepath.Join(dir, "group-"+name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		png.Encode(f, h.tt.Image())
		f.Close()
	}
	// Settings: Models with two providers, and General.
	h.st.SaveProvider(store.Provider{Name: "DeepSeek", API: "anthropic", BaseURL: "https://api.deepseek.com/anthropic", APIKey: "sk-x", Models: []string{"deepseek-flash", "deepseek-v4-pro"}, Options: map[string]bool{"web_search": true}})
	h.st.SaveProvider(store.Provider{Name: "Ollama", API: "chat", BaseURL: "http://localhost:11434/v1", APIKey: "ollama", Models: []string{"qwen3"}})
	for _, pane := range []string{paneModels, paneModels + "-detail", paneGeneral} {
		h.a.set.pane, h.a.set.provSel, h.a.set.prov = strings.TrimSuffix(pane, "-detail"), "", nil
		if strings.HasSuffix(pane, "-detail") {
			ps, _ := h.st.Providers()
			h.a.set.provSel = ps[0].ID
		}
		st := ui.NewTester(h.a.settingsRoot, 800, 580)
		st.Frame()
		f, _ := os.Create(filepath.Join(dir, "settings-"+pane+".png"))
		png.Encode(f, st.Image())
		f.Close()
	}
	ps, _ := h.st.Providers()
	for _, p := range ps {
		h.st.DeleteProvider(p.ID)
	}

	// The setup, before any model.
	dmScout, _ := h.st.DMOf(scout.ID)
	h.a.selectChat(dmScout.ID)
	h.a.inspector = false
	h.a.fallback = connect.Config{}
	h.tt.SetDark(false)
	h.tt.Frame()
	f, _ := os.Create(filepath.Join(dir, "setup-light.png"))
	png.Encode(f, h.tt.Image())
	f.Close()
	_ = strings.TrimSpace
}

func TestMemoryEditorConflict(t *testing.T) {
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("ok") }))
	chef := h.a.bots[0]
	m := h.a.memoryOf(chef)
	m.Update("append", "likes tea", "", "x")
	h.a.inspector = true
	h.tt.Frame()
	if err := h.tt.Click("Edit…"); err != nil { // the bot's; memory's is the second
		t.Fatal(err)
	}
	h.a.botSheet = nil
	text := m.Read()
	h.a.memSheet = &memoryForm{open: true, bot: chef, was: text, text: text + "- edited by hand\n"}
	m.Update("append", "likes jazz", "", "x") // the bot writes meanwhile
	h.tt.Frame()
	h.tt.Click("Save")
	h.tt.Frame()
	if h.a.memSheet == nil || !h.a.memSheet.changed || !h.tt.HasText("Overwrite") {
		t.Fatalf("no conflict: %q", h.tt.Texts())
	}
	h.tt.Click("Overwrite")
	h.tt.Frame()
	h.tt.Click("Save")
	h.tt.Frame()
	if h.a.memSheet != nil || !strings.Contains(m.Read(), "edited by hand") {
		t.Fatalf("not saved: %q", m.Read())
	}
}

func TestSearchRowsFromEntryAndEvents(t *testing.T) {
	call := agent.Part{Kind: agent.PartServer, Name: "web_search", Raw: json.RawMessage(`{"type":"server_tool_use","id":"s1","name":"web_search","input":{"query":"kyoto"}}`)}
	res := agent.Part{Kind: agent.PartServer, Name: "web_search_tool_result", Raw: json.RawMessage(`{"type":"web_search_tool_result","tool_use_id":"s1","content":[{"type":"web_search_result"}]}`)}
	rows := entryRows(store.Entry{Seq: 1, Msg: agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopEnd, Parts: []agent.Part{call, res, {Kind: agent.PartText, Text: "Mild."}}}})
	if len(rows) != 1 || len(rows[0].searches) != 1 || searchText(rows[0].searches[0]) != "Searched the web for “kyoto”" {
		t.Fatalf("rows %+v", rows)
	}
	v := &chatView{stream: map[string]int{}}
	v.applyAgent(team.Event{Turn: "t", BotID: "b", Agent: agent.Event{Kind: agent.EventMessageStart}})
	v.applyAgent(team.Event{Turn: "t", Agent: agent.Event{Kind: agent.EventServerTool, Call: &call}})
	if s := searchText(v.rows[0].searches[0]); s != "Searching the web for “kyoto”…" {
		t.Fatalf("live: %s", s)
	}
	v.applyAgent(team.Event{Turn: "t", Agent: agent.Event{Kind: agent.EventServerTool, Call: &res}})
	if !v.rows[0].searches[0].done {
		t.Fatal("result not applied")
	}
}

func TestRoutineInspectorAndSheet(t *testing.T) {
	ran := make(chan struct{}, 1)
	h := newHarness(t, script(func(bot string, msgs []agent.Message) agent.Message {
		select {
		case ran <- struct{}{}:
		default:
		}
		return say("PASS")
	}))
	chef := h.a.bots[0]
	r, _ := h.st.SaveRoutine(store.Routine{BotID: chef.ID, Name: "Brief", Prompt: "Summarize", Schedule: "0 8 * * 1-5", Timezone: "UTC", Enabled: true})
	h.a.inspector = true
	h.tt.Frame()
	time.Sleep(250 * time.Millisecond) // it slides open
	h.tt.Frame()
	if !h.tt.HasText("Brief") || !h.tt.HasText("Weekdays at 8:00 · next ") {
		t.Fatalf("inspector: %q", h.tt.Texts())
	}
	h.tt.Click("Brief")
	h.tt.Frame()
	if h.a.routine == nil || !h.tt.HasText("Run Now") {
		t.Fatalf("no sheet: %q", h.tt.Texts())
	}
	h.tt.Click("Pause")
	h.tt.Frame()
	if got, _ := h.st.Routine(r.ID); got.Enabled {
		t.Fatal("not paused")
	}
	h.tt.Click("Run Now")
	h.pump("the run", func() bool { return len(ran) == 1 && h.idle() && h.tt.HasText("Routine · Brief") })
	if h.tt.HasText("PASS") {
		t.Fatal("a pass shows")
	}
}

func TestUnreadAndNotification(t *testing.T) {
	h := newHarness(t, script(func(bot string, msgs []agent.Message) agent.Message { return say("Report ready.") }))
	var notes []string
	h.a.notify = func(title, subtitle, body, chatID string) { notes = append(notes, title+": "+body) }
	chef := h.a.bots[0]
	scout, _ := h.st.CreateBot(store.Bot{Name: "Scout", Icon: "brain"})
	h.a.reload()
	dmScout, _ := h.st.DMOf(scout.ID)
	h.a.selectChat(dmScout.ID) // the user looks at Scout while Chef works
	dmChef, _ := h.st.DMOf(chef.ID)
	h.a.team.Send(dmChef.ID, "write the report", nil, nil)
	h.pump("the reply", func() bool { return h.idle() && h.a.unread[dmChef.ID] == 1 })
	h.tt.Frame()
	if len(notes) != 1 || notes[0] != "Chef: Report ready." || !h.tt.HasText("1 unread") {
		t.Fatalf("notes %q texts %q", notes, h.tt.Texts())
	}
	h.a.selectChat(dmChef.ID)
	if h.a.unread[dmChef.ID] != 0 || h.st.Unread(dmChef.ID) != 0 {
		t.Fatal("not read")
	}
}

// reviewing answers reviews by the command and turns by a script.
type reviewing struct{ turn script }

func (r reviewing) Call(ctx context.Context, system string, msgs []agent.Message, tools []tool.Tool, emit func(agent.Event)) agent.Message {
	if strings.HasPrefix(system, "You review one action") {
		text := lastText(msgs)
		answer := `{"decision":"allow","reason":"Lists files."}`
		if strings.Contains(text, "rm -rf") {
			answer = `{"decision":"ask","reason":"Deletes a folder."}`
		}
		return agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopEnd, Parts: []agent.Part{{Kind: agent.PartText, Text: answer}}}
	}
	return r.turn.Call(ctx, system, msgs, tools, emit)
}

func TestAutoReviewAndAlwaysAllow(t *testing.T) {
	h := newHarness(t, reviewing{script(func(bot string, msgs []agent.Message) agent.Message {
		if l := msgs[len(msgs)-1]; l.Role == agent.RoleTool {
			return say("result: " + l.Parts[0].Text)
		}
		return agent.Message{StopReason: agent.StopToolUse, Parts: []agent.Part{{Kind: agent.PartToolCall, CallID: store.NewID("c"), Name: "shell",
			Args: json.RawMessage(`{"command":"` + lastText(msgs) + `"}`)}}}
	})})
	h.a.prefs.commands = ModeReview
	h.a.applyPrefs()
	h.send("ls")
	h.pump("ls ran", func() bool { return h.tt.HasText(`result: ran {"command":"ls"}`) && h.idle() })

	h.send("rm -rf build")
	h.pump("the review's question", func() bool { return h.tt.HasText("Auto-review: Deletes a folder.") })
	if err := h.tt.Click("Always Allow"); err != nil {
		t.Fatal(err)
	}
	h.pump("rm ran", func() bool { return h.tt.HasText(`result: ran {"command":"rm -rf build"}`) && h.idle() })
	if !strings.Contains(h.st.Setting("allow_rules"), "shell:rm -rf build") {
		t.Fatalf("rules %q", h.st.Setting("allow_rules"))
	}
	h.send("rm -rf build") // allowed by the rule, no question
	h.pump("rm ran again", func() bool { return h.idle() && len(h.a.approvals.in(h.a.sel)) == 0 })
}

func TestSetupThenRetry(t *testing.T) {
	h := newHarness(t, script(func(bot string, msgs []agent.Message) agent.Message { return say("ready") }))
	h.a.fallback = connect.Config{}
	h.tt.Frame()
	if !h.tt.HasText("Set up a model") {
		t.Fatalf("no setup: %q", h.tt.Texts())
	}
	h.tt.Type("sk-test")
	h.tt.Frame()
	if err := h.tt.Click("Start"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	ps, _ := h.st.Providers()
	if len(ps) != 1 || ps[0].Name != "DeepSeek" || ps[0].APIKey != "sk-test" || !ps[0].Options["web_search"] || h.st.Setting(team.SettingDefaultProvider) != ps[0].ID {
		t.Fatalf("providers %+v", ps)
	}
	if h.tt.HasText("Set up a model") {
		t.Fatal("setup still shown")
	}

	// A reply that failed for want of a model offers Retry, which runs it again.
	failing := true
	h.a.team.Model = func(store.Bot) (agent.Model, error) {
		if failing {
			return nil, team.ErrNoProvider
		}
		return script(func(string, []agent.Message) agent.Message { return say("here now") }), nil
	}
	h.send("hello")
	h.pump("the failure", func() bool { return h.idle() && h.tt.HasText("Retry") })
	if !h.tt.HasText("No model is set up yet. Add a provider in Settings, then retry.") || !h.tt.HasText("Open Settings") {
		t.Fatalf("texts %q", h.tt.Texts())
	}
	failing = false
	h.tt.Click("Retry")
	h.pump("the retried reply", func() bool { return h.idle() && h.tt.HasText("here now") })
	if h.tt.HasText("Retry") {
		t.Fatal("Retry stays on an older failure")
	}
}

func TestLanguageInSettings(t *testing.T) {
	defer setLanguage("English", "")
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("ok") }))
	h.a.openSettings("")
	st := ui.NewTester(h.a.settingsRoot, 800, 580)
	if !h.tt.HasText("Search chats") || !st.HasText("General") || !st.HasText("Follow System") {
		t.Fatalf("no language field: %q", st.Texts())
	}
	if err := st.Click("Follow System"); err != nil {
		t.Fatal(err)
	}
	if err := st.ChooseMenuItem("简体中文"); err != nil {
		t.Fatal(err)
	}
	st.Frame()
	st.Frame()
	h.tt.Frame()
	if h.a.prefs.language != "中文" || !st.HasText("通用") || !h.tt.HasText("搜索聊天") || h.st.Setting("pref.language") != "中文" {
		t.Fatalf("language %q settings %q", h.a.prefs.language, st.Texts())
	}
}

func TestSettingsModelsAndCommands(t *testing.T) {
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("ok") }))
	h.a.openSettings(paneModels)
	st := ui.NewTester(h.a.settingsRoot, 800, 580)
	if !st.HasText("Add a provider to give your bots a model.") {
		t.Fatalf("models: %q", st.Texts())
	}
	if err := st.Click("Add Provider…"); err != nil {
		t.Fatal(err)
	}
	st.Frame()
	h.a.set.prov.name, h.a.set.prov.key, h.a.set.prov.models = "DeepSeek", "sk-x", "deepseek-flash, deepseek-v4-pro"
	st.Frame()
	if err := st.Click("Create"); err != nil {
		t.Fatal(err)
	}
	st.Frame()
	ps, _ := h.st.Providers()
	if len(ps) != 1 || len(ps[0].Models) != 2 || h.st.Setting(team.SettingDefaultProvider) != ps[0].ID || !st.HasText("Saved") || !st.HasText("Default") {
		t.Fatalf("providers %+v texts %q", ps, st.Texts())
	}
	// Back to the list, and into the provider again.
	if err := st.Click("Back to Models"); err != nil {
		t.Fatal(err)
	}
	st.Frame()
	if !st.HasText("deepseek-flash, deepseek-v4-pro · Anthropic Messages") || st.HasText("Test Connection") {
		t.Fatalf("list: %q", st.Texts())
	}
	if err := st.Click("DeepSeek"); err != nil {
		t.Fatal(err)
	}
	st.Frame()
	h.a.set.prov.models = "deepseek-flash"
	st.Frame()
	if !st.HasText("Revert") || st.HasText("Saved") {
		t.Fatalf("an edit not shown: %q", st.Texts())
	}
	st.Click("Revert")
	st.Frame()
	if h.a.set.prov.models != "deepseek-flash, deepseek-v4-pro" {
		t.Fatal("not reverted")
	}
	if err := st.Click("Delete"); err != nil {
		t.Fatal(err)
	}
	st.Frame()
	if h.a.set.confirm == nil {
		t.Fatal("no confirmation")
	}
	h.a.set.confirm.run()
	h.a.set.confirm = nil // the alert closed
	if ps, _ := h.st.Providers(); len(ps) != 0 {
		t.Fatal("not deleted")
	}

	h.st.SetSetting("allow_rules", "shell:ls -la\ntool:github__create_issue")
	h.a.openSettings(paneCommands)
	st.Frame()
	if !st.HasText("ls -la") || !st.HasText("github__create_issue") {
		t.Fatalf("rules: %q", st.Texts())
	}
	if err := st.Click("Remove"); err != nil {
		t.Fatal(err)
	}
	st.Frame()
	if r := h.a.approvals.rules(); len(r) != 1 {
		t.Fatalf("rules %v", r)
	}
	st.Key(0, ui.KeyDown) // the list keeps no focus yet: nothing moves
	if err := st.Click("About"); err != nil {
		t.Fatal(err)
	}
	st.Frame()
	if !st.HasText(h.st.Dir()) {
		t.Fatalf("about: %q", st.Texts())
	}
}

func TestTestConnection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if r.Header.Get("authorization") != "Bearer good" {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":{"message":"invalid api key"}}`)
			return
		}
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	cfg := connect.Config{API: connect.Chat, BaseURL: srv.URL, APIKey: "good", Model: "m"}
	if ok, msg := checkProvider(cfg); !ok || msg != "Connected: m answered." {
		t.Fatalf("%v %q", ok, msg)
	}
	cfg.APIKey = "bad"
	if ok, msg := checkProvider(cfg); ok || !strings.Contains(msg, "invalid api key") {
		t.Fatalf("%v %q", ok, msg)
	}
}

func TestQuitIsNotHeldByKeepRunning(t *testing.T) {
	if !keepOpen(true, false) || keepOpen(true, true) || keepOpen(false, false) {
		t.Fatal("closing hides only while keeping running and not quitting")
	}
}

func TestBlankPressInDialogKeepsFieldsUnfocused(t *testing.T) {
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("ok") }))
	h.a.newBot()
	h.tt.Frame()
	h.tt.Frame()
	h.tt.Type("Scout") // the name field has the focus as the sheet opens
	h.tt.Frame()
	f := h.a.botSheet
	if f.name != "Scout" {
		t.Fatalf("name %q", f.name)
	}
	h.tt.Click("Description") // a field's label focuses its field, as forms do
	h.tt.Frame()
	h.tt.Type("Finds")
	h.tt.Frame()
	if f.description != "Finds" {
		t.Fatalf("description %q", f.description)
	}
	// A press on nothing: what is typed then goes nowhere.
	if err := h.tt.Click("A bot is a name, a description (its system prompt), the model it runs on, and a workspace folder of its own."); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	h.tt.Type("zz")
	h.tt.Frame()
	if f.name != "Scout" || f.description != "Finds" || h.a.botSheet == nil {
		t.Fatalf("name %q description %q", f.name, f.description)
	}
}

func TestRoutineNotificationsAndBadge(t *testing.T) {
	reply := "Two meetings today."
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say(reply) }))
	var notes []string
	h.a.notify = func(title, subtitle, body, chatID string) { notes = append(notes, title+": "+body) }
	badge := -1
	h.a.setBadge = func(n int) { badge = n }
	chef := h.a.bots[0]
	scout, _ := h.st.CreateBot(store.Bot{Name: "Scout"})
	h.a.reload()
	dmScout, _ := h.st.DMOf(scout.ID)
	h.a.selectChat(dmScout.ID) // looking elsewhere
	r, _ := h.st.SaveRoutine(store.Routine{BotID: chef.ID, Name: "Brief", Prompt: "Brief me", Schedule: "every 1h", Enabled: true})

	h.a.team.RunRoutine(r)
	h.pump("the run", func() bool { return h.idle() && len(notes) == 1 })
	if notes[0] != "Chef · Brief: Two meetings today." || badge != 1 {
		t.Fatalf("notes %q badge %d", notes, badge)
	}

	reply = "PASS" // nothing to report: no notification, nothing unread
	h.a.team.RunRoutine(r)
	h.pump("the quiet run", func() bool { return h.idle() })
	if len(notes) != 1 || badge != 1 {
		t.Fatalf("a pass notified: %q badge %d", notes, badge)
	}

	dmChef, _ := h.st.DMOf(chef.ID)
	h.a.selectChat(dmChef.ID)
	if badge != 0 {
		t.Fatalf("badge %d after reading", badge)
	}

	// Away a week since the user last wrote: the due routine pauses, and says so.
	h.st.Append(dmScout.ID, store.User, "", store.Meta{}, agent.Message{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: "hi"}}})
	h.a.selectChat(dmScout.ID)
	h.a.team.Now = func() time.Time { return time.Now().Add(8 * 24 * time.Hour) }
	h.a.team.TickRoutines()
	h.pump("the pause notice", func() bool { return len(h.posts) == 0 && len(notes) == 2 })
	if !strings.HasPrefix(notes[1], `Chef: Paused the routine "Brief"`) {
		t.Fatalf("notes %q", notes)
	}
}

func TestAttachDoesNotBlockTheFrame(t *testing.T) {
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("ok") }))
	img := filepath.Join(t.TempDir(), "a.png")
	os.WriteFile(img, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}, 0o644)
	chosen := make(chan struct{})
	// As main.go does: the dialog waits for the user off the main thread,
	// and its answer comes back through post.
	h.a.pickFiles = func(done func([]string)) {
		go func() {
			<-chosen // the user picks a while later
			h.a.post(func() { done([]string{img}) })
		}()
	}
	h.tt.Frame()
	if err := h.tt.Click("Attach files"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame() // the frame goes on while the dialog shows
	close(chosen)
	h.pump("the file", func() bool { return h.tt.HasText("a.png") })
}

func TestContextSettings(t *testing.T) {
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("ok") }))
	if w, c, ttl := h.a.compactionPolicy(); w != 0.8 || c != 0.4 || ttl != 5*time.Minute {
		t.Fatalf("defaults %v %v %v", w, c, ttl)
	}
	h.a.openSettings(paneModels)
	st := ui.NewTester(h.a.settingsRoot, 800, 900)
	if !st.HasText("Once the cache has expired") {
		t.Fatalf("no context settings: %q", st.Texts())
	}
	if err := st.Click("At 40% of the window"); err != nil {
		t.Fatal(err)
	}
	if err := st.ChooseMenuItem("At 30% of the window"); err != nil {
		t.Fatal(err)
	}
	st.Frame()
	if _, c, _ := h.a.compactionPolicy(); c != 0.3 || h.st.Setting("pref.compact_cold") != "0.30" {
		t.Fatalf("cold %v, stored %q", c, h.st.Setting("pref.compact_cold"))
	}
	if p := loadPrefs(h.st); p.compactCold != 0.3 || p.compactWarm != 0.8 || p.cacheTTL != 5*time.Minute {
		t.Fatalf("reloaded %+v", p)
	}
}

func TestSidebarPreviewAndPin(t *testing.T) {
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("Soup tonight.") }))
	scout, _ := h.st.CreateBot(store.Bot{Name: "Scout", Description: "Finds things."})
	h.a.reload()
	h.send("dinner?")
	h.pump("the reply", func() bool { return h.tt.HasText("Soup tonight.") && h.idle() })
	h.tt.Frame()
	if !h.tt.HasText("No messages yet") || !h.tt.HasText(time.Now().Format("15:04")) {
		t.Fatalf("previews: %q", h.tt.Texts())
	}

	// Pinning Scout puts it first, under Pinned.
	if err := h.tt.RightClick("Scout"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem("Pin"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if list := h.a.shownChats(); list[0].Owner != scout.ID || !h.tt.HasText("Pinned") {
		t.Fatalf("not pinned first: %+v", list)
	}

	if err := h.tt.RightClick("Scout"); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(h.tt.Menu(), "Rename") || !slices.Contains(h.tt.Menu(), "Unpin") {
		t.Fatalf("menu %q", h.tt.Menu())
	}
	h.tt.CloseMenu()
}

func TestBotAvatarPicture(t *testing.T) {
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("ok") }))
	chef := h.a.bots[0]
	if colorName(chef) == "" || colorName(store.Bot{ID: chef.ID, Color: "teal"}) != "teal" {
		t.Fatal("colors")
	}
	pic := filepath.Join(t.TempDir(), "me.png")
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 900, 600)))
	os.WriteFile(pic, buf.Bytes(), 0o644)
	h.a.pickImage = func(done func(string)) { h.a.post(func() { done(pic) }) }
	h.a.editBot(chef.ID)
	h.tt.Frame()
	if err := h.tt.Click("Choose Picture…"); err != nil {
		t.Fatal(err)
	}
	h.pump("the picture", func() bool { return h.a.botSheet.avatar != "" })
	if err := h.tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	b, _ := h.st.Bot(chef.ID)
	data, err := os.ReadFile(h.st.AvatarPath(b.Avatar))
	cfg, _, _ := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != 512 || cfg.Height != 512 {
		t.Fatalf("avatar %q %dx%d %v", b.Avatar, cfg.Width, cfg.Height, err)
	}
	h.pump("the bitmap", func() bool { return h.a.picture(h.st.AvatarPath(b.Avatar)) != nil })

	// Use Icon, then Cancel: the stored picture stays.
	h.a.editBot(chef.ID)
	h.tt.Frame()
	h.tt.Click("Use Icon")
	h.tt.Frame()
	h.tt.Click("Cancel")
	h.tt.Frame()
	if b2, _ := h.st.Bot(chef.ID); b2.Avatar != b.Avatar {
		t.Fatalf("cancel changed the avatar to %q", b2.Avatar)
	}
	if _, err := os.Stat(h.st.AvatarPath(b.Avatar)); err != nil {
		t.Fatal("cancel removed the stored picture")
	}
}

func TestStamp(t *testing.T) {
	now := time.Date(2026, 10, 10, 15, 0, 0, 0, time.Local) // a Saturday
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{now.Add(-time.Hour), "14:00"},
		{now.AddDate(0, 0, -1), "Yesterday"},
		{now.AddDate(0, 0, -3), "Wed"},
		{now.AddDate(0, -1, 0), "9/10"},
		{now.AddDate(-1, 0, 0), "25/10/10"},
	} {
		if got := stamp(now, c.t); got != c.want {
			t.Errorf("stamp(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestAttachedPictureShowsAsAThumbnail(t *testing.T) {
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("nice") }))
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1200, 600)))
	if err := h.a.team.SendFiles(h.a.sel, "", []team.Attachment{{Name: "wide.png", Data: buf.Bytes()}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	h.pump("the reply", func() bool { return h.tt.HasText("nice") && h.idle() })
	es, _ := h.st.Messages(h.a.sel, 0)
	h.pump("the thumbnail", func() bool { return h.a.thumb(es[0].Meta.Files[0]) != nil })
	h.tt.Frame()
	if r, ok := h.tt.Find("wide.png"); !ok || r.W != 240 || r.H != 120 {
		t.Fatalf("thumbnail at %+v %v", r, ok)
	}
}

func TestProviderModelsFromTheServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"qwen3"},{"id":"llama4","context_length":131072},{"id":"nomic-embed-text"}]}`))
	}))
	defer srv.Close()
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("ok") }))
	h.a.openSettings(paneModels)
	st := ui.NewTester(h.a.settingsRoot, 800, 900)
	if err := st.Click("Add Provider…"); err != nil {
		t.Fatal(err)
	}
	st.Frame()
	f := h.a.set.prov
	f.name, f.api, f.base = "Local", apiLabel(connect.Chat), srv.URL+"/v1"
	st.Frame()
	if err := st.Click("Load from Server"); err != nil {
		t.Fatal(err)
	}
	h.pump("the list", func() bool { return !f.listing })
	st.Frame()
	if !st.HasText("2 models; check the ones to use.") || !st.HasText("131K") || st.HasText("nomic-embed-text") || f.models != "qwen3" {
		t.Fatalf("list %q, models %q", st.Texts(), f.models)
	}
	if err := st.Click("llama4"); err != nil {
		t.Fatal(err)
	}
	st.Frame()
	st.Click("qwen3")
	st.Frame()
	if f.models != "llama4" {
		t.Fatalf("models %q", f.models)
	}
	if toggleModel("a, b", "c") != "a, b, c" || windowText(1048576) != "1M" || windowText(200000) != "200K" {
		t.Fatal("helpers")
	}
}

func TestNewProviderListsModelsOnceTheURLRests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"qwen3"}]}`))
	}))
	defer srv.Close()
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("ok") }))
	h.a.openSettings(paneModels)
	st := ui.NewTester(h.a.settingsRoot, 800, 900)
	st.Click("Add Provider…")
	st.Frame()
	f := h.a.set.prov
	f.api, f.base = apiLabel(connect.Chat), srv.URL+"/v1"
	st.Frame()
	if f.listing || f.listed != nil {
		t.Fatal("listed while typing")
	}
	time.Sleep(listRest + 100*time.Millisecond)
	st.Frame()
	h.pump("the list", func() bool { return f.listed != nil })
	if f.models != "qwen3" {
		t.Fatalf("models %q", f.models)
	}
}

func TestMessageTimeShowsOnHover(t *testing.T) {
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("Soup.\n\nAnd bread.") }))
	h.send("dinner?")
	h.pump("the reply", func() bool { return h.tt.HasText("Soup.") && h.idle() })
	now := time.Now().Format("15:04")
	h.tt.Move(1, 1)
	h.tt.Frame()
	h.tt.Frame()
	stamps := func() int {
		n := 0
		for _, s := range h.tt.Texts() {
			if s == now {
				n++
			}
		}
		return n
	}
	before := stamps() // the sidebar's row shows the time already
	for _, text := range []string{"dinner?", "And bread."} {
		r, ok := h.tt.Find(text)
		if !ok {
			t.Fatalf("no %q", text)
		}
		h.tt.Move(r.X+2, r.Y+2)
		h.tt.Frame()
		h.tt.Frame()
		if stamps() != before+1 {
			t.Fatalf("hovering %q: %d stamps, %d before; %q", text, stamps(), before, h.tt.Texts())
		}
	}
}

func TestClock(t *testing.T) {
	now := time.Date(2026, 10, 10, 15, 0, 0, 0, time.Local)
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{now.Add(-time.Hour), "14:00"},
		{now.AddDate(0, 0, -1), "Yesterday 15:00"},
		{now.AddDate(0, 0, -5), "10/5 15:00"},
		{now.AddDate(-1, 0, 0), "2025/10/10 15:00"},
	} {
		if got := clock(now, c.t); got != c.want {
			t.Errorf("clock(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestMCPServerEditor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			ID     json.RawMessage
			Method string
		}
		json.NewDecoder(r.Body).Decode(&m)
		if len(m.ID) == 0 {
			w.WriteHeader(202)
			return
		}
		res := map[string]any{"tools": []map[string]any{{"name": "search", "description": "Searches the docs", "inputSchema": map[string]any{"type": "object"}}}}
		if m.Method == "initialize" {
			res = map[string]any{"protocolVersion": mcp.ProtocolVersion, "serverInfo": map[string]string{"name": "docs"}, "capabilities": map[string]any{"resources": map[string]any{}}}
		}
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": res})
	}))
	defer srv.Close()
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("ok") }))
	path := filepath.Join(t.TempDir(), "mcp.json")
	os.WriteFile(path, []byte(`{"mcpServers": {"Old": {"command": "nope-not-a-command", "alwaysAllow": ["x"]}}}`), 0o644)
	h.a.team.Plugins, h.a.mcpPath = mcp.NewManager(path), path
	defer h.a.team.Plugins.Close()
	h.a.openSettings(paneServers)
	st := ui.NewTester(h.a.settingsRoot, 800, 900)
	h.pump("the check", func() bool { return !h.a.checking })
	st.Frame()
	if !st.HasText("Old") {
		t.Fatalf("list %q", st.Texts())
	}

	// A new remote server.
	if err := st.Click("Add Server…"); err != nil {
		t.Fatal(err)
	}
	st.Frame()
	f := h.a.set.srv
	f.name, f.kind, f.url, f.headers = "Docs", serverKinds[1], srv.URL, "Authorization: Bearer x"
	st.Frame()
	if err := st.Click("Test Connection"); err != nil {
		t.Fatal(err)
	}
	h.pump("the test", func() bool { return !f.testing })
	if !f.testOK || f.test != "Connected: 1 tools and resources." {
		t.Fatalf("test %q", f.test)
	}
	st.Click("Create")
	st.Frame()
	h.pump("the servers", func() bool {
		for _, s := range h.a.servers {
			if s.Server.Name == "Docs" && s.State == "ready" {
				return true
			}
		}
		return false
	})
	st.Frame()
	if h.a.set.srvSel != "Docs" || !st.HasText("Saved") || !st.HasText("search") || !st.HasText("1 tools, resources") {
		t.Fatalf("page %q", st.Texts())
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"Authorization": "Bearer x"`) || !strings.Contains(string(b), "alwaysAllow") {
		t.Fatalf("file %s", b)
	}

	// Back, then Old: rename it and save.
	st.Key(0, ui.KeyEscape)
	st.Frame()
	st.Click("Old")
	st.Frame()
	h.a.set.srv.name, h.a.set.srv.args = "Older", "--flag\n  two words  "
	st.Frame()
	st.Click("Save")
	st.Frame()
	b, _ = os.ReadFile(path)
	ss, _ := mcp.Parse(b)
	if len(ss) != 2 || ss[1].Name != "Older" || len(ss[1].Args) != 2 || ss[1].Args[1] != "two words" || !strings.Contains(string(b), "alwaysAllow") {
		t.Fatalf("after rename %+v %s", ss, b)
	}
	// Delete it.
	st.Click("Delete")
	st.Frame()
	h.a.set.confirm.run()
	h.a.set.confirm = nil
	b, _ = os.ReadFile(path)
	if ss, _ := mcp.Parse(b); len(ss) != 1 || h.a.set.srvSel != "" {
		t.Fatalf("after delete %+v", ss)
	}
	if _, err := (&serverForm{kind: serverKinds[0], name: "x", command: "x", env: "NOEQUALS"}).server(); err == nil {
		t.Fatal("a bad env line")
	}
}

// slowTool reports a line, then waits to be let go.
type slowTool struct{ release chan struct{} }

func (slowTool) Name() string            { return "find" }
func (slowTool) Description() string     { return "finds slowly" }
func (slowTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s slowTool) Execute(ctx context.Context, _ json.RawMessage) tool.Result {
	tool.Update(ctx, tool.Result{Text: "scanning src/\nfound 3 so far"})
	<-s.release
	return tool.Result{Text: "found 5"}
}

func TestRunningToolShowsWhatItHasSoFar(t *testing.T) {
	h := newHarness(t, script(func(bot string, msgs []agent.Message) agent.Message {
		if msgs[len(msgs)-1].Role == agent.RoleTool {
			return say("all done")
		}
		return agent.Message{StopReason: agent.StopToolUse, Parts: []agent.Part{{Kind: agent.PartToolCall, CallID: "c1", Name: "find", Args: json.RawMessage(`{}`)}}}
	}))
	slow := slowTool{release: make(chan struct{})}
	h.a.team.Tools = func(store.Bot, string) []tool.Tool { return []tool.Tool{slow} }
	h.send("look")
	h.pump("the partial output", func() bool { return h.tt.HasText("scanning src/\nfound 3 so far") })
	close(slow.release)
	h.pump("the end", func() bool { return h.tt.HasText("all done") && h.idle() })
	if h.tt.HasText("scanning src/\nfound 3 so far") {
		t.Fatal("the partial output stays after the end")
	}
}

func TestBotFolder(t *testing.T) {
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("ok") }))
	chef := h.a.bots[0]
	project := t.TempDir()
	h.a.pickFolder = func(done func(string)) { h.a.post(func() { done(project) }) }
	h.a.editBot(chef.ID)
	h.tt.Frame()
	if err := h.tt.Click("Choose…"); err != nil {
		t.Fatal(err)
	}
	h.pump("the folder", func() bool { return h.a.botSheet.workdir == project })
	h.tt.Click("Save")
	h.tt.Frame()
	if b, _ := h.st.Bot(chef.ID); h.st.WorkDir(b) != project {
		t.Fatalf("workdir %q", b.Workdir)
	}
	// A folder that is not there is refused, and the sheet says so.
	h.a.editBot(chef.ID)
	h.tt.Frame()
	h.a.botSheet.workdir = filepath.Join(project, "missing")
	h.tt.Frame()
	h.tt.Click("Save")
	h.tt.Frame()
	if h.a.botSheet.err == "" || !h.a.botSheet.open {
		t.Fatal("a missing folder saved")
	}
}

func TestInspectorSlides(t *testing.T) {
	h := newHarness(t, script(func(string, []agent.Message) agent.Message { return say("ok") }))
	width := func() float32 {
		r, ok := h.tt.Find("Spent in this chat")
		if !ok {
			return 0
		}
		return 1000 - r.X // how far in from the window's right edge
	}
	h.a.inspector = true
	h.tt.Frame()
	h.tt.Frame()
	mid := width()
	time.Sleep(250 * time.Millisecond)
	h.tt.Frame()
	open := width()
	if !(mid < open) || open < 200 {
		t.Fatalf("not sliding: %v then %v", mid, open)
	}
	h.a.inspector = false
	h.tt.Frame()
	h.tt.Frame()
	if h.tt.HasText("Spent in this chat") && width() >= open {
		t.Fatal("closed at once or not at all")
	}
	time.Sleep(250 * time.Millisecond)
	h.tt.Frame()
	if h.tt.HasText("Spent in this chat") {
		t.Fatal("still open")
	}
	h.tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	h.a.inspector = true
	h.tt.Frame()
	h.tt.Frame()
	if width() < 200 {
		t.Fatalf("Reduce Motion slides: %v", width())
	}
}
