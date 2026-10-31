package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/host"
	"github.com/zucchiniEvader/baggy/provider/connect"
	"github.com/zucchiniEvader/baggy/remote"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/team"
	"github.com/zucchiniEvader/baggy/tool"
)

// newRemoteHarness is an app whose bots run on a server: a host behind
// remote.Server, reached over HTTP as another machine would reach it.
func newRemoteHarness(t *testing.T, m agent.Model) *harness {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st.Seed()
	srv := &remote.Server{Token: remote.NewToken(), Name: "Box"}
	tm := &team.Team{Store: st, Model: func(store.Bot) (agent.Model, error) { return m, nil },
		Tools: func(store.Bot, string) []tool.Tool { return []tool.Tool{echoTool{}} }, Emit: srv.Publish, Fallback: connect.Config{APIKey: "test"}}
	srv.Host = host.New(st, tm)
	hs := httptest.NewServer(srv.Handler())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); tm.Wait(); srv.Stop(); hs.Close(); st.Close() })

	h := &harness{t: t, st: st, tm: tm, host: srv.Host, posts: make(chan func(), 1024)}
	rb := newRemoteBackend(&remote.Client{Base: hs.URL, Token: srv.Token, Name: "Box"}, t.TempDir())
	h.a = newApp(remoteStore{rb}, remoteHost{rb}, func(fn func()) { h.posts <- fn })
	h.a.remote, h.a.readFile = rb, rb.readFile
	up := make(chan struct{}, 1)
	go rb.follow(ctx, func(e team.Event) { h.a.post(func() { h.a.apply(e) }) }, func(err error) {
		if err == nil {
			select {
			case up <- struct{}{}:
			default:
			}
		}
	})
	<-up
	h.a.applyPrefs()
	h.tt = ui.NewTester(h.a.root, 1000, 700)
	return h
}

func TestRemoteChatAndApproval(t *testing.T) {
	h := newRemoteHarness(t, script(func(bot string, msgs []agent.Message) agent.Message {
		if l := msgs[len(msgs)-1]; l.Role == agent.RoleTool {
			return say("result: " + l.Parts[0].Text)
		}
		if lastText(msgs) == "hello" {
			return say("Hi from the server")
		}
		return agent.Message{StopReason: agent.StopToolUse, Parts: []agent.Part{{Kind: agent.PartToolCall, CallID: "c1", Name: "shell", Args: json.RawMessage(`{"command":"ls"}`)}}}
	}))
	h.tt.Frame()
	if !h.tt.HasText("Chef") || !h.tt.HasText("Box") {
		t.Fatalf("no chats or server: %q", h.tt.Texts())
	}
	h.send("hello")
	h.pump("the reply", func() bool { return h.tt.HasText("Hi from the server") && h.idle() })

	// The mode is the server's setting; the card answers the server's hold.
	h.a.prefs.commands = ModeAsk
	h.a.applyPrefs()
	if h.st.Setting("pref.commands") != ModeAsk {
		t.Fatal("the mode did not reach the server")
	}
	h.send("list files")
	h.pump("the approval card", func() bool { return h.tt.HasText("Allow") })
	if err := h.tt.Click("Allow"); err != nil {
		t.Fatal(err)
	}
	h.pump("the result", func() bool { return h.tt.HasText(`result: ran {"command":"ls"}`) && h.idle() })

	// A bot made here is the server's.
	if _, err := h.a.st.CreateBot(store.Bot{Name: "Scout"}); err != nil {
		t.Fatal(err)
	}
	h.pump("the new bot", func() bool { return h.tt.HasText("Scout") })
	if bs, _ := h.st.BotsNamed("Scout"); len(bs) != 1 {
		t.Fatal("not on the server")
	}
}

func TestRemoteOffline(t *testing.T) {
	rb := newRemoteBackend(&remote.Client{Base: "http://127.0.0.1:1", Token: "x", Name: "Gone"}, t.TempDir())
	var bots []store.Bot
	if err := rb.read("Store.Bots", nil, &bots); err == nil {
		t.Fatal("reached nothing")
	}
	if rb.offline() == "" {
		t.Fatal("not offline after a refused call")
	}
	if err := (remoteStore{rb}).SetSetting("k", "v"); err == nil || err.Error() != "Gone cannot be reached" {
		t.Fatalf("write offline: %v", err)
	}
}
