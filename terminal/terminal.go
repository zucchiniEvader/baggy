// Package terminal runs a bot's commands in pseudo-terminals of their own,
// after ../lorca/docs/architecture/terminal-sessions.md: a command can ask
// a question and be answered, keep running in the background, and outlive
// the call that started it. macOS and Linux; elsewhere Supported is false.
package terminal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// MaxSessions run at once; a new one stops the oldest (background last).
	MaxSessions = 8
	// IdleLimit stops a command with no output or input that long (a
	// background one only while it sits at a question).
	IdleLimit = 30 * time.Minute
	// PromptQuiet is how long a question's line holds still before a call
	// returns at it; Silence how long no output at all returns one.
	PromptQuiet = 2 * time.Second
	Silence     = 20 * time.Second
	// BackgroundSettle is how long a background command's call waits.
	BackgroundSettle = 2 * time.Second
	keep             = 200 << 10 // bytes of output a session keeps
)

// ErrUnsupported is returned where there are no pseudo-terminals.
var ErrUnsupported = errors.New("terminal sessions are not supported here")

// Session is one command in its terminal.
type Session struct {
	ID, Chat, Bot, Command string
	Description            string
	Started                time.Time

	m          *Manager
	mu         sync.Mutex
	out        []byte // raw, the last keep bytes
	read       int    // of out, what the bot has read (an offset into the total)
	total      int    // bytes ever written
	lastOut    time.Time
	lastIn     time.Time
	background bool
	answered   bool // input came after the last output
	done       chan struct{}
	exitCode   int
	ended      string // how it ended: "exited", "failed", "stopped: …"
	stop       func()
	write      func([]byte) error
	changed    chan struct{} // closed and replaced on every output
}

// Manager keeps the sessions of every chat.
type Manager struct {
	// Env is the environment a command starts with (the login shell's).
	Env func() []string
	// OnExit hears a background command that ended by itself.
	OnExit func(s *Session)

	mu       sync.Mutex
	sessions map[string]*Session
	next     int
	idle     *time.Ticker
}

func NewManager() *Manager { return &Manager{sessions: map[string]*Session{}} }

// Start runs command in dir for a bot in a chat.
func (m *Manager) Start(chat, bot, dir, command, description string, background bool) (*Session, error) {
	if !Supported {
		return nil, ErrUnsupported
	}
	m.mu.Lock()
	m.next++
	s := &Session{ID: fmt.Sprintf("s%d", m.next), Chat: chat, Bot: bot, Command: command, Description: description, Started: time.Now(),
		m: m, background: background, done: make(chan struct{}), changed: make(chan struct{})}
	s.lastOut = s.Started
	m.sessions[s.ID] = s
	if m.idle == nil {
		m.idle = time.NewTicker(time.Minute)
		go m.reap(m.idle)
	}
	m.mu.Unlock()
	m.limit(s)

	env := os.Environ()
	if m.Env != nil {
		env = m.Env()
	}
	env = append(env, "TERM=xterm-256color", "PAGER=cat", "GIT_PAGER=cat", "GOBOT_SESSION="+s.ID)
	shell := "/bin/bash"
	if _, err := os.Stat(shell); err != nil {
		shell = "/bin/sh"
	}
	// Echo off, so what is typed (a y, a password) never comes back as
	// output; SIGHUP ignored, so what the command leaves behind lives on.
	cmd := exec.Command(shell, "-c", "stty -echo 2>/dev/null; trap '' HUP; "+command)
	cmd.Dir, cmd.Env = dir, env
	if err := start(s, cmd); err != nil {
		m.mu.Lock()
		delete(m.sessions, s.ID)
		m.mu.Unlock()
		return nil, err
	}
	return s, nil
}

// output is called by the reader with what the terminal printed.
func (s *Session) output(b []byte) {
	s.mu.Lock()
	s.out = append(s.out, b...)
	s.total += len(b)
	if over := len(s.out) - keep; over > 0 {
		s.out = s.out[over:]
	}
	s.lastOut, s.answered = time.Now(), false
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
}

