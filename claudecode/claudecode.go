// Package claudecode hands coding tasks to the user's own Claude Code: the
// claude command on this computer, signed in to their account, driven over
// its stream-json protocol (docs/claude-code.md). A Run is one task in a
// folder; a follow-up resumes the same Claude Code conversation.
package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// MaxRuns run at once across every chat; one more is refused.
const MaxRuns = 2

// Unset are taken out of the environment Claude Code starts with, so it
// works on the account it is signed in to, never on an API key of the shell.
var Unset = []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT"}

// Find is the claude command on env's PATH.
func Find(env []string) (string, error) {
	path := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			path = v
		}
	}
	for _, dir := range filepath.SplitList(path) {
		for _, name := range []string{"claude", "claude.exe", "claude.cmd"} {
			p := filepath.Join(dir, name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
				return p, nil
			}
		}
	}
	return "", errors.New("Claude Code is not installed: the claude command is not on the PATH")
}

// Request is Claude Code asking to use a tool (can_use_tool).
type Request struct {
	ID          string
	Tool        string
	Input       json.RawMessage
	Description string
}

// Step is a tool call of a run.
type Step struct {
	ID, Tool, Summary string
	Done, Failed      bool
	Nested            bool // a subagent's
}

// Options start a run.
type Options struct {
	Chat, Bot, Dir, Task string
	// Resume is the Claude Code session to continue; "" starts a new one.
	Resume string
	// Approve answers Claude Code's requests to use a tool; nil allows them.
	// A no comes with what Claude Code reads.
	Approve func(ctx context.Context, req Request) (bool, string)
	// OnEnd hears the run's end, however it ended.
	OnEnd func(r *Run)
}

// Run is one task given to Claude Code.
type Run struct {
	ID, Chat, Bot, Dir, Task string
	Started                  time.Time

	opts   Options
	real   string // Dir with its links followed, as Claude Code names paths
	cancel context.CancelFunc
	done   chan struct{}

	mu       sync.Mutex
	session  string
	account  string // the plan it works on, or "API key"
	steps    []Step
	text     string // the last thing it said
	result   string
	ended    string // "finished", "failed: …", "stopped: …"
	turns    int
	stderr   []byte
	fresh    bool // a resume that was not found started over
	finished time.Time
}

// Session is the Claude Code session, once it is known.
func (r *Run) Session() string { r.mu.Lock(); defer r.mu.Unlock(); return r.session }

// Account is what the run is billed to: the plan ("Claude Pro") or "API key".
func (r *Run) Account() string { r.mu.Lock(); defer r.mu.Unlock(); return r.account }

// Fresh reports whether a session to resume was gone and the run started a
// new one.
func (r *Run) Fresh() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.fresh }

// Steps are its tool calls so far.
func (r *Run) Steps() []Step {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Step(nil), r.steps...)
}

// Text is the last thing it said.
func (r *Run) Text() string { r.mu.Lock(); defer r.mu.Unlock(); return r.text }

// Result is its final report, once it has ended.
func (r *Run) Result() string { r.mu.Lock(); defer r.mu.Unlock(); return r.result }

// Done reports whether it has ended, and how: "finished", "failed: …",
// "stopped: …".
func (r *Run) Done() (bool, string) {
	select {
	case <-r.done:
		r.mu.Lock()
		defer r.mu.Unlock()
		return true, r.ended
	default:
		return false, ""
	}
}

// Stopped reports whether it was stopped rather than ending by itself.
func (r *Run) Stopped() bool {
	done, how := r.Done()
	return done && strings.HasPrefix(how, "stopped")
}

// Took is how long it ran, or has run so far.
func (r *Run) Took() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.finished.IsZero() {
		return r.finished.Sub(r.Started)
	}
	return time.Since(r.Started)
}

// Stop ends it and everything it started.
func (r *Run) Stop(reason string) {
	r.mu.Lock()
	if r.ended == "" {
		r.ended = "stopped: " + reason
	}
	r.mu.Unlock()
	r.cancel()
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
	}
}

// Wait waits up to d (0: no limit) for the run to end, or for ctx; it
// reports whether it has.
func (r *Run) Wait(ctx context.Context, d time.Duration) bool {
	var limit <-chan time.Time
	if d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		limit = t.C
	}
	select {
	case <-r.done:
		return true
	case <-limit:
	case <-ctx.Done():
	}
	return false
}

