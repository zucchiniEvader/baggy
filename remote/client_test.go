package remote

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/host"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/team"
)

func client(s *Server, base string) *Client { return &Client{Base: base, Token: s.Token} }

func TestCallReachesStoreAndHost(t *testing.T) {
	s, hs, dm := setup(t)
	c := client(s, hs.URL)
	var bots []store.Bot
	if err := c.Call("Store.Bots", nil, &bots); err != nil || len(bots) != 1 || bots[0].Name != "Chef" {
		t.Fatalf("bots %+v, %v", bots, err)
	}
	var b store.Bot
	if err := c.Call("Store.CreateBot", []any{store.Bot{Name: "Sous"}}, &b); err != nil || b.ID == "" {
		t.Fatalf("create: %+v, %v", b, err)
	}
	var snap host.Snapshot
	if err := c.Call("Host.Snapshot", nil, &snap); err != nil || len(snap.Bots) != 2 {
		t.Fatalf("snapshot %+v, %v", snap, err)
	}
	if err := c.Call("Store.Chat", []any{"nope"}, new(store.Chat)); err == nil {
		t.Error("an unknown chat gave no error")
	}
	if err := c.Call("Store.Close", nil); err == nil || !strings.Contains(err.Error(), "no method") {
		t.Errorf("Close: %v", err)
	}
	if err := c.Call("Store.Chat", []any{dm.ID, 1}); err == nil {
		t.Error("too many arguments")
	}
}

func TestFullEvents(t *testing.T) {
	s, hs, dm := setup(t)
	c := client(s, hs.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var mu sync.Mutex
	var got []team.Event
	up := make(chan struct{}, 1)
	go c.Events(ctx, func(e team.Event) { mu.Lock(); got = append(got, e); mu.Unlock() }, func(err error) {
		if err == nil {
			up <- struct{}{}
		}
	})
	<-up
	for { // the subscription is made after the handshake
		s.mu.Lock()
		n := len(s.subs)
		s.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := c.Call("Host.SendFiles", []any{dm.ID, "hi", nil, nil, nil}); err != nil {
		t.Fatal(err)
	}
	for {
		mu.Lock()
		done := len(got) > 0 && got[len(got)-1].Kind == team.EventRunEnd
		mu.Unlock()
		if done {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("no run_end: %+v", got)
		case <-time.After(10 * time.Millisecond):
		}
	}
	sig := false
	for _, e := range got {
		if e.Kind == team.EventEntry && e.Entry.Msg.Role == agent.RoleAssistant {
			for _, p := range e.Entry.Msg.Parts {
				sig = sig || p.Signature == "sig"
			}
		}
	}
	if !sig {
		t.Error("a desktop's entry lost its signature")
	}
}

func TestDesktopRoutesWantTailnet(t *testing.T) {
	for addr, want := range map[string]bool{"127.0.0.1:5": true, "100.101.1.2:5": true, "[fd7a:115c:a1e0::1]:5": true, "192.168.1.4:5": false, "8.8.8.8:5": false} {
		if Tailnet(addr) != want {
			t.Errorf("%s: %v", addr, !want)
		}
	}
}

func TestParseLink(t *testing.T) {
	c, err := ParseLink(PairURL("100.64.1.2", 7841, "tok", "Box"))
	if err != nil || c.Base != "http://100.64.1.2:7841" || c.Token != "tok" || c.Name != "Box" {
		t.Errorf("%+v, %v", c, err)
	}
	if _, err := ParseLink("https://example.com"); err == nil {
		t.Error("an http link")
	}
}
