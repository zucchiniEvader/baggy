package team

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/mcp"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/tool"
)

func TestCodemodeToolCallsOwnToolsAndKeepsValues(t *testing.T) {
	var results []string
	f := setup(t, script(func(bot string, msgs []agent.Message) agent.Message {
		if l := msgs[len(msgs)-1]; l.Role == agent.RoleTool {
			results = append(results, l.Parts[0].Text)
			return say("done")
		}
		code := `const n = (load("runs") || 0) + 1; store("runs", n);
const files = await Promise.all(["a.txt", "b.txt"].map(p => tools.read_file({ path: p })));
await tools.write_file({ path: "out.txt", content: files.join("+") });
return { runs: n, out: await tools.read_file({ path: "out.txt" }) }`
		return call("codemode", map[string]string{"code": code})
	}))
	f.team.Codemode = true
	f.team.Tools = func(b store.Bot, ws string) []tool.Tool {
		return []tool.Tool{tool.ReadFile{Dir: ws}, tool.WriteFile{Dir: ws}}
	}
	a := f.bot(t, "A")
	ws := f.st.Workspace(a.ID)
	os.WriteFile(filepath.Join(ws, "a.txt"), []byte("one"), 0o644)
	os.WriteFile(filepath.Join(ws, "b.txt"), []byte("two"), 0o644)
	dm, _ := f.st.DMOf(a.ID)
	f.team.Send(dm.ID, "go", nil, nil)
	f.team.Wait()
	f.team.Send(dm.ID, "again", nil, nil)
	f.team.Wait()
	if len(results) != 2 || !strings.Contains(results[0], `"out": "one+two"`) || !strings.Contains(results[1], `"runs": 2`) {
		t.Fatalf("results %q", results)
	}
}

func TestChecksOnlyRead(t *testing.T) {
	f := setup(t, script(func(string, []agent.Message) agent.Message { return say("x") }))
	f.team.Tools = func(b store.Bot, ws string) []tool.Tool {
		return []tool.Tool{tool.Grep{Dir: ws}, tool.WriteFile{Dir: ws}}
	}
	a := f.bot(t, "A")
	os.WriteFile(filepath.Join(f.st.Workspace(a.ID), "inbox.txt"), []byte("hello\nURGENT: call back\n"), 0o644)
	found, err := f.team.RunCheck(context.Background(), store.Routine{BotID: a.ID, Check: `
const hits = await tools.grep({ pattern: "URGENT" });
const seen = load("seen"); store("seen", hits);
return hits === seen ? null : hits`})
	if err != nil || !strings.Contains(found, "inbox.txt:2: URGENT") {
		t.Fatalf("check: %q %v", found, err)
	}
	again, _ := f.team.RunCheck(context.Background(), store.Routine{BotID: a.ID, Check: `const hits = await tools.grep({ pattern: "URGENT" }); return hits === load("seen") ? null : hits`})
	if again != "" {
		t.Fatalf("nothing new, yet %q", again)
	}
	if _, err := f.team.RunCheck(context.Background(), store.Routine{BotID: a.ID, Check: `await tools.write_file({ path: "x", content: "y" })`}); err == nil || !strings.Contains(err.Error(), "write_file") {
		t.Fatalf("a check wrote: %v", err)
	}
}

func TestPluginsPrompt(t *testing.T) {
	p := PluginsPrompt([]mcp.Status{
		{Server: mcp.Server{Name: "GitHub", Description: "The user's repos."}, ID: "github", State: "ready", Tools: []mcp.Tool{{Name: "list_issues", Description: "List issues\nmore"}}},
		{Server: mcp.Server{Name: "Mail"}, ID: "mail", State: "failed", Err: "starting npx: not found"},
		{Server: mcp.Server{Name: "Off"}, ID: "off", State: "off"},
	})
	for _, want := range []string{"### GitHub (github)", "The user's repos.", "- github__list_issues: List issues", "Mail (mail): not available: starting npx"} {
		if !strings.Contains(p, want) {
			t.Fatalf("missing %q in\n%s", want, p)
		}
	}
	if strings.Contains(p, "Off") || PluginsPrompt(nil) != "" {
		t.Fatal(p)
	}
}

func TestCodemodeShowsWhatItPrintsAsItRuns(t *testing.T) {
	f := setup(t, script(func(string, []agent.Message) agent.Message { return say("x") }))
	f.team.Codemode = true
	a := f.bot(t, "A")
	dm, _ := f.st.DMOf(a.ID)
	tl := f.team.codemodeTool(a, dm, nil, agent.Hooks{}, nil)
	var seen []string
	ctx := tool.WithUpdate(context.Background(), func(r tool.Result) { seen = append(seen, r.Text) })
	r := tl.Execute(ctx, json.RawMessage(`{"code":"console.log('step 1'); console.log('step 2'); return 'ok'"}`))
	if r.IsError || len(seen) != 2 || seen[1] != "step 1\nstep 2\n" {
		t.Fatalf("result %+v, updates %q", r, seen)
	}
	p := &scriptProgress{ctx: ctx}
	p.print("a\n")
	p.call("c1", "github__search: 1/2 halfway")
	if seen[len(seen)-1] != "a\ngithub__search: 1/2 halfway\n" {
		t.Fatalf("with a call %q", seen[len(seen)-1])
	}
	p.done("c1")
	p.print("a\nb\n")
	if seen[len(seen)-1] != "a\nb\n" {
		t.Fatalf("after the call %q", seen[len(seen)-1])
	}
}
