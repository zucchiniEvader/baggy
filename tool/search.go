package tool

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// EditFile replaces exact passages of a file: one (old, new), or several
// (edits) applied in order, each to the text the one before left.
type EditFile struct{ Dir string }

func (EditFile) Name() string { return "edit_file" }
func (EditFile) Description() string {
	return "Replace exact passages of a text file. Give old and new for one, or edits [{old, new}] for several, applied in order. " +
		"Each old must appear exactly once unless all is true; include enough surrounding lines to make it unique. Prefer this to rewriting a whole file."
}
func (EditFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
"path":{"type":"string","description":"File path, absolute or relative to the working directory."},
"old":{"type":"string","description":"The exact text to replace, whitespace included."},
"new":{"type":"string","description":"The text to put in its place."},
"all":{"type":"boolean","description":"Replace every occurrence. Default false."},
"edits":{"type":"array","items":{"type":"object","properties":{"old":{"type":"string"},"new":{"type":"string"},"all":{"type":"boolean"}},"required":["old","new"],"additionalProperties":false}}
},"required":["path"],"additionalProperties":false}`)
}

type edit struct {
	Old, New string
	All      bool
}

func (e EditFile) Execute(_ context.Context, args json.RawMessage) Result {
	var a struct {
		Path, Old, New string
		All            bool
		Edits          []edit
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	edits := a.Edits
	if a.Old != "" || len(edits) == 0 {
		edits = append([]edit{{a.Old, a.New, a.All}}, edits...)
	}
	p := resolve(e.Dir, a.Path)
	b, err := os.ReadFile(p)
	if err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	text := string(b)
	crlf := strings.Contains(text, "\r\n")
	total := 0
	for i, ed := range edits {
		which := ""
		if len(edits) > 1 {
			which = fmt.Sprintf("edit %d: ", i+1)
		}
		if ed.Old == "" {
			return Result{Text: which + "old is empty: use write_file to create a file", IsError: true}
		}
		n := strings.Count(text, ed.Old)
		if n == 0 && crlf && !strings.Contains(ed.Old, "\r\n") {
			// The file has Windows line ends and the model wrote Unix ones.
			ed.Old, ed.New = strings.ReplaceAll(ed.Old, "\n", "\r\n"), strings.ReplaceAll(ed.New, "\n", "\r\n")
			n = strings.Count(text, ed.Old)
		}
		switch {
		case n == 0:
			return Result{Text: which + "old was not found in " + a.Path + "; read the file and copy the passage exactly. Nothing was changed.", IsError: true}
		case n > 1 && !ed.All:
			return Result{Text: fmt.Sprintf("%sold appears %d times in %s; add surrounding lines to make it unique, or set all. Nothing was changed.", which, n, a.Path), IsError: true}
		}
		if ed.All {
			text = strings.ReplaceAll(text, ed.Old, ed.New)
			total += n
		} else {
			text = strings.Replace(text, ed.Old, ed.New, 1)
			total++
		}
	}
	info, _ := os.Stat(p)
	if err := os.WriteFile(p, []byte(text), info.Mode().Perm()); err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	return Result{Text: fmt.Sprintf("replaced %d occurrence(s) in %s", total, a.Path)}
}

// Ls lists a folder.
type Ls struct{ Dir string }

func (Ls) Name() string { return "ls" }
func (Ls) Description() string {
	return "List a folder's entries: folders end with /, files show their size. At most 500, folders first."
}
func (Ls) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
"path":{"type":"string","description":"A folder, absolute or relative to the working directory. Default: the working directory."}
},"additionalProperties":false}`)
}

func (l Ls) Execute(_ context.Context, args json.RawMessage) Result {
	var a struct{ Path string }
	json.Unmarshal(args, &a)
	es, err := os.ReadDir(resolve(l.Dir, a.Path))
	if err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	sort.SliceStable(es, func(i, j int) bool { return es[i].IsDir() && !es[j].IsDir() })
	var b strings.Builder
	for i, e := range es {
		if i == 500 {
			b.WriteString(note("%d more entries not shown", len(es)-500))
			break
		}
		if e.IsDir() {
			b.WriteString(e.Name() + "/\n")
			continue
		}
		size := ""
		if info, err := e.Info(); err == nil {
			size = fmt.Sprintf("  %d", info.Size())
		}
		b.WriteString(e.Name() + size + "\n")
	}
	if len(es) == 0 {
		return Result{Text: "(empty)"}
	}
	return Result{Text: strings.TrimRight(b.String(), "\n")}
}

// skipDir is a folder find and grep do not enter.
func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", ".hg", ".svn", "__pycache__", ".venv", "target", ".cache":
		return true
	}
	return false
}

const maxMatches = 200

// Find lists files whose path matches a glob.
type Find struct{ Dir string }

