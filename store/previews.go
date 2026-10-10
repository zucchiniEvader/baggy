package store

import (
	"encoding/json"
	"strings"

	"github.com/spike/gobot/agent"
)

// SetPinned pins a chat to the top of the list, or unpins it.
func (s *Store) SetPinned(chatID string, pinned bool) error {
	at := int64(0)
	if pinned {
		at = now()
	}
	res, err := s.db.Exec(`UPDATE chats SET pinned = ? WHERE id = ?`, at, chatID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Preview is the last thing said in a chat, as a list shows it.
type Preview struct {
	Author string // User or a bot id
	Text   string // one line, at most 160 characters
	At     int64
}

// Previews are the chats' last words by chat id: the newest user message
// or reply with text or files, past tool results, failed turns, notes for
// the bots and summaries.
func (s *Store) Previews() (map[string]Preview, error) {
	rows, err := s.db.Query(`SELECT chat_id, author, meta, body, created FROM (
		SELECT chat_id, author, meta, body, created, ROW_NUMBER() OVER (PARTITION BY chat_id ORDER BY seq DESC) AS n
		FROM messages WHERE author != ? AND json_extract(body, '$.role') IN ('user', 'assistant'))
		WHERE n <= 12 ORDER BY chat_id, n`, System)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Preview{}
	for rows.Next() {
		var chatID, author, meta, body string
		var at int64
		if err := rows.Scan(&chatID, &author, &meta, &body, &at); err != nil {
			return nil, err
		}
		if _, ok := out[chatID]; ok {
			continue
		}
		var m agent.Message
		var mt Meta
		if json.Unmarshal([]byte(body), &m) != nil {
			continue
		}
		json.Unmarshal([]byte(meta), &mt)
		if text := previewText(m, mt); text != "" {
			out[chatID] = Preview{Author: author, Text: text, At: at}
		}
	}
	return out, rows.Err()
}

func previewText(m agent.Message, meta Meta) string {
	if m.Kind == "compaction" || m.StopReason == agent.StopError || m.StopReason == agent.StopAborted && !hasText(m) {
		return ""
	}
	if meta.RoutineName != "" {
		return meta.RoutineName
	}
	var b strings.Builder
	for _, p := range m.Parts {
		if p.Kind == agent.PartText && !p.IsError {
			b.WriteString(p.Text + " ")
		}
	}
	text := strings.Join(strings.Fields(b.String()), " ")
	if text == "" {
		var names []string
		for _, f := range meta.Files {
			names = append(names, f.Name)
		}
		text = strings.Join(names, ", ")
	}
	if r := []rune(text); len(r) > 160 {
		text = string(r[:159]) + "…"
	}
	return text
}

func hasText(m agent.Message) bool {
	for _, p := range m.Parts {
		if p.Kind == agent.PartText && strings.TrimSpace(p.Text) != "" && !p.IsError {
			return true
		}
	}
	return false
}
