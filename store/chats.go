package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/spike/gobot/agent"
)

// Chat kinds.
const (
	DM    = "dm"    // one bot, for good; one per bot
	Group = "group" // one to MaxMembers bots
)

// MaxMembers is the most bots a group holds.
const MaxMembers = 6

// User is the author of the user's messages.
const User = "user"

// System is the author of routine runs and notices.
const System = "system"

type Chat struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`       // groups; a DM shows its bot's name
	Description string   `json:"description"` // groups: what it is for
	Owner       string   `json:"owner"`       // the bot holding the work
	Members     []string `json:"members"`     // bot ids in turn order
	Created     int64    `json:"created"`
	Updated     int64    `json:"updated"`  // last message
	ReadSeq     int64    `json:"read_seq"` // the newest message the user has seen
}

func (s *Store) Chat(id string) (Chat, error) {
	cs, err := s.queryChats(`WHERE id = ?`, id)
	if err != nil {
		return Chat{}, err
	}
	if len(cs) == 0 {
		return Chat{}, ErrNotFound
	}
	return cs[0], nil
}

// Chats lists every chat, most recently active first.
func (s *Store) Chats() ([]Chat, error) { return s.queryChats(`ORDER BY updated DESC, rowid DESC`) }

// DMOf is the bot's DM.
func (s *Store) DMOf(botID string) (Chat, error) {
	cs, err := s.queryChats(`WHERE kind = 'dm' AND owner = ?`, botID)
	if err != nil {
		return Chat{}, err
	}
	if len(cs) == 0 {
		return Chat{}, ErrNotFound
	}
	return cs[0], nil
}

// chatsOf lists the chats a bot is a member of.
func (s *Store) chatsOf(botID string) ([]Chat, error) {
	return s.queryChats(`WHERE id IN (SELECT chat_id FROM members WHERE bot_id = ?)`, botID)
}

// ChatsOf lists the chats a bot is a member of, most recently active first.
func (s *Store) ChatsOf(botID string) ([]Chat, error) {
	return s.queryChats(`WHERE id IN (SELECT chat_id FROM members WHERE bot_id = ?) ORDER BY updated DESC`, botID)
}

func (s *Store) queryChats(where string, args ...any) ([]Chat, error) {
	rows, err := s.db.Query(`SELECT id, kind, title, description, owner, created, updated, read_seq FROM chats `+where, args...)
	if err != nil {
		return nil, err
	}
	var out []Chat
	for rows.Next() {
		var c Chat
		if err := rows.Scan(&c.ID, &c.Kind, &c.Title, &c.Description, &c.Owner, &c.Created, &c.Updated, &c.ReadSeq); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		m, err := s.members(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Members = m
	}
	return out, nil
}

func (s *Store) members(chatID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT bot_id FROM members WHERE chat_id = ? ORDER BY pos`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// CreateGroup makes a group of the bots, owned by the first.
func (s *Store) CreateGroup(title, description string, members []string) (Chat, error) {
	members = dedupe(members)
	if len(members) == 0 || len(members) > MaxMembers {
		return Chat{}, errors.New("a group holds one to six bots")
	}
	if strings.TrimSpace(title) == "" {
		return Chat{}, errors.New("a group needs a name")
	}
	c := Chat{ID: NewID("chat"), Kind: Group, Title: strings.TrimSpace(title), Description: description, Owner: members[0], Members: members, Created: now()}
	c.Updated = c.Created
	tx, err := s.db.Begin()
	if err != nil {
		return Chat{}, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO chats(id, kind, title, description, owner, created, updated) VALUES(?,?,?,?,?,?,?)`,
		c.ID, c.Kind, c.Title, c.Description, c.Owner, c.Created, c.Updated); err != nil {
		return Chat{}, err
	}
	for i, b := range members {
		if _, err := tx.Exec(`INSERT INTO members(chat_id, bot_id, pos) VALUES(?,?,?)`, c.ID, b, i); err != nil {
			return Chat{}, err
		}
	}
	return c, tx.Commit()
}

func dedupe(ids []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func (s *Store) group(id string) (Chat, error) {
	c, err := s.Chat(id)
	if err == nil && c.Kind != Group {
		err = errors.New("a DM keeps its one bot")
	}
	return c, err
}

// AddMember seats a bot last in a group.
func (s *Store) AddMember(chatID, botID string) error {
	c, err := s.group(chatID)
	if err != nil {
		return err
	}
	for _, m := range c.Members {
		if m == botID {
			return nil
		}
	}
	if len(c.Members) >= MaxMembers {
		return errors.New("a group holds at most six bots")
	}
	_, err = s.db.Exec(`INSERT INTO members(chat_id, bot_id, pos) VALUES(?,?,?)`, chatID, botID, len(c.Members))
	return err
}

// RemoveMember takes a bot out of a group. The owner's seat passes to the
// first remaining member; a group left empty is removed.
func (s *Store) RemoveMember(chatID, botID string) error {
	c, err := s.group(chatID)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := removeMember(tx, c, botID); err != nil {
		return err
	}
	return tx.Commit()
}

func removeMember(tx *sql.Tx, c Chat, botID string) error {
	var rest []string
	for _, m := range c.Members {
		if m != botID {
			rest = append(rest, m)
		}
	}
	if len(rest) == 0 {
		_, err := tx.Exec(`DELETE FROM chats WHERE id = ?`, c.ID)
		return err
	}
	if _, err := tx.Exec(`DELETE FROM members WHERE chat_id = ? AND bot_id = ?`, c.ID, botID); err != nil {
		return err
	}
	if c.Owner == botID {
		if _, err := tx.Exec(`UPDATE chats SET owner = ? WHERE id = ?`, rest[0], c.ID); err != nil {
			return err
		}
	}
	return nil
}

// SetOwner hands a group's work to one of its members.
func (s *Store) SetOwner(chatID, botID string) error {
	c, err := s.group(chatID)
	if err != nil {
		return err
	}
	for _, m := range c.Members {
		if m == botID {
			_, err := s.db.Exec(`UPDATE chats SET owner = ? WHERE id = ?`, botID, chatID)
			return err
		}
	}
	return errors.New("the owner must be a member")
}

// SetGroupInfo renames a group and changes its description.
func (s *Store) SetGroupInfo(chatID, title, description string) error {
	if _, err := s.group(chatID); err != nil {
		return err
	}
	if strings.TrimSpace(title) == "" {
		return errors.New("a group needs a name")
	}
	_, err := s.db.Exec(`UPDATE chats SET title = ?, description = ? WHERE id = ?`, strings.TrimSpace(title), description, chatID)
	return err
}

// DeleteChat removes a group and its messages. A DM goes only with its bot.
func (s *Store) DeleteChat(chatID string) error {
	if _, err := s.group(chatID); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM chats WHERE id = ?`, chatID)
	return err
}