func (Find) Name() string { return "find" }
func (Find) Description() string {
	return "List files under a folder whose path matches a glob: `*` within a name, `**` across folders " +
		"(`**/*.go`, `docs/*.md`). Skips what .gitignore ignores, .git, node_modules and the like. At most 200 paths, newest first."
}
func (Find) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
"pattern":{"type":"string","description":"A glob matched against paths relative to path, e.g. **/*.go"},
"path":{"type":"string","description":"The folder to search, absolute or relative to the working directory. Default: the working directory."}
},"required":["pattern"],"additionalProperties":false}`)
}

func (f Find) Execute(ctx context.Context, args json.RawMessage) Result {
	var a struct{ Pattern, Path string }
	if err := json.Unmarshal(args, &a); err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	root := resolve(f.Dir, a.Path)
	re, err := globRegexp(a.Pattern)
	if err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	type hit struct {
		rel string
		mod int64
	}
	var hits []hit
	ig := &ignorer{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || ctx.Err() != nil {
			return ctx.Err()
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if p != root && (skipDir(d.Name()) || ig.ignored(rel, true)) {
				return filepath.SkipDir
			}
			if p == root {
				rel = ""
			}
			ig.load(p, rel)
			return nil
		}
		if ig.ignored(rel, false) {
			return nil
		}
		if re.MatchString(rel) {
			info, _ := d.Info()
			var mod int64
			if info != nil {
				mod = info.ModTime().UnixNano()
			}
			hits = append(hits, hit{rel, mod})
		}
		return nil
	})
	if err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	if len(hits) == 0 {
		return Result{Text: "no files match " + a.Pattern}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].mod > hits[j].mod })
	var b strings.Builder
	for i, h := range hits {
		if i == maxMatches {
			b.WriteString(note("%d more paths not shown; narrow the pattern", len(hits)-maxMatches))
			break
		}
		b.WriteString(h.rel + "\n")
	}
	return Result{Text: strings.TrimRight(b.String(), "\n")}
}

// globRegexp turns a glob with ** into a regexp over slash paths. A pattern
// without a slash matches a name in any folder.
func globRegexp(glob string) (*regexp.Regexp, error) {
	glob = filepath.ToSlash(strings.TrimPrefix(glob, "./"))
	if !strings.Contains(glob, "/") {
		glob = "**/" + glob
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch {
		case strings.HasPrefix(glob[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '{':
			j := strings.IndexByte(glob[i:], '}')
			if j < 0 {
				return nil, fmt.Errorf("unclosed { in %q", glob)
			}
			alts := strings.Split(glob[i+1:i+j], ",")
			for k := range alts {
				alts[k] = regexp.QuoteMeta(alts[k])
			}
			b.WriteString("(?:" + strings.Join(alts, "|") + ")")
			i += j
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// Grep finds lines matching a regular expression.
type Grep struct{ Dir string }

func (Grep) Name() string { return "grep" }
func (Grep) Description() string {
	return "Search text files for lines matching a regular expression (Go syntax; (?i) for any case). " +
		"Returns path:line: text, at most 200 lines. `glob` limits the files (e.g. *.go). Skips binary files and what .gitignore ignores, .git, node_modules."
}
func (Grep) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
"pattern":{"type":"string","description":"A regular expression."},
"path":{"type":"string","description":"A file or folder, absolute or relative to the working directory. Default: the working directory."},
"glob":{"type":"string","description":"Only files whose path matches this glob."}
},"required":["pattern"],"additionalProperties":false}`)
}

func (g Grep) Execute(ctx context.Context, args json.RawMessage) Result {
	var a struct{ Pattern, Path, Glob string }
	if err := json.Unmarshal(args, &a); err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	re, err := regexp.Compile(a.Pattern)
	if err != nil {
		return Result{Text: "bad pattern: " + err.Error(), IsError: true}
	}
	var only *regexp.Regexp
	if a.Glob != "" {
		if only, err = globRegexp(a.Glob); err != nil {
			return Result{Text: err.Error(), IsError: true}
		}
	}
	root := resolve(g.Dir, a.Path)
	var b strings.Builder
	n, more := 0, 0
	search := func(p, rel string) {
		f, err := os.Open(p)
		if err != nil {
			return
		}
		defer f.Close()
		head := make([]byte, 8000)
		k, _ := f.Read(head)
		if bytes.IndexByte(head[:k], 0) >= 0 {
			return // binary
		}
		f.Seek(0, 0)
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for line := 1; sc.Scan(); line++ {
			if re.Match(sc.Bytes()) {
				if n >= maxMatches {
					more++
					continue
				}
				n++
				fmt.Fprintf(&b, "%s:%d: %s\n", rel, line, clipLine(sc.Text()))
			}
		}
	}
	info, err := os.Stat(root)
	if err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	if !info.IsDir() {
		search(root, path.Base(filepath.ToSlash(a.Path)))
	} else {
		ig := &ignorer{}
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || ctx.Err() != nil {
				return ctx.Err()
			}
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if p != root && (skipDir(d.Name()) || ig.ignored(rel, true)) {
					return filepath.SkipDir
				}
				if p == root {
					rel = ""
				}
				ig.load(p, rel)
				return nil
			}
			if ig.ignored(rel, false) {
				return nil
			}
			if only == nil || only.MatchString(rel) {
				search(p, rel)
			}
			return nil
		})
	}
	if n == 0 {
		return Result{Text: "no matches for " + a.Pattern}
	}
	text := strings.TrimRight(b.String(), "\n")
	if more > 0 {
		text += note("%d more matching lines not shown; narrow the pattern or the path", more)
	}
	return Result{Text: text}
}

func clipLine(s string) string {
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
