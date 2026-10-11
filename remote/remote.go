// Package remote serves a store's chats to the phone app over the local
// network: a JSON API to read and send, and a WebSocket of team events, both
// behind one token the phone gets by scanning the pairing code (docs/mobile.md).
package remote

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/team"
)

// DefaultPort is where the server listens unless told otherwise.
const DefaultPort = 7841

// Server answers the phone. Publish every team.Event to it.
type Server struct {
	Store   *store.Store
	Team    *team.Team
	Token   string
	Name    string // the computer's name, shown on the phone
	Version string
	// Changed, when set, is told after the phone changed a chat's state the
	// desktop shows (read), so it can draw it.
	Changed func(chatID string)

	mu   sync.Mutex
	subs map[chan []byte]struct{}
	srv  *http.Server
}

// NewToken is a fresh random token: 32 bytes as hex.
func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// PairURL is what the pairing code holds.
func PairURL(host string, port int, token, name string) string {
	q := url.Values{"host": {host}, "port": {strconv.Itoa(port)}, "token": {token}}
	if name != "" {
		q.Set("name", name)
	}
	return "baggy://pair?" + q.Encode()
}

// Addresses are this machine's IPv4 addresses the phone may reach: the
// private ranges (Wi-Fi, Ethernet) first, then Tailscale's 100.64/10.
func Addresses() []string {
	var lan, other []string
	ifs, _ := net.Interfaces()
	for _, in := range ifs {
		if in.Flags&net.FlagUp == 0 || in.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := in.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil || n.IP.IsLinkLocalUnicast() {
				continue
			}
			if n.IP.IsPrivate() {
				lan = append(lan, n.IP.String())
			} else {
				other = append(other, n.IP.String())
			}
		}
	}
	return append(lan, other...)
}

// Start listens on addr (":7841") until Stop.
func (s *Server) Start(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.srv = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	srv := s.srv
	s.mu.Unlock()
	go srv.Serve(ln)
	return nil
}

// Stop closes the listener and every phone's connection.
func (s *Server) Stop() {
	s.mu.Lock()
	srv := s.srv
	s.srv = nil
	for ch := range s.subs {
		close(ch)
	}
	s.subs = nil
	s.mu.Unlock()
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		srv.Close()
	}
}

// Handler is the API, every route behind the token.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/hello", s.hello)
	mux.HandleFunc("GET /v1/snapshot", s.snapshot)
	mux.HandleFunc("GET /v1/chats/{id}/messages", s.messages)
	mux.HandleFunc("POST /v1/chats/{id}/messages", s.send)
	mux.HandleFunc("POST /v1/chats/{id}/stop", s.stop)
	mux.HandleFunc("POST /v1/chats/{id}/read", s.read)
	mux.HandleFunc("GET /v1/avatars/{name}", s.avatar)
	mux.HandleFunc("GET /v1/events", s.events)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			fail(w, http.StatusUnauthorized, errors.New("not paired"))
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// authorized checks the bearer token, or the token query parameter (a
// WebSocket or an image the phone loads by URL).
func (s *Server) authorized(r *http.Request) bool {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		got = r.URL.Query().Get("token")
	}
	return s.Token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) == 1
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func (s *Server) hello(w http.ResponseWriter, r *http.Request) {
	reply(w, map[string]string{"app": "Baggy", "name": s.Name, "version": s.Version})
}

// Snapshot is everything the chat list needs.
type Snapshot struct {
	Bots     []store.Bot              `json:"bots"`
	Chats    []store.Chat             `json:"chats"`
	Previews map[string]store.Preview `json:"previews"`
	Unread   map[string]int           `json:"unread"`
	Busy     []string                 `json:"busy"`
}

func (s *Server) snapshot(w http.ResponseWriter, r *http.Request) {
	bots, err := s.Store.Bots()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	chats, err := s.Store.Chats()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	pv, err := s.Store.Previews()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	snap := Snapshot{Bots: bots, Chats: chats, Previews: pv, Unread: map[string]int{}, Busy: []string{}}
	for _, c := range chats {
		if n := s.Store.Unread(c.ID); n > 0 {
			snap.Unread[c.ID] = n
		}
		if s.Team.Busy(c.ID) {
			snap.Busy = append(snap.Busy, c.ID)
		}
	}
	reply(w, snap)
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) (store.Chat, bool) {
	c, err := s.Store.Chat(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return c, false
	}
	return c, true
}

