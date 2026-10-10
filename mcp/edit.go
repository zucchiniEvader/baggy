package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// known are the keys of an entry that Server reads; SaveServer writes them
// and keeps every other key as it was.
var known = []string{"command", "args", "env", "cwd", "type", "url", "headers", "description", "disabled", "timeout"}

// SaveServer writes s to the file at path as the entry named old ("" for a
// new one), renaming it to s.Name. The file's other entries, its other
// keys, and the keys of the entry Server does not read stay as they were.
func SaveServer(path, old string, s Server) error {
	name := strings.TrimSpace(s.Name)
	switch {
	case name == "":
		return errors.New("a server needs a name")
	case s.URL == "" && s.Command == "":
		return errors.New("a server needs a command or a URL")
	}
	return edit(path, func(servers map[string]json.RawMessage) error {
		for n := range servers {
			if n != old && (n == name || ID(n) == ID(name)) {
				return fmt.Errorf("there is a server named %s already", n)
			}
		}
		entry := map[string]json.RawMessage{}
		if old != "" {
			if raw, ok := servers[old]; ok {
				json.Unmarshal(raw, &entry)
			}
			delete(servers, old)
		}
		for _, k := range known {
			delete(entry, k)
		}
		b, _ := json.Marshal(s)
		var fields map[string]json.RawMessage
		json.Unmarshal(b, &fields)
		for k, v := range fields {
			entry[k] = v
		}
		raw, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		servers[name] = raw
		return nil
	})
}

// DeleteServer takes the entry named name out of the file at path.
func DeleteServer(path, name string) error {
	return edit(path, func(servers map[string]json.RawMessage) error {
		if _, ok := servers[name]; !ok {
			return fmt.Errorf("no server named %s", name)
		}
		delete(servers, name)
		return nil
	})
}

// edit changes the servers of the file at path (made when missing) and
// writes it back whole, through a temporary file.
func edit(path string, change func(map[string]json.RawMessage) error) error {
	top := map[string]json.RawMessage{}
	key := "mcpServers"
	if b, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(b)) > 0 {
		if err := json.Unmarshal(b, &top); err != nil {
			return fmt.Errorf("mcp.json does not read: %w", err)
		}
		if _, ok := top[key]; !ok {
			if _, ok := top["servers"]; ok {
				key = "servers" // VS Code's spelling, kept
			}
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := top[key]; ok {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return fmt.Errorf("mcp.json: %s: %w", key, err)
		}
	}
	if err := change(servers); err != nil {
		return err
	}
	raw, err := json.Marshal(servers)
	if err != nil {
		return err
	}
	top[key] = raw
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