// Progress is its last n steps as lines, and what it said last.
func (r *Run) Progress(n int) string {
	steps := r.Steps()
	if len(steps) > n {
		steps = steps[len(steps)-n:]
	}
	var b strings.Builder
	for _, s := range steps {
		mark := "…"
		switch {
		case s.Failed:
			mark = "✗"
		case s.Done:
			mark = "✓"
		}
		line := s.Tool
		if s.Summary != "" {
			line += " " + s.Summary
		}
		if s.Nested {
			line = "  " + line
		}
		fmt.Fprintf(&b, "%s %s\n", mark, line)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Manager keeps the runs of every chat.
type Manager struct {
	// Env is the environment Claude Code starts with (the login shell's);
	// Unset is taken out of it.
	Env func() []string
	// Command is the claude command; "" finds it on Env's PATH.
	Command string

	mu   sync.Mutex
	runs map[string]*Run
	next int
}

func NewManager() *Manager { return &Manager{runs: map[string]*Run{}} }

func (m *Manager) env() []string {
	env := os.Environ()
	if m.Env != nil {
		env = m.Env()
	}
	out := env[:0:0]
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, u := range Unset {
			drop = drop || k == u
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

// Available is the claude command Start would run.
func (m *Manager) Available() (string, error) {
	if m.Command != "" {
		return m.Command, nil
	}
	return Find(m.env())
}

// Start hands a task to Claude Code in a folder.
func (m *Manager) Start(o Options) (*Run, error) {
	if strings.TrimSpace(o.Task) == "" {
		return nil, errors.New("no task")
	}
	if st, err := os.Stat(o.Dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("no folder %s", o.Dir)
	}
	command, err := m.Available()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	running := 0
	for _, r := range m.runs {
		if done, _ := r.Done(); !done {
			running++
			if r.Chat == o.Chat && r.Bot == o.Bot {
				m.mu.Unlock()
				return nil, fmt.Errorf("Claude Code is still working on run %s in this chat: wait for it or stop it", r.ID)
			}
		}
	}
	if running >= MaxRuns {
		m.mu.Unlock()
		return nil, fmt.Errorf("Claude Code already has %d tasks running: wait for one to end", running)
	}
	m.next++
	ctx, cancel := context.WithCancel(context.Background())
	r := &Run{ID: fmt.Sprintf("c%d", m.next), Chat: o.Chat, Bot: o.Bot, Dir: o.Dir, Task: o.Task, Started: time.Now(),
		opts: o, cancel: cancel, done: make(chan struct{})}
	if real, err := filepath.EvalSymlinks(o.Dir); err == nil {
		r.real = real
	}
	m.runs[r.ID] = r
	m.mu.Unlock()
	go r.work(ctx, command, m.env())
	return r, nil
}

// Get finds a run by id.
func (m *Manager) Get(id string) (*Run, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	return r, ok
}

// List is a chat's runs ("" for every chat's), oldest first.
func (m *Manager) List(chat string) []*Run {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Run
	for _, r := range m.runs {
		if chat == "" || r.Chat == chat {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out
}

// Running is a chat's runs still at work ("" for every chat's).
func (m *Manager) Running(chat string) []*Run {
	var out []*Run
	for _, r := range m.List(chat) {
		if done, _ := r.Done(); !done {
			out = append(out, r)
		}
	}
	return out
}

// Latest is a bot's newest run in a chat, or nil.
func (m *Manager) Latest(chat, bot string) *Run {
	var last *Run
	for _, r := range m.List(chat) {
		if r.Bot == bot {
			last = r
		}
	}
	return last
}

// StopAll ends every run (the app quits).
func (m *Manager) StopAll() {
	for _, r := range m.Running("") {
		r.Stop("Baggy quit")
	}
}

// Forget drops runs that ended longer than d ago.
func (m *Manager) Forget(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, r := range m.runs {
		if done, _ := r.Done(); done && time.Since(r.Started.Add(r.Took())) > d {
			delete(m.runs, id)
		}
	}
}

// work runs the task to its end, once more without resuming when the
// session to resume is gone.
func (r *Run) work(ctx context.Context, command string, env []string) {
	defer func() {
		r.mu.Lock()
		r.finished = time.Now()
		if r.ended == "" {
			r.ended = "finished"
		}
		r.mu.Unlock()
		r.cancel()
		close(r.done)
		if r.opts.OnEnd != nil {
			r.opts.OnEnd(r)
		}
	}()
	resume := r.opts.Resume
	for {
		gone := r.attempt(ctx, command, env, resume)
		if !gone || resume == "" || ctx.Err() != nil {
			return
		}
		r.mu.Lock()
		r.fresh, r.ended, r.result, r.stderr = true, "", "", nil
		r.mu.Unlock()
		resume = ""
	}
}

// Args are the claude command's arguments for a run.
func Args(resume string) []string {
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--permission-prompt-tool", "stdio", "--permission-mode", "acceptEdits"}
	if resume != "" {
		args = append(args, "--resume", resume)
	}
	return args
}

// attempt runs the command once; it reports whether the session to resume
// was not found.
func (r *Run) attempt(ctx context.Context, command string, env []string, resume string) (gone bool) {
	cmd := exec.CommandContext(ctx, command, Args(resume)...)
	cmd.Dir, cmd.Env = r.Dir, env
	killGroup(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		r.fail(err.Error())
		return false
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.fail(err.Error())
		return false
	}
	cmd.Stderr = &tailWriter{r: r}
	if err := cmd.Start(); err != nil {
		r.fail(err.Error())
		return false
	}
	c := &conn{r: r, in: stdin, ctx: ctx}
	c.send(map[string]any{"type": "control_request", "request_id": "init", "request": map[string]any{"subtype": "initialize"}})
	c.send(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": r.Task}, "parent_tool_use_id": nil, "session_id": ""})
	out := bufio.NewReaderSize(stdout, 64<<10)
	result := c.read(out)
	stdin.Close()
	go io.Copy(io.Discard, out) // whatever follows the result, so it never blocks
	werr := cmd.Wait()

	r.mu.Lock()
	defer r.mu.Unlock()
	stderr := strings.TrimSpace(string(r.stderr))
	if r.ended != "" { // stopped
		return false
	}
	if result == nil {
		why := "Claude Code ended without a result"
		if werr != nil {
			why += " (" + werr.Error() + ")"
		}
		if stderr != "" {
			why += ": " + lastLine(stderr)
		}
		r.ended = "failed: " + why
		return false
	}
	r.result = result.Result
	r.turns = result.Turns
	if result.IsError {
		if result.Turns == 0 && strings.Contains(stderr, "No conversation found") {
			return true
		}
		why := result.Subtype
		if result.Result != "" {
			why = result.Result
		} else if stderr != "" {
			why = lastLine(stderr)
		}
		r.ended = "failed: " + oneLine(why, 300)
	}
	return false
}

func (r *Run) fail(why string) {
	r.mu.Lock()
	if r.ended == "" {
		r.ended = "failed: " + why
	}
	r.mu.Unlock()
}

// tailWriter keeps the last 8 KB of stderr.
type tailWriter struct{ r *Run }

func (w *tailWriter) Write(b []byte) (int, error) {
	w.r.mu.Lock()
	w.r.stderr = append(w.r.stderr, b...)
	if over := len(w.r.stderr) - 8<<10; over > 0 {
		w.r.stderr = w.r.stderr[over:]
	}
	w.r.mu.Unlock()
	return len(b), nil
}

// conn is one run of the command: what it writes is read here, and what is
// written to it goes through send.
type conn struct {
	r   *Run
	ctx context.Context
	mu  sync.Mutex
	in  io.WriteCloser
}

func (c *conn) send(v any) {
	b, _ := json.Marshal(v)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.in.Write(append(b, '\n'))
}

type resultFrame struct {
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
	Turns   int    `json:"num_turns"`
	Session string `json:"session_id"`
}

type frame struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	Session   string          `json:"session_id"`
	APIKey    string          `json:"apiKeySource"`
	Parent    *string         `json:"parent_tool_use_id"`
	Message   json.RawMessage `json:"message"`
	RequestID string          `json:"request_id"`
	Request   json.RawMessage `json:"request"`
	Response  json.RawMessage `json:"response"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
}

// read takes frames until the result, which it returns (nil when the
// output ended first). A result ends the conversation: stdin closes and
// the command exits.
func (c *conn) read(out *bufio.Reader) *resultFrame {
	for {
		line, err := out.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			var f frame
			if json.Unmarshal(line, &f) == nil {
				if res := c.handle(f, line); res != nil {
					return res
				}
			}
		}
		if err != nil {
			return nil
		}
	}
}

func (c *conn) handle(f frame, line []byte) *resultFrame {
	r := c.r
	switch f.Type {
	case "system":
		if f.Subtype == "init" {
			r.mu.Lock()
			r.session = f.Session
			if f.APIKey != "" && f.APIKey != "none" {
				r.account = "API key"
			}
			r.mu.Unlock()
		}
	case "control_response":
		var resp struct {
			Response struct {
				Account struct {
					Subscription string `json:"subscriptionType"`
				} `json:"account"`
			} `json:"response"`
		}
		json.Unmarshal(f.Response, &resp)
		if s := resp.Response.Account.Subscription; s != "" {
			r.mu.Lock()
			if r.account == "" {
				r.account = s
			}
			r.mu.Unlock()
		}
	case "assistant":
		var msg struct{ Content []block }
		json.Unmarshal(f.Message, &msg)
		r.mu.Lock()
		for _, b := range msg.Content {
			switch b.Type {
			case "text":
				if f.Parent == nil && strings.TrimSpace(b.Text) != "" {
					r.text = b.Text
				}
			case "tool_use":
				r.steps = append(r.steps, Step{ID: b.ID, Tool: b.Name, Summary: summary(b.Input, r.real), Nested: f.Parent != nil})
			}
		}
		r.mu.Unlock()
	case "user":
		var msg struct {
			Content json.RawMessage
		}
		json.Unmarshal(f.Message, &msg)
		var blocks []block
		json.Unmarshal(msg.Content, &blocks)
		r.mu.Lock()
		for _, b := range blocks {
			if b.Type != "tool_result" {
				continue
			}
			for i := range r.steps {
				if r.steps[i].ID == b.ToolUseID {
					r.steps[i].Done, r.steps[i].Failed = true, b.IsError
				}
			}
		}
		r.mu.Unlock()
	case "control_request":
		go c.answer(f)
	case "result":
		var res resultFrame
		json.Unmarshal(line, &res)
		if res.Session != "" {
			r.mu.Lock()
			r.session = res.Session
			r.mu.Unlock()
		}
		return &res
	}
	return nil
}

// noQuestions is what Claude Code reads when it asks the user a question.
const noQuestions = "Nobody can answer questions during this task. Decide for yourself, and say what you assumed in your final report."

// answer replies to a request of Claude Code's: a tool to approve, or
// something this host does not do.
func (c *conn) answer(f frame) {
	var req struct {
		Subtype     string          `json:"subtype"`
		Tool        string          `json:"tool_name"`
		Input       json.RawMessage `json:"input"`
		Description string          `json:"description"`
	}
	json.Unmarshal(f.Request, &req)
	reply := func(resp map[string]any) {
		c.send(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": f.RequestID, "response": resp}})
	}
	if req.Subtype != "can_use_tool" {
		c.send(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "error", "request_id": f.RequestID, "error": "not supported: " + req.Subtype}})
		return
	}
	deny := func(why string) { reply(map[string]any{"behavior": "deny", "message": why}) }
	if req.Tool == "AskUserQuestion" {
		deny(noQuestions)
		return
	}
	input := req.Input
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	if ap := c.r.opts.Approve; ap != nil {
		ok, why := ap(c.ctx, Request{ID: f.RequestID, Tool: req.Tool, Input: input, Description: req.Description})
		if !ok {
			if why == "" {
				why = "The user declined this."
			}
			deny(why)
			return
		}
	}
	reply(map[string]any{"behavior": "allow", "updatedInput": input})
}

// summary is a tool call's input in a few words: the command, the file,
// the pattern.
func summary(input json.RawMessage, dir string) string {
	var a map[string]any
	if json.Unmarshal(input, &a) != nil {
		return ""
	}
	for _, k := range []string{"command", "file_path", "notebook_path", "path", "pattern", "url", "query", "description", "prompt"} {
		s, ok := a[k].(string)
		if !ok || s == "" {
			continue
		}
		if strings.HasSuffix(k, "path") {
			if rel, err := filepath.Rel(dir, s); err == nil && !strings.HasPrefix(rel, "..") {
				s = rel
			}
		}
		return oneLine(s, 120)
	}
	return ""
}

// Describe is a request in words, for a review and a card.
func Describe(req Request) string {
	if s := summary(req.Input, ""); s != "" {
		return req.Tool + ": " + s
	}
	return req.Tool + " " + oneLine(string(req.Input), 400)
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	return oneLine(s[strings.LastIndexByte(s, '\n')+1:], 300)
}