// ---- messages ----

// Entry is one stored message of a chat.
type Entry struct {
	ChatID  string        `json:"chat_id"`
	Seq     int64         `json:"seq"`
	Author  string        `json:"author"` // User or a bot id
	Turn    string        `json:"turn"`   // the bot turn it belongs to
	Meta    Meta          `json:"meta"`
	Msg     agent.Message `json:"msg"`
	Created int64         `json:"created"`
}

// Meta is what a message carries besides its transcript form.
type Meta struct {
	Mentions []string `json:"mentions,omitempty"` // bot ids the user picked
	ReplyTo  *Quote   `json:"reply_to,omitempty"`
	// From names the bot that sent this message with message_bot, and Hops
	// counts bot-to-bot hops since a user message.
	From string `json:"from,omitempty"`
	Hops int    `json:"hops,omitempty"`
	// KeepID, on a compaction summary, is the id of the first message kept
	// after it (team.Project).
	KeepID string `json:"keep_id,omitempty"`
	// Routine names the routine whose run this message started.
	Routine     string `json:"routine,omitempty"`
	RoutineName string `json:"routine_name,omitempty"`
	// Found is what the routine's check returned, read as data.
	Found string `json:"found,omitempty"`
}

// Quote is the message a reply answers, kept with the reply.
type Quote struct {
	Seq    int64  `json:"seq"`
	Author string `json:"author"`
	Text   string `json:"text"` // one line, at most 280 characters
}

