// Package store keeps gobot's state in one SQLite file: bots, chats and their
// members, the messages of every chat, providers, and settings. Bot
// workspaces are folders beside it, keyed by bot id.
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned for an id no row has.
var ErrNotFound = errors.New("not found")

type Store struct {
	db         *sql.DB
	dir        string
	workspaces string // the bots' folders: dir/workspaces unless UseWorkspaces moved them
}

// Open opens (creating as needed) dir/baggy.db; one named gobot.db, from
// when the app was gobot, is renamed first.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	for _, ext := range []string{"", "-wal", "-shm"} { // the app was gobot
		old, path := filepath.Join(dir, "gobot.db"+ext), filepath.Join(dir, "baggy.db"+ext)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			os.Rename(old, path)
		}
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "baggy.db")+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one writer; reads are quick
	s := &Store{db: db, dir: dir, workspaces: filepath.Join(dir, "workspaces")}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Dir is the folder the store lives in.
func (s *Store) Dir() string { return s.dir }

// Workspace is a bot's own folder, keyed by id: MEMORY.md, memory/, the
// files attached to its chats, and its work unless it has a Workdir.
func (s *Store) Workspace(botID string) string { return filepath.Join(s.workspaces, botID) }

// Workspaces is the folder the bots' workspaces are in.
func (s *Store) Workspaces() string { return s.workspaces }

// UseWorkspaces keeps the bots' workspaces in root (as ~/.gobot/workspaces)
// rather than beside the database. A workspace still in the old folder is
// moved over, unless root has one for that bot already; the old folder goes
// when nothing is left in it.
func (s *Store) UseWorkspaces(root string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	old := s.workspaces
	s.workspaces = root
	if filepath.Clean(old) == filepath.Clean(root) {
		return nil
	}
	ents, err := os.ReadDir(old)
	if err != nil {
		return nil // nothing to move
	}
	var failed []string
	for _, e := range ents {
		from, to := filepath.Join(old, e.Name()), filepath.Join(root, e.Name())
		if _, err := os.Lstat(to); err == nil {
			continue // already there: the old one stays where it was
		}
		if err := move(from, to); err != nil {
			failed = append(failed, e.Name()+": "+err.Error())
		}
	}
	os.Remove(old) // only when empty
	if len(failed) > 0 {
		return fmt.Errorf("some workspaces were not moved to %s: %s", root, strings.Join(failed, "; "))
	}
	return nil
}

// move renames a folder, copying it when the two are on different volumes.
func move(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	if err := os.CopyFS(to, os.DirFS(from)); err != nil {
		os.RemoveAll(to)
		return err
	}
	return os.RemoveAll(from)
}

// checkWorkdir refuses a working folder that is not a full path to a
// folder there is.
func (s *Store) checkWorkdir(b Bot) error {
	d := strings.TrimSpace(b.Workdir)
	if d == "" {
		return nil
	}
	if d != "~" && !strings.HasPrefix(d, "~/") && !filepath.IsAbs(d) {
		return fmt.Errorf("the working folder %q is not a full path", d)
	}
	if info, err := os.Stat(s.WorkDir(b)); err != nil || !info.IsDir() {
		return fmt.Errorf("there is no folder %s", d)
	}
	return nil
}

// WorkDir is where a bot's tools run: its Workdir, ~ read as the home
// folder, else its workspace.
func (s *Store) WorkDir(b Bot) string {
	d := strings.TrimSpace(b.Workdir)
	if d == "" {
		return s.Workspace(b.ID)
	}
	if home, err := os.UserHomeDir(); err == nil {
		if d == "~" {
			return home
		}
		if rest, ok := strings.CutPrefix(d, "~/"); ok {
			return filepath.Join(home, rest)
		}
	}
	return d
}

