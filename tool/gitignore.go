package tool

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// ignoreRule is one line of a .gitignore, under the folder that holds it.
type ignoreRule struct {
	base   string // slash path of the folder, "" for the root
	re     *regexp.Regexp
	negate bool
	dir    bool // matches folders only
}

// ignorer holds the rules of the .gitignore files found so far.
type ignorer struct{ rules []ignoreRule }

// load reads dir's .gitignore (rel is dir's slash path under the root).
func (g *ignorer) load(dir, rel string) {
	b, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimRight(line, " \r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r := ignoreRule{base: rel}
		if strings.HasPrefix(line, "!") {
			r.negate, line = true, line[1:]
		}
		if strings.HasSuffix(line, "/") {
			r.dir, line = true, strings.TrimSuffix(line, "/")
		}
		anchored := strings.Contains(strings.TrimSuffix(line, "/"), "/")
		line = strings.TrimPrefix(line, "/")
		pattern := line
		if !anchored {
			pattern = "**/" + line
		}
		re, err := globRegexp(pattern)
		if err != nil {
			continue
		}
		r.re = re
		g.rules = append(g.rules, r)
	}
}

// ignored reports whether the slash path rel (under the root) is ignored;
// the last rule that matches decides.
func (g *ignorer) ignored(rel string, isDir bool) bool {
	out := false
	for _, r := range g.rules {
		p := rel
		if r.base != "" {
			if !strings.HasPrefix(rel, r.base+"/") {
				continue
			}
			p = strings.TrimPrefix(rel, r.base+"/")
		}
		if r.dir && !isDir {
			// a folder rule still covers what is inside the folder
			if dirMatch(r, p) {
				out = !r.negate
			}
			continue
		}
		if r.re.MatchString(p) {
			out = !r.negate
		}
	}
	return out
}

func dirMatch(r ignoreRule, p string) bool {
	for d := path.Dir(p); d != "." && d != "/"; d = path.Dir(d) {
		if r.re.MatchString(d) {
			return true
		}
	}
	return false
}