// Append stores a message at the end of a chat.
func (s *Store) Append(chatID, author, turn string, meta Meta, m agent.Message) (Entry, error) {
	body, err := json.Marshal(m)
	if err != nil {
		return Entry{}, err
	}
	mb, _ := json.Marshal(meta)
	e := Entry{ChatID: chatID, Author: author, Turn: turn, Meta: meta, Msg: m, Created: now()}
	tx, err := s.db.Begin()
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) + 1 FROM messages WHERE chat_id = ?`, chatID).Scan(&e.Seq); err != nil {
		return Entry{}, err
	}
	if _, err := tx.Exec(`INSERT INTO messages(chat_id, seq, author, turn, meta, body, created) VALUES(?,?,?,?,?,?,?)`,
		chatID, e.Seq, author, turn, string(mb), string(body), e.Created); err != nil {
		return Entry{}, err
	}
	if _, err := tx.Exec(`UPDATE chats SET updated = ? WHERE id = ?`, e.Created, chatID); err != nil {
		return Entry{}, err
	}
	return e, tx.Commit()
}

// Messages returns a chat's messages after seq, in order.
func (s *Store) Messages(chatID string, after int64) ([]Entry, error) {
	rows, err := s.db.Query(`SELECT seq, author, turn, meta, body, created FROM messages WHERE chat_id = ? AND seq > ? ORDER BY seq`, chatID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e := Entry{ChatID: chatID}
		var meta, body string
		if err := rows.Scan(&e.Seq, &e.Author, &e.Turn, &meta, &body, &e.Created); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(meta), &e.Meta)
		if err := json.Unmarshal([]byte(body), &e.Msg); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Entry returns one message.
func (s *Store) Entry(chatID string, seq int64) (Entry, error) {
	es, err := s.Messages(chatID, seq-1)
	if err != nil {
		return Entry{}, err
	}
	if len(es) == 0 || es[0].Seq != seq {
		return Entry{}, ErrNotFound
	}
	return es[0], nil
}

// DeleteTurn removes the messages of one bot turn (a turn that ended in PASS).
func (s *Store) DeleteTurn(chatID, turn string) error {
	if turn == "" {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM messages WHERE chat_id = ? AND turn = ?`, chatID, turn)
	return err
}

// Since lists the messages a bot wrote in any chat after the time (ms), newest
// first, at most limit: the recent-work brief reads it.
func (s *Store) Since(botID string, after int64, limit int) ([]Entry, error) {
	rows, err := s.db.Query(`SELECT chat_id, seq, author, turn, meta, body, created FROM messages
		WHERE author = ? AND created > ? ORDER BY created DESC LIMIT ?`, botID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		var meta, body string
		if err := rows.Scan(&e.ChatID, &e.Seq, &e.Author, &e.Turn, &meta, &body, &e.Created); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(meta), &e.Meta)
		json.Unmarshal([]byte(body), &e.Msg)
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteEntry removes one message.
func (s *Store) DeleteEntry(chatID string, seq int64) error {
	_, err := s.db.Exec(`DELETE FROM messages WHERE chat_id = ? AND seq = ?`, chatID, seq)
	return err
}

// MarkRead records that the user has seen a chat up to its newest message.
func (s *Store) MarkRead(chatID string) error {
	_, err := s.db.Exec(`UPDATE chats SET read_seq = (SELECT COALESCE(MAX(seq), 0) FROM messages WHERE chat_id = ?) WHERE id = ?`, chatID, chatID)
	return err
}

// Unread counts the bots' finished replies after the chat's read mark.
func (s *Store) Unread(chatID string) int {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM messages m JOIN chats c ON c.id = m.chat_id
		WHERE m.chat_id = ? AND m.seq > c.read_seq AND m.author != ? AND m.author != ?
		AND json_extract(m.body, '$.role') = 'assistant' AND json_extract(m.body, '$.stop_reason') != 'tool_use'`,
		chatID, User, System).Scan(&n)
	return n
}

// LastSeq is the sequence of a chat's newest message, 0 for none.
func (s *Store) LastSeq(chatID string) int64 {
	var n int64
	s.db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM messages WHERE chat_id = ?`, chatID).Scan(&n)
	return n
}