var migrations = []string{`
CREATE TABLE providers (
	id TEXT PRIMARY KEY, name TEXT NOT NULL, api TEXT NOT NULL,
	base_url TEXT NOT NULL DEFAULT '', api_key TEXT NOT NULL DEFAULT '',
	models TEXT NOT NULL DEFAULT '[]', options TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE bots (
	id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
	provider TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', thinking TEXT NOT NULL DEFAULT '',
	created INTEGER NOT NULL
);
CREATE TABLE chats (
	id TEXT PRIMARY KEY, kind TEXT NOT NULL, title TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '', owner TEXT NOT NULL DEFAULT '',
	created INTEGER NOT NULL, updated INTEGER NOT NULL
);
CREATE TABLE members (
	chat_id TEXT NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
	bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
	pos INTEGER NOT NULL,
	PRIMARY KEY (chat_id, bot_id)
);
CREATE TABLE messages (
	chat_id TEXT NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
	seq INTEGER NOT NULL,
	author TEXT NOT NULL,
	turn TEXT NOT NULL DEFAULT '',
	meta TEXT NOT NULL DEFAULT '{}',
	body TEXT NOT NULL,
	created INTEGER NOT NULL,
	PRIMARY KEY (chat_id, seq)
);
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`, `
CREATE TABLE routines (
	id TEXT PRIMARY KEY,
	bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
	name TEXT NOT NULL, prompt TEXT NOT NULL, schedule TEXT NOT NULL,
	timezone TEXT NOT NULL DEFAULT '', missed TEXT NOT NULL DEFAULT 'coalesce',
	check_code TEXT NOT NULL DEFAULT '',
	enabled INTEGER NOT NULL DEFAULT 1, paused_reason TEXT NOT NULL DEFAULT '',
	last_scheduled_at INTEGER NOT NULL DEFAULT 0, last_run_at INTEGER NOT NULL DEFAULT 0,
	last_outcome TEXT NOT NULL DEFAULT '', last_check_at INTEGER NOT NULL DEFAULT 0,
	last_check TEXT NOT NULL DEFAULT '',
	created INTEGER NOT NULL, enabled_at INTEGER NOT NULL
);
`, `
CREATE TABLE IF NOT EXISTS script_store (
	chat_id TEXT NOT NULL, bot_id TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL,
	PRIMARY KEY (chat_id, bot_id, key)
);
`, `
ALTER TABLE bots ADD COLUMN emoji TEXT NOT NULL DEFAULT '';
ALTER TABLE bots ADD COLUMN color TEXT NOT NULL DEFAULT '';
ALTER TABLE chats ADD COLUMN read_seq INTEGER NOT NULL DEFAULT 0;
`, `
ALTER TABLE chats ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;
`, `
ALTER TABLE bots ADD COLUMN avatar TEXT NOT NULL DEFAULT '';
`, `
ALTER TABLE bots ADD COLUMN workdir TEXT NOT NULL DEFAULT '';
`, `
ALTER TABLE bots ADD COLUMN template TEXT NOT NULL DEFAULT '';
UPDATE bots SET template = 'chef' WHERE name = 'Chef';
`}

func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// NewID returns prefix-xxxxxxxx with eight random hex digits.
func NewID(prefix string) string {
	b := make([]byte, 4)
	rand.Read(b)
	return prefix + "-" + hex.EncodeToString(b)
}

func now() int64 { return time.Now().UnixMilli() }

// ---- settings ----

// Setting returns the value of key, "" when unset.
func (s *Store) Setting(key string) string {
	var v string
	s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	return v
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// ---- providers ----

// Provider is a configured server: the protocol it speaks (connect.Anthropic,
// connect.Chat, connect.Responses), where, the key, and the models offered.
type Provider struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	API     string   `json:"api"`
	BaseURL string   `json:"base_url"`
	APIKey  string   `json:"api_key"`
	Models  []string `json:"models"`
	// Options are adapter switches, e.g. "replay_reasoning".
	Options map[string]bool `json:"options"`
}

// SaveProvider inserts or replaces p; an empty ID gets a new one.
func (s *Store) SaveProvider(p Provider) (Provider, error) {
	if p.ID == "" {
		p.ID = NewID("prov")
	}
	models, _ := json.Marshal(nonNil(p.Models))
	opts, _ := json.Marshal(p.Options)
	_, err := s.db.Exec(`INSERT INTO providers(id, name, api, base_url, api_key, models, options) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, api=excluded.api, base_url=excluded.base_url,
		api_key=excluded.api_key, models=excluded.models, options=excluded.options`,
		p.ID, p.Name, p.API, p.BaseURL, p.APIKey, string(models), string(opts))
	return p, err
}