func (s *Server) messages(w http.ResponseWriter, r *http.Request) {
	c, ok := s.chat(w, r)
	if !ok {
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	es, err := s.Store.Messages(c.ID, after)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	for i := range es {
		es[i] = Slim(es[i])
	}
	if es == nil {
		es = []store.Entry{}
	}
	reply(w, es)
}

// Slim is an entry as the phone gets it: no image bytes, thinking
// signatures or vendor blocks, which only a model reads.
func Slim(e store.Entry) store.Entry {
	e.Msg = slimMessage(e.Msg)
	return e
}

func slimMessage(m agent.Message) agent.Message {
	parts := make([]agent.Part, len(m.Parts))
	for i, p := range m.Parts {
		p.Data, p.Signature, p.Raw = nil, "", nil
		parts[i] = p
	}
	m.Parts = parts
	return m
}

type sendBody struct {
	Text     string   `json:"text"`
	Mentions []string `json:"mentions"`
	ReplyTo  int64    `json:"reply_to"` // the seq of the message answered, 0 for none
}

func (s *Server) send(w http.ResponseWriter, r *http.Request) {
	c, ok := s.chat(w, r)
	if !ok {
		return
	}
	var b sendBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&b); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(b.Text) == "" {
		fail(w, http.StatusBadRequest, errors.New("empty message"))
		return
	}
	var quote *store.Quote
	if b.ReplyTo > 0 {
		if e, err := s.Store.Entry(c.ID, b.ReplyTo); err == nil {
			quote = team.QuoteOf(e)
		}
	}
	if err := s.Team.Send(c.ID, b.Text, b.Mentions, quote); err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	reply(w, map[string]bool{"ok": true})
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	c, ok := s.chat(w, r)
	if !ok {
		return
	}
	s.Team.Stop(c.ID)
	reply(w, map[string]bool{"ok": true})
}

func (s *Server) read(w http.ResponseWriter, r *http.Request) {
	c, ok := s.chat(w, r)
	if !ok {
		return
	}
	if err := s.Store.MarkRead(c.ID); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	if s.Changed != nil {
		s.Changed(c.ID)
	}
	s.Publish(team.Event{Kind: team.EventChanged, ChatID: c.ID}) // another phone's count
	reply(w, map[string]bool{"ok": true})
}

func (s *Server) avatar(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		fail(w, http.StatusBadRequest, errors.New("bad name"))
		return
	}
	p := s.Store.AvatarPath(name)
	if _, err := os.Stat(p); err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, p)
}

// Event is a team.Event as the phone gets it.
type Event struct {
	Kind   team.EventKind `json:"kind"`
	ChatID string         `json:"chat_id"`
	BotID  string         `json:"bot_id,omitempty"`
	Turn   string         `json:"turn,omitempty"`
	Since  int64          `json:"since,omitempty"`
	Agent  *AgentEvent    `json:"agent,omitempty"`
	Entry  *store.Entry   `json:"entry,omitempty"`
	Error  string         `json:"error,omitempty"`
}

// AgentEvent is a streamed agent.Event without tool output, which the
// phone does not show; a tool's name and whether it failed are kept.
type AgentEvent struct {
	Kind    agent.EventKind `json:"kind"`
	Delta   string          `json:"delta,omitempty"`
	Message *agent.Message  `json:"message,omitempty"`
	Tool    string          `json:"tool,omitempty"`
	CallID  string          `json:"call_id,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
}

// Wire is e as the phone gets it; ok is false for what it does not need
// (a tool's running output).
func Wire(e team.Event) (Event, bool) {
	out := Event{Kind: e.Kind, ChatID: e.ChatID, BotID: e.BotID, Turn: e.Turn, Since: e.Since}
	if e.Entry != nil {
		en := Slim(*e.Entry)
		out.Entry = &en
	}
	if e.Err != nil {
		out.Error = e.Err.Error()
	}
	if e.Kind == team.EventAgent {
		ev := e.Agent
		if ev.Kind == agent.EventToolUpdate {
			return out, false
		}
		ae := &AgentEvent{Kind: ev.Kind, Delta: ev.Delta}
		if ev.Message != nil {
			m := slimMessage(*ev.Message)
			ae.Message = &m
		}
		if ev.Call != nil {
			ae.Tool, ae.CallID = ev.Call.Name, ev.Call.CallID
		}
		if ev.Result != nil {
			ae.IsError = ev.Result.IsError
		}
		out.Agent = ae
	}
	return out, true
}

// Publish sends e to every connected phone. A phone too slow to keep up is
// dropped; it reconnects and reloads.
func (s *Server) Publish(e team.Event) {
	s.mu.Lock()
	n := len(s.subs)
	s.mu.Unlock()
	if n == 0 {
		return
	}
	ev, ok := Wire(e)
	if !ok {
		return
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- b:
		default:
			delete(s.subs, ch)
			close(ch)
		}
	}
}

func (s *Server) subscribe() chan []byte {
	ch := make(chan []byte, 512)
	s.mu.Lock()
	if s.subs == nil {
		s.subs = map[chan []byte]struct{}{}
	}
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

func (s *Server) unsubscribe(ch chan []byte) {
	s.mu.Lock()
	if _, ok := s.subs[ch]; ok {
		delete(s.subs, ch)
		close(ch)
	}
	s.mu.Unlock()
}

// events is the WebSocket: one JSON event per text message, and a ping
// every 20 seconds so a dead phone is noticed.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true}) // a phone app sends no browser Origin
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ch := s.subscribe()
	defer s.unsubscribe(ch)
	ctx := conn.CloseRead(r.Context())
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case b, ok := <-ch:
			if !ok {
				conn.Close(websocket.StatusGoingAway, "reload")
				return
			}
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Write(wctx, websocket.MessageText, b)
			cancel()
			if err != nil {
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}
