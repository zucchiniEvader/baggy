// Package remote serves a host over the network, behind one token: to the
// phone app, a JSON API to read and send chats and a WebSocket of team
// events (docs/mobile.md); to a desktop on another machine of the tailnet,
// every method of the host and its store, the events in full and the
// attached files (docs/remote.md). Client is that desktop's end.
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
	"github.com/zucchiniEvader/baggy/host"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/team"
)

// DefaultPort is where the server listens unless told otherwise.
const DefaultPort = 7841

// Server answers phones and desktops. Publish every team.Event to it.
type Server struct {
	Host    *host.Host
	Token   string
	Name    string // the computer's name, shown on the phone
	Version string
	// Changed, when set, is told after the phone changed a chat's state the
	// desktop shows (read), so it can draw it.
	Changed func(chatID string)

	mu   sync.Mutex
	subs map[chan []byte]bool // true for a desktop's, which takes events in full
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
	mux.HandleFunc("POST /v1/call", s.call)
	mux.HandleFunc("GET /v1/files/{id}/{name}", s.file)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			fail(w, http.StatusUnauthorized, errors.New("not paired"))
			return
		}
		if desktopRoute(r) && !Tailnet(r.RemoteAddr) {
			fail(w, http.StatusForbidden, errors.New("a desktop connects over Tailscale"))
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// desktopRoute reports whether r is a desktop's: the host's methods, the
// events in full, the attached files.
func desktopRoute(r *http.Request) bool {
	p := r.URL.Path
	return p == "/v1/call" || strings.HasPrefix(p, "/v1/files/") || (p == "/v1/events" && r.URL.Query().Get("full") == "1")
}

var tailnets = []*net.IPNet{
	{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)},
	{IP: net.ParseIP("fd7a:115c:a1e0::"), Mask: net.CIDRMask(48, 128)},
}

// Tailnet reports whether addr (host:port, or an IP) is on Tailscale's
// network or this machine.
func Tailnet(addr string) bool {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		addr = h
	}
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, n := range tailnets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// TailnetAddresses are this machine's Tailscale IPv4 addresses.
func TailnetAddresses() []string {
	var out []string
	for _, a := range Addresses() {
		if ip := net.ParseIP(a); ip != nil && !ip.IsLoopback() && Tailnet(a) {
			out = append(out, a)
		}
	}
	return out
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
type Snapshot = host.Snapshot

func (s *Server) snapshot(w http.ResponseWriter, r *http.Request) {
	snap, err := s.Host.Snapshot()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	reply(w, snap)
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) (store.Chat, bool) {
	c, err := s.Host.Store.Chat(r.PathValue("id"))
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
	es, err := s.Host.Store.Messages(c.ID, after)
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
		if e, err := s.Host.Store.Entry(c.ID, b.ReplyTo); err == nil {
			quote = team.QuoteOf(e)
		}
	}
	if err := s.Host.Team.Send(c.ID, b.Text, b.Mentions, quote); err != nil {
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
	s.Host.Stop(c.ID)
	reply(w, map[string]bool{"ok": true})
}

func (s *Server) read(w http.ResponseWriter, r *http.Request) {
	c, ok := s.chat(w, r)
	if !ok {
		return
	}
	if err := s.Host.Store.MarkRead(c.ID); err != nil {
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
	p := s.Host.Store.AvatarPath(name)
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
	if e.Kind == team.EventApproval { // approvals are the computer's
		return out, false
	}
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

// Publish sends e to every connected phone and desktop. One too slow to
// keep up is dropped; it reconnects and reloads.
func (s *Server) Publish(e team.Event) {
	s.mu.Lock()
	n := len(s.subs)
	s.mu.Unlock()
	if n == 0 {
		return
	}
	var slim, full []byte
	if ev, ok := Wire(e); ok {
		slim, _ = json.Marshal(ev)
	}
	full, _ = json.Marshal(Full(e))
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch, desktop := range s.subs {
		b := slim
		if desktop {
			b = full
		}
		if b == nil {
			continue
		}
		select {
		case ch <- b:
		default:
			delete(s.subs, ch)
			close(ch)
		}
	}
}

func (s *Server) subscribe(desktop bool) chan []byte {
	ch := make(chan []byte, map[bool]int{false: 512, true: 4096}[desktop])
	s.mu.Lock()
	if s.subs == nil {
		s.subs = map[chan []byte]bool{}
	}
	s.subs[ch] = desktop
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

// events is the WebSocket: one JSON event per text message (Wire, or Full
// with full=1), and a ping every 20 seconds so a dead phone is noticed.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true}) // a phone app sends no browser Origin
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ch := s.subscribe(r.URL.Query().Get("full") == "1")
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
