package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func resolve(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

// ReadFile reads a text file, whole or by line range. Relative paths are
// relative to Dir.
type ReadFile struct{ Dir string }

func (ReadFile) Name() string { return "read_file" }
func (ReadFile) Description() string {
	return "Read a text file. Returns at most 2000 lines or 50 KB from `offset`; " +
		"when cut, the note at the end says where to continue."
}
func (ReadFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
"path":{"type":"string","description":"File path, absolute or relative to the working directory."},
"offset":{"type":"integer","minimum":1,"description":"First line to read, 1-based. Default 1."},
"limit":{"type":"integer","minimum":1,"description":"Maximum number of lines."}
},"required":["path"],"additionalProperties":false}`)
}

func (r ReadFile) Execute(_ context.Context, args json.RawMessage) Result {
	var a struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	b, err := os.ReadFile(resolve(r.Dir, a.Path))
	if err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	if bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 {
		return Result{Text: a.Path + " is a binary file", IsError: true}
	}

	lines := strings.Split(string(b), "\n")
	total := len(lines)
	start := max(a.Offset, 1) - 1
	if start >= total {
		return Result{Text: fmt.Sprintf("offset %d is past the end of the file (%d lines)", a.Offset, total), IsError: true}
	}
	lines = lines[start:]
	limited := false
	if a.Limit > 0 && a.Limit < len(lines) {
		lines, limited = lines[:a.Limit], true
	}
	text, kept, cut := truncateHead(strings.Join(lines, "\n"))
	if cut || limited {
		next := start + kept + 1
		text += note("showing lines %d-%d of %d; continue with offset %d", start+1, start+kept, total, next)
	}
	return Result{Text: text}
}

// WriteFile writes a whole file, creating parent directories.
type WriteFile struct{ Dir string }

func (WriteFile) Name() string { return "write_file" }
func (WriteFile) Description() string {
	return "Write a file, replacing it if it exists and creating parent directories."
}
func (WriteFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
"path":{"type":"string","description":"File path, absolute or relative to the working directory."},
"content":{"type":"string","description":"The complete new content."}
},"required":["path","content"],"additionalProperties":false}`)
}

func (w WriteFile) Execute(_ context.Context, args json.RawMessage) Result {
	var a struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	p := resolve(w.Dir, a.Path)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	if err := os.WriteFile(p, []byte(a.Content), 0o644); err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	return Result{Text: fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path)}
}
