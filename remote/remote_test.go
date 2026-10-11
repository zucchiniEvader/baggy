package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/team"
	"github.com/zucchiniEvader/baggy/tool"
)

// echo answers every message with "echo: " and its text.
type echo struct{}

func (echo) Call(ctx context.Context, system string, msgs []agent.Message, tools []tool.Tool, emit func(agent.Event)) agent.Message {
	text := ""
	for _, p := range msgs[len(msgs)-1].Parts {
		text += p.Text
	}
	emit(agent.Event{Kind: agent.EventTextDelta, Delta: "echo: " + text})
	return agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopEnd,
		Parts: []agent.Part{{Kind: agent.PartText, Text: "echo: " + text}, {Kind: agent.PartThinking, Text: "hm", Signature: "sig"}}}
}

func setup(t *testing.T) (*Server, *httptest.Server, store.Chat) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := &Server{Store: st, Token: NewToken(), Name: "Mac"}
	tm := &team.Team{Store: st, Model: func(store.Bot) (agent.Model, error) { return echo{}, nil }, Emit: s.Publish}
	s.Team = tm
	b, err := st.CreateBot(store.Bot{Name: "Chef"})
	if err != nil {
		t.Fatal(err)
	}
	dm, err := st.DMOf(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	hs := httptest.NewServer(s.Handler())
	t.Cleanup(func() { tm.Wait(); hs.Close() })
	return s, hs, dm
}

func call(t *testing.T, s *Server, hs *httptest.Server, method, path, body string, out any) int {
	t.Helper()
	req, _ := http.NewRequest(method, hs.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+s.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

func TestTokenIsRequired(t *testing.T) {
	_, hs, _ := setup(t)
	for _, h := range []string{"", "Bearer wrong"} {
		req, _ := http.NewRequest("GET", hs.URL+"/v1/snapshot", nil)
		if h != "" {
			req.Header.Set("Authorization", h)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%q: status %d, want 401", h, res.StatusCode)
		}
	}
}

func TestSendStreamsAndStores(t *testing.T) {
	s, hs, dm := setup(t)
	var snap Snapshot
	if code := call(t, s, hs, "GET", "/v1/snapshot", "", &snap); code != 200 {
		t.Fatalf("snapshot: %d", code)
	}
	if len(snap.Bots) != 1 || len(snap.Chats) != 1 || snap.Chats[0].ID != dm.ID {
		t.Fatalf("snapshot: %+v", snap)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u := "ws" + strings.TrimPrefix(hs.URL, "http") + "/v1/events?token=" + url.QueryEscape(s.Token)
	conn, _, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	for { // the subscription is made after the handshake
		s.mu.Lock()
		n := len(s.subs)
		s.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if code := call(t, s, hs, "POST", "/v1/chats/"+dm.ID+"/messages", `{"text":"hi"}`, nil); code != 200 {
		t.Fatalf("send: %d", code)
	}
	var kinds []string
	delta := ""
	for {
		_, b, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err, kinds)
		}
		var ev Event
		if err := json.Unmarshal(b, &ev); err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, string(ev.Kind))
		if ev.Agent != nil && ev.Agent.Kind == agent.EventTextDelta {
			delta += ev.Agent.Delta
		}
		if ev.Entry != nil {
			for _, p := range ev.Entry.Msg.Parts {
				if p.Signature != "" {
					t.Error("a signature reached the phone")
				}
			}
		}
		if ev.Kind == team.EventRunEnd {
			break
		}
	}
	if delta != "echo: hi" {
		t.Errorf("streamed %q", delta)
	}

	var es []store.Entry
	call(t, s, hs, "GET", "/v1/chats/"+dm.ID+"/messages", "", &es)
	if len(es) != 2 || es[0].Author != store.User || es[1].Msg.Parts[0].Text != "echo: hi" {
		t.Fatalf("messages: %+v", es)
	}
	var after []store.Entry
	call(t, s, hs, "GET", "/v1/chats/"+dm.ID+"/messages?after="+itoa(es[0].Seq), "", &after)
	if len(after) != 1 {
		t.Errorf("after: %d entries", len(after))
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestEmptyAndUnknown(t *testing.T) {
	s, hs, dm := setup(t)
	if code := call(t, s, hs, "POST", "/v1/chats/"+dm.ID+"/messages", `{"text":"  "}`, nil); code != 400 {
		t.Errorf("empty message: %d", code)
	}
	if code := call(t, s, hs, "GET", "/v1/chats/nope/messages", "", nil); code != 404 {
		t.Errorf("unknown chat: %d", code)
	}
	if code := call(t, s, hs, "GET", "/v1/avatars/..%2Fbaggy.db", "", nil); code != 400 && code != 404 {
		t.Errorf("avatar outside its folder: %d", code)
	}
}

func TestPairURL(t *testing.T) {
	u, err := url.Parse(PairURL("192.168.1.5", 7841, "abc", "Mac"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "baggy" || u.Host != "pair" || q.Get("host") != "192.168.1.5" || q.Get("port") != "7841" || q.Get("token") != "abc" || q.Get("name") != "Mac" {
		t.Errorf("%s", u)
	}
}
