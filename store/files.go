package store

import (
	"os"
	"path/filepath"
	"strings"
)

// File is a file attached to a message. Its bytes are kept at
// attachments/<id>/<name> in the store's folder, and copied to the same
// path in the workspace of each bot that reads the message (Place).
type File struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Type string `json:"type,omitempty"` // a media type
	// Image says the message carries the file as an image part; Note says
	// what making it one changed, or why it could not be.
	Image bool   `json:"image,omitempty"`
	Note  string `json:"note,omitempty"`
}

// Rel is the file's path relative to a workspace or the store's folder.
func (f File) Rel() string { return filepath.Join("attachments", f.ID, f.Name) }

// SaveFile keeps data as a new attached file named name.
func (s *Store) SaveFile(name string, data []byte, mediaType string) (File, error) {
	name = cleanName(name)
	f := File{ID: NewID("file"), Name: name, Size: int64(len(data)), Type: mediaType}
	p := s.FilePath(f)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return File{}, err
	}
	return f, os.WriteFile(p, data, 0o644)
}

// FilePath is where an attached file's bytes are kept.
func (s *Store) FilePath(f File) string { return filepath.Join(s.dir, f.Rel()) }

// Place copies an attached file into a bot's workspace, at f.Rel(), unless
// it is there.
func (s *Store) Place(botID string, f File) error {
	dst := filepath.Join(s.Workspace(botID), f.Rel())
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	data, err := os.ReadFile(s.FilePath(f))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// cleanName is a file name with no folders and nothing a path would read
// as one.
func cleanName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == '/' || r == ':' {
			return '_'
		}
		return r
	}, name)
	if name == "." || name == ".." || name == "" || name == "/" {
		name = "file"
	}
	return name
}