// exited is called once the command has ended.
func (s *Session) exited(code int, err error) {
	s.mu.Lock()
	if s.ended == "" {
		s.exitCode = code
		s.ended = "exited"
		if code != 0 {
			s.ended = fmt.Sprintf("failed with exit code %d", code)
		}
		if err != nil && code == 0 {
			s.ended = "failed: " + err.Error()
		}
	}
	bg, byItself := s.background, !strings.HasPrefix(s.ended, "stopped")
	close(s.done)
	s.mu.Unlock()
	if bg && byItself && s.m.OnExit != nil {
		s.m.OnExit(s)
	}
}

// Done reports whether the command has ended, and how.
func (s *Session) Done() (bool, string) {
	select {
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return true, s.ended
	default:
		return false, ""
	}
}

func (s *Session) Background() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.background }

// SendToBackground marks a running command as one that keeps running.
func (s *Session) SendToBackground() { s.mu.Lock(); s.background = true; s.mu.Unlock() }

// Stop ends the command and everything it started.
func (s *Session) Stop(reason string) {
	s.mu.Lock()
	if s.ended == "" {
		s.ended = "stopped: " + reason
	}
	stop := s.stop
	s.mu.Unlock()
	if stop != nil {
		stop()
		select { // until it has ended, so what follows sees it gone
		case <-s.done:
		case <-time.After(2 * time.Second):
		}
	}
}

// Write types text into the terminal, Enter after it unless enter is false.
func (s *Session) Write(text string, enter bool) error {
	if done, how := s.Done(); done {
		return fmt.Errorf("the command has ended (%s)", how)
	}
	if enter {
		text += "\r"
	}
	s.mu.Lock()
	s.lastIn, s.answered = time.Now(), true
	w := s.write
	s.mu.Unlock()
	return w([]byte(Keys(text)))
}

// Keys turns spelled-out escapes (`\u0003`, `\x1b[B`) in typed text into
// the keys they name.
func Keys(text string) string {
	r := strings.NewReplacer(`\u0003`, "\x03", `\u0004`, "\x04", `\u001b`, "\x1b", `\x1b`, "\x1b", `\x03`, "\x03", `\r`, "\r", `\t`, "\t")
	return r.Replace(text)
}

// Text is all the output the session keeps, as text.
func (s *Session) Text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Sanitize(string(s.out))
}