func (s *Store) Providers() ([]Provider, error) {
	rows, err := s.db.Query(`SELECT id, name, api, base_url, api_key, models, options FROM providers ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Provider
	for rows.Next() {
		var p Provider
		var models, opts string
		if err := rows.Scan(&p.ID, &p.Name, &p.API, &p.BaseURL, &p.APIKey, &models, &opts); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(models), &p.Models)
		json.Unmarshal([]byte(opts), &p.Options)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Provider(id string) (Provider, error) {
	ps, err := s.Providers()
	if err != nil {
		return Provider{}, err
	}
	for _, p := range ps {
		if p.ID == id {
			return p, nil
		}
	}
	return Provider{}, ErrNotFound
}

func (s *Store) DeleteProvider(id string) error {
	_, err := s.db.Exec(`DELETE FROM providers WHERE id = ?`, id)
	return err
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// ---- bots ----

// Bot is a name, a description (its system prompt), the model it runs on, and
// its workspace (Store.Workspace).
type Bot struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Provider    string `json:"provider"` // a Provider id; "" is the default provider
	Model       string `json:"model"`    // "" is the provider's first model
	Thinking    string `json:"thinking"` // a provider.ThinkingLevel
	Icon        string `json:"icon"`     // its look: an icon's name (the desktop's set); "" is the default
	Color       string `json:"color"`    // its avatar's color, a name of the desktop's set; "" is one picked by its id
	Avatar      string `json:"avatar"`   // its own picture, a file in avatars/ (SaveAvatar); "" is its icon
	Workdir     string `json:"workdir"`  // where its tools run (~ is the home folder); "" is its workspace
	Template    string `json:"template"` // the Template it was made from; "" for none
	Created     int64  `json:"created"`
}

const botCols = `id, name, description, provider, model, thinking, emoji, color, avatar, workdir, template, created` // emoji holds the icon

func scanBot(sc interface{ Scan(...any) error }) (Bot, error) {
	var b Bot
	err := sc.Scan(&b.ID, &b.Name, &b.Description, &b.Provider, &b.Model, &b.Thinking, &b.Icon, &b.Color, &b.Avatar, &b.Workdir, &b.Template, &b.Created)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return b, err
}

// CreateBot adds b with its DM and workspace folder.
func (s *Store) CreateBot(b Bot) (Bot, error) {
	b.Name = strings.TrimSpace(b.Name)
	if b.Name == "" {
		return Bot{}, errors.New("a bot needs a name")
	}
	if err := s.checkWorkdir(b); err != nil {
		return Bot{}, err
	}
	if b.ID == "" {
		b.ID = NewID("bot")
	}
	b.Created = now()
	if err := os.MkdirAll(s.Workspace(b.ID), 0o755); err != nil {
		return Bot{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Bot{}, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO bots(`+botCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.Name, b.Description, b.Provider, b.Model, b.Thinking, b.Icon, b.Color, b.Avatar, strings.TrimSpace(b.Workdir), b.Template, b.Created); err != nil {
		return Bot{}, err
	}
	chat := NewID("chat")
	if _, err := tx.Exec(`INSERT INTO chats(id, kind, owner, created, updated) VALUES(?, 'dm', ?, ?, ?)`, chat, b.ID, b.Created, b.Created); err != nil {
		return Bot{}, err
	}
	if _, err := tx.Exec(`INSERT INTO members(chat_id, bot_id, pos) VALUES(?, ?, 0)`, chat, b.ID); err != nil {
		return Bot{}, err
	}
	return b, tx.Commit()
}

