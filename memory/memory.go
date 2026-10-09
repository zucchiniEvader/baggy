// Package memory is a bot's long-term memory, plain markdown in its
// workspace: MEMORY.md, the curated facts that open every turn's system
// prompt; memory/<topic>.md, longer notes it reads when it needs them; and
// memory/log/YYYY-MM-DD.md, its diary of what happened, which never loads
// into a prompt and is found with recall.
package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// LoadLines and LoadBytes bound the part of MEMORY.md a prompt carries,
	// whichever cuts first.
	LoadLines = 200
	LoadBytes = 24 << 10
)

// Memory is one bot's memory folder (its workspace).
type Memory struct {
	Dir string
	Now func() time.Time
}

func New(workspace string) *Memory { return &Memory{Dir: workspace, Now: time.Now} }

// One lock per folder, so two turns of a bot never interleave a write.
var locks sync.Map

func (m *Memory) lock() func() {
	v, _ := locks.LoadOrStore(m.Dir, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (m *Memory) Index() string   { return filepath.Join(m.Dir, "MEMORY.md") }
func (m *Memory) Topics() string  { return filepath.Join(m.Dir, "memory") }
func (m *Memory) LogDir() string  { return filepath.Join(m.Dir, "memory", "log") }
func (m *Memory) now() time.Time  { return m.Now() }
func (m *Memory) today() string   { return m.now().Format("2006-01-02") }
func readFile(p string) string    { b, _ := os.ReadFile(p); return string(b) }
func (m *Memory) Read() string    { return readFile(m.Index()) }

// write replaces a file atomically, after scrubbing credentials.
func write(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(Scrub(text)); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Ops of Update.
const (
	Append    = "append"    // add a fact; an exact duplicate is refused
	Replace   = "replace"   // rewrite one unique passage
	Supersede = "supersede" // strike an entry through and add the new fact
	Remove    = "remove"    // delete one unique passage
)

// ErrNotUnique is returned when old matches no passage or several.
var ErrNotUnique = errors.New("the passage must appear exactly once in MEMORY.md")

// Update changes MEMORY.md by one dated entry: "- 2026-09-17 · from <source>
// · <text>". old names the passage replace, supersede and remove act on.
func (m *Memory) Update(op, text, old, source string) (string, error) {
	defer m.lock()()
	cur := m.Read()
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	entry := fmt.Sprintf("- %s · from %s · %s", m.today(), source, text)
	unique := func() (int, error) {
		old = strings.TrimSpace(old)
		if old == "" || strings.Count(cur, old) != 1 {
			return 0, ErrNotUnique
		}
		return strings.Index(cur, old), nil
	}
	switch op {
	case Append, "":
		if text == "" {
			return "", errors.New("nothing to remember")
		}
		if strings.Contains(cur, "· "+text+"\n") || strings.HasSuffix(cur, "· "+text) {
			return "", errors.New("already in memory")
		}
		cur = appendLine(cur, entry)
	case Replace:
		i, err := unique()
		if err != nil {
			return "", err
		}
		cur = cur[:i] + text + cur[i+len(old):]
	case Supersede:
		i, err := unique()
		if err != nil {
			return "", err
		}
		// Strike the whole line holding the passage.
		start := strings.LastIndexByte(cur[:i], '\n') + 1
		end := strings.IndexByte(cur[i:], '\n')
		if end < 0 {
			end = len(cur)
		} else {
			end += i
		}
		line := strings.TrimPrefix(cur[start:end], "- ")
		cur = cur[:start] + fmt.Sprintf("- ~~%s~~ (superseded %s)", line, m.today()) + cur[end:]
		cur = appendLine(cur, entry)
	case Remove:
		i, err := unique()
		if err != nil {
			return "", err
		}
		cur = cur[:i] + cur[i+len(old):]
		cur = blankLines.ReplaceAllString(cur, "\n")
	default:
		return "", fmt.Errorf("unknown op %q (append, replace, supersede, remove)", op)
	}
	return entry, write(m.Index(), cur)
}

var blankLines = regexp.MustCompile(`\n(\s*-?\s*\n)+`)

func appendLine(cur, line string) string {
	if cur != "" && !strings.HasSuffix(cur, "\n") {
		cur += "\n"
	}
	return cur + line + "\n"
}

// Log adds a line to today's diary: "- 15:04 · <line>".
func (m *Memory) Log(line string) error {
	defer m.lock()()
	now := m.now()
	path := filepath.Join(m.LogDir(), now.Format("2006-01-02")+".md")
	cur := readFile(path)
	line = strings.TrimSpace(strings.ReplaceAll(line, "\n", " "))
	return write(path, appendLine(cur, fmt.Sprintf("- %s · %s", now.Format("15:04"), line)))
}

// Prompt is the memory part of a system prompt: MEMORY.md up to its load
// budget (saying how much is cut off), and the topic notes there are.
func (m *Memory) Prompt() string {
	text := m.Read()
	var b strings.Builder
	b.WriteString("## Your memory\n\nMEMORY.md, the facts you keep (change it only with memory_update; add events to your diary with memory_log; search both and your chats with recall):\n\n")
	if strings.TrimSpace(text) == "" {
		b.WriteString("(empty)\n")
	} else {
		loaded, cut := budget(text)
		b.WriteString(loaded)
		if !strings.HasSuffix(loaded, "\n") {
			b.WriteString("\n")
		}
		if cut > 0 {
			fmt.Fprintf(&b, "\n(%d more lines of MEMORY.md are past the load budget and not shown: consolidate it with memory_update replace and remove.)\n", cut)
		}
	}
	if topics := m.TopicNames(); len(topics) > 0 {
		b.WriteString("\nLonger notes in memory/, read them with read_file when you need them: " + strings.Join(topics, ", ") + "\n")
	}
	return b.String()
}

// budget cuts text to LoadLines lines or LoadBytes bytes and counts the
// lines left out.
func budget(text string) (string, int) {
	lines := strings.SplitAfter(text, "\n")
	n, size := 0, 0
	for n < len(lines) && n < LoadLines && size+len(lines[n]) <= LoadBytes {
		size += len(lines[n])
		n++
	}
	cut := 0
	for _, l := range lines[n:] {
		if l != "" {
			cut++
		}
	}
	return strings.Join(lines[:n], ""), cut
}

// TopicNames lists memory/*.md.
func (m *Memory) TopicNames() []string {
	es, _ := os.ReadDir(m.Topics())
	var out []string
	for _, e := range es {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			out = append(out, "memory/"+e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Hit is a line recall found.
type Hit struct {
	Where string // a file, or a chat
	Who   string // the speaker, for a chat
	When  time.Time
	Text  string
}

// Search finds the lines of the memory files holding every word of query,
// diary lines after since only when since is set.
func (m *Memory) Search(query string, since time.Time) []Hit {
	words := Words(query)
	var hits []Hit
	scan := func(path, rel string, when time.Time) {
		for _, line := range strings.Split(readFile(path), "\n") {
			if strings.TrimSpace(line) != "" && Matches(line, words) {
				hits = append(hits, Hit{Where: rel, When: when, Text: strings.TrimSpace(line)})
			}
		}
	}
	if since.IsZero() {
		scan(m.Index(), "MEMORY.md", time.Time{})
		for _, t := range m.TopicNames() {
			scan(filepath.Join(m.Dir, t), t, time.Time{})
		}
	}
	logs, _ := os.ReadDir(m.LogDir())
	for _, e := range logs {
		day, err := time.ParseInLocation("2006-01-02", strings.TrimSuffix(e.Name(), ".md"), time.Local)
		if err != nil || (!since.IsZero() && day.Add(24*time.Hour).Before(since)) {
			continue
		}
		scan(filepath.Join(m.LogDir(), e.Name()), "memory/log/"+e.Name(), day)
	}
	return hits
}

// Words are the lowercased words of a query.
func Words(q string) []string {
	return strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return r == ' ' || r == ',' || r == '.' || r == '"' || r == '\'' || r == '\t' || r == '\n'
	})
}

// Matches reports whether text holds every word.
func Matches(text string, words []string) bool {
	if len(words) == 0 {
		return false
	}
	low := strings.ToLower(text)
	for _, w := range words {
		if !strings.Contains(low, w) {
			return false
		}
	}
	return true
}

var secrets = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`\b(sk|pk|rk)-[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{16,}`),
}

var passwordRe = regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|api[_-]?key|token)(\s*[:=]\s*)(\S+)`)

// Scrub replaces credentials with «redacted N chars»: API keys, AWS, GitHub
// and Slack tokens, JWTs, bearer tokens, private key blocks, and the values
// of password:, token: and the like.
func Scrub(s string) string {
	red := func(x string) string { return fmt.Sprintf("«redacted %d chars»", len([]rune(x))) }
	for _, re := range secrets {
		s = re.ReplaceAllStringFunc(s, red)
	}
	return passwordRe.ReplaceAllStringFunc(s, func(x string) string {
		m := passwordRe.FindStringSubmatch(x)
		if strings.HasPrefix(m[3], "«redacted") {
			return x
		}
		return m[1] + m[2] + red(m[3])
	})
}

// ErrChanged is returned by Save when MEMORY.md changed after it was read.
var ErrChanged = errors.New("MEMORY.md changed since it was opened")

// Save replaces MEMORY.md with text, the person's edit, unless it no longer
// holds was, what the editor opened.
func (m *Memory) Save(was, text string) error {
	defer m.lock()()
	if m.Read() != was {
		return ErrChanged
	}
	return write(m.Index(), text)
}

// Loaded reports how much of MEMORY.md a prompt carries: lines and bytes
// loaded, and the lines past the budget.
func (m *Memory) Loaded() (lines, bytes, cut int) {
	loaded, cut := budget(m.Read())
	return strings.Count(loaded, "\n"), len(loaded), cut
}
