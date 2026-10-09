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
	db  *sql.DB
	dir string
}

// Open opens (creating as needed) dir/gobot.db and dir/workspaces.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "workspaces"), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "gobot.db")+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one writer; reads are quick
	s := &Store{db: db, dir: dir}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Dir is the folder the store lives in.
func (s *Store) Dir() string { return s.dir }

// Workspace is a bot's folder: its files, MEMORY.md, and memory/.
func (s *Store) Workspace(botID string) string { return filepath.Join(s.dir, "workspaces", botID) }

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
	Created     int64  `json:"created"`
}

const botCols = `id, name, description, provider, model, thinking, created`

func scanBot(sc interface{ Scan(...any) error }) (Bot, error) {
	var b Bot
	err := sc.Scan(&b.ID, &b.Name, &b.Description, &b.Provider, &b.Model, &b.Thinking, &b.Created)
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
	if _, err := tx.Exec(`INSERT INTO bots(`+botCols+`) VALUES(?,?,?,?,?,?,?)`,
		b.ID, b.Name, b.Description, b.Provider, b.Model, b.Thinking, b.Created); err != nil {
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
	res, err := s.db.Exec(`UPDATE bots SET name=?, description=?, provider=?, model=?, thinking=? WHERE id=?`,
		strings.TrimSpace(b.Name), b.Description, b.Provider, b.Model, b.Thinking, b.ID)
	return notFound(res, err)
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
	return tx.Commit()
}

// ChefDescription is the first bot's description.
const ChefDescription = `You are Chef, the user's chief of staff. Plan the work the user brings you. Hand each task to the teammate best suited to it with message_bot; when no teammate fits, propose one and create it with create_bot once the user agrees. Do hands-on work yourself when that is quicker or nobody else can. Keep the user informed and keep replies short.`

// Seed creates Chef when there are no bots yet and returns whether it did.
func (s *Store) Seed() (bool, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM bots`).Scan(&n); err != nil || n > 0 {
		return false, err
	}
	if s.Setting("seeded") != "" {
		return false, nil // the user deleted every bot; don't bring Chef back
	}
	if _, err := s.CreateBot(Bot{Name: "Chef", Description: ChefDescription}); err != nil {
		return false, err
	}
	return true, s.SetSetting("seeded", "1")
}