// UpdateBot changes a bot's name, description and model.
func (s *Store) UpdateBot(b Bot) error {
	if strings.TrimSpace(b.Name) == "" {
		return errors.New("a bot needs a name")
	}
	if err := s.checkWorkdir(b); err != nil {
		return err
	}
	old, err := s.Bot(b.ID)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE bots SET name=?, description=?, provider=?, model=?, thinking=?, emoji=?, color=?, avatar=?, workdir=? WHERE id=?`,
		strings.TrimSpace(b.Name), b.Description, b.Provider, b.Model, b.Thinking, b.Icon, b.Color, b.Avatar, strings.TrimSpace(b.Workdir), b.ID)
	if err := notFound(res, err); err != nil {
		return err
	}
	if old.Avatar != "" && old.Avatar != b.Avatar {
		os.Remove(s.AvatarPath(old.Avatar))
	}
	return nil
}

// SaveAvatar keeps a picture for a bot's avatar and gives the name a Bot's
// Avatar takes; a new name each time, so a changed picture is never read
// from a cache.
func (s *Store) SaveAvatar(png []byte) (string, error) {
	name := NewID("avatar") + ".png"
	if err := os.MkdirAll(filepath.Join(s.dir, "avatars"), 0o755); err != nil {
		return "", err
	}
	return name, os.WriteFile(s.AvatarPath(name), png, 0o644)
}

// AvatarPath is where an avatar picture is kept.
func (s *Store) AvatarPath(name string) string {
	return filepath.Join(s.dir, "avatars", filepath.Base(name))
}

func notFound(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Bot(id string) (Bot, error) {
	return scanBot(s.db.QueryRow(`SELECT `+botCols+` FROM bots WHERE id = ?`, id))
}

// Bots lists every bot, oldest first.
func (s *Store) Bots() ([]Bot, error) {
	rows, err := s.db.Query(`SELECT ` + botCols + ` FROM bots ORDER BY created, rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Bot
	for rows.Next() {
		b, err := scanBot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// BotsNamed returns the bots whose name is name, ignoring case.
func (s *Store) BotsNamed(name string) ([]Bot, error) {
	all, err := s.Bots()
	if err != nil {
		return nil, err
	}
	var out []Bot
	for _, b := range all {
		if strings.EqualFold(b.Name, strings.TrimSpace(name)) {
			out = append(out, b)
		}
	}
	return out, nil
}

// DeleteBot removes a bot, its DM, and its seat in every group. A group left
// without bots is removed; one whose owner it was passes to its first
// remaining member. The workspace folder is kept.
func (s *Store) DeleteBot(id string) error {
	groups, err := s.chatsOf(id)
	if err != nil {
		return err
	}
	old, _ := s.Bot(id) // its avatar goes with it
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, c := range groups {
		if c.Kind == DM {
			if _, err := tx.Exec(`DELETE FROM chats WHERE id = ?`, c.ID); err != nil {
				return err
			}
			continue
		}
		if err := removeMember(tx, c, id); err != nil {
			return err
		}
	}
	res, err := tx.Exec(`DELETE FROM bots WHERE id = ?`, id)
	if err := notFound(res, err); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if old.Avatar != "" {
		os.Remove(s.AvatarPath(old.Avatar))
	}
	return nil
}

// Seed creates Chef when there are no bots yet and returns whether it did.
func (s *Store) Seed() (bool, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM bots`).Scan(&n); err != nil || n > 0 {
		return false, err
	}
	if s.Setting("seeded") != "" {
		return false, nil // the user deleted every bot; don't bring Chef back
	}
	chef, _ := TemplateOf("chef")
	if _, err := s.CreateBot(chef.Bot()); err != nil {
		return false, err
	}
	return true, s.SetSetting("seeded", "1")
}

// ScriptLoad and ScriptSave keep codemode's values per chat and bot.
func (s *Store) ScriptLoad(chatID, botID, key string) (string, bool) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM script_store WHERE chat_id = ? AND bot_id = ? AND key = ?`, chatID, botID, key).Scan(&v)
	return v, err == nil
}

func (s *Store) ScriptSave(chatID, botID, key, value string) error {
	_, err := s.db.Exec(`INSERT INTO script_store(chat_id, bot_id, key, value) VALUES(?,?,?,?)
		ON CONFLICT(chat_id, bot_id, key) DO UPDATE SET value = excluded.value`, chatID, botID, key, value)
	return err
}