// Last is the last n lines of output.
func (s *Session) Last(n int) string {
	lines := strings.Split(strings.TrimRight(s.Text(), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// unread is the output since the bot last read, marking it read.
func (s *Session) unread() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	start := len(s.out) - (s.total - s.read)
	cut := false
	if start < 0 {
		start, cut = 0, true
	}
	text := Sanitize(string(s.out[start:]))
	s.read = s.total
	if cut {
		text = "[… earlier output not kept …]\n" + text
	}
	return text
}

var question = regexp.MustCompile(`(?i)(password|passphrase|\[y/n\]|\(yes/no|enter |press |\bpin\b|token|code|otp|continue\?)|[?:>)\]$#]\s*$`)

// asking is the open last line, when it reads like a question nobody has
// answered.
func (s *Session) asking() (string, bool) {
	s.mu.Lock()
	answered := s.answered
	raw := string(s.out[max(0, len(s.out)-4096):])
	s.mu.Unlock()
	if answered || strings.HasSuffix(raw, "\n") {
		return "", false
	}
	text := Sanitize(raw)
	line := text[strings.LastIndexByte(text, '\n')+1:]
	if strings.TrimSpace(line) == "" || !question.MatchString(line) {
		return "", false
	}
	return strings.TrimSpace(line), true
}

// Wait is how a call waits on its command: until it ends, sits at a
// question for PromptQuiet, prints nothing for Silence, max passes (0: no
// limit), or ctx ends. It returns the output since the last read and a note
// on where the command stands.
func (s *Session) Wait(ctx context.Context, max time.Duration) (output, note string, ended bool) {
	var deadline <-chan time.Time
	if max > 0 {
		t := time.NewTimer(max)
		defer t.Stop()
		deadline = t.C
	}
	for {
		s.mu.Lock()
		changed, lastOut := s.changed, s.lastOut
		s.mu.Unlock()
		quiet := time.Since(lastOut)
		if q, ok := s.asking(); ok && quiet >= PromptQuiet {
			return s.unread(), fmt.Sprintf("The command waits at a question: %q. Answer with shell_input {session_id: %q, text}, or stop it with text \\u0003.", q, s.ID), false
		}
		if quiet >= Silence && max == 0 {
			return s.unread(), fmt.Sprintf("No output for %d s: the command may be working, or waiting at a question it printed where the terminal does not show it (into a pipe or a file). Read on with shell_output {session_id: %q}, or stop it with shell_input text \\u0003.", int(Silence/time.Second), s.ID), false
		}
		wait := min(PromptQuiet, Silence-quiet)
		if wait <= 0 {
			wait = PromptQuiet
		}
		timer := time.NewTimer(wait)
		select {
		case <-s.done:
			timer.Stop()
			_, how := s.Done()
			return s.unread(), "The command " + how + ".", true
		case <-changed:
		case <-timer.C:
		case <-deadline:
			timer.Stop()
			return s.unread(), fmt.Sprintf("The command is still running (session %s).", s.ID), false
		case <-ctx.Done():
			timer.Stop()
			return s.unread(), "", false
		}
		timer.Stop()
	}
}

// Get finds a session by id.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

// List is a chat's sessions ("" for every chat's), oldest first, ended ones
// kept until Forget.
func (m *Manager) List(chat string) []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Session
	for _, s := range m.sessions {
		if chat == "" || s.Chat == chat {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out
}

// Running is a chat's commands still running.
func (m *Manager) Running(chat string) []*Session {
	var out []*Session
	for _, s := range m.List(chat) {
		if done, _ := s.Done(); !done {
			out = append(out, s)
		}
	}
	return out
}

// StopChat ends a chat's commands, but for those in the background.
func (m *Manager) StopChat(chat string) {
	for _, s := range m.Running(chat) {
		if !s.Background() {
			s.Stop("Stop in the chat")
		}
	}
}

// StopAll ends every command (the app quits).
func (m *Manager) StopAll() {
	for _, s := range m.Running("") {
		s.Stop("gobot quit")
	}
}

// Forget drops ended sessions older than d.
func (m *Manager) Forget(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		select {
		case <-s.done:
			if time.Since(s.Started) > d {
				delete(m.sessions, id)
			}
		default:
		}
	}
}

// limit stops the oldest command when too many run: one not in the
// background first.
func (m *Manager) limit(newest *Session) {
	running := m.Running("")
	if len(running) <= MaxSessions {
		return
	}
	for _, bg := range []bool{false, true} {
		for _, s := range running {
			if s != newest && s.Background() == bg {
				s.Stop("more than eight commands were running")
				return
			}
		}
	}
}

func (m *Manager) reap(t *time.Ticker) {
	for range t.C {
		for _, s := range m.Running("") {
			s.mu.Lock()
			last := s.lastOut
			if s.lastIn.After(last) {
				last = s.lastIn
			}
			bg := s.background
			s.mu.Unlock()
			_, asking := s.asking()
			if time.Since(last) > IdleLimit && (!bg || asking) {
				s.Stop("30 minutes without output or input")
			}
		}
		m.Forget(time.Hour)
	}
}

var (
	csi     = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	osc     = regexp.MustCompile(`\x1b\][^\x07\x1b]*(\x07|\x1b\\)`)
	escape2 = regexp.MustCompile(`\x1b[@-Z\\-_]`)
)

// Sanitize makes terminal output text: escape sequences go, a carriage
// return redraws its line, a backspace takes back a character, other
// control characters go.
func Sanitize(s string) string {
	s = osc.ReplaceAllString(s, "")
	s = csi.ReplaceAllString(s, "")
	s = escape2.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var out strings.Builder
	for _, line := range strings.SplitAfter(s, "\n") {
		var cur []rune
		col := 0
		for _, r := range strings.TrimSuffix(line, "\n") {
			switch {
			case r == '\r':
				col = 0
			case r == '\b':
				if col > 0 {
					col--
				}
			case r == '\t' || r >= ' ' && r != 0x7f:
				if col < len(cur) {
					cur[col] = r
				} else {
					cur = append(cur, r)
				}
				col++
			}
		}
		out.WriteString(string(cur))
		if strings.HasSuffix(line, "\n") {
			out.WriteByte('\n')
		}
	}
	return out.String()
}
