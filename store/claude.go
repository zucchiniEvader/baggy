package store

import (
	"database/sql"
	"errors"
)

// ClaudeSession is the Claude Code session a bot's tasks continue in a chat
// and a folder; "" when there is none.
func (s *Store) ClaudeSession(chat, bot, dir string) string {
	var id string
	err := s.db.QueryRow(`SELECT session FROM claude_sessions WHERE chat_id=? AND bot_id=? AND dir=?`, chat, bot, dir).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	return id
}

// SetClaudeSession keeps the session a bot's next task in a chat and a
// folder continues.
func (s *Store) SetClaudeSession(chat, bot, dir, session string) error {
	_, err := s.db.Exec(`INSERT INTO claude_sessions(chat_id, bot_id, dir, session, updated) VALUES(?,?,?,?,?)
ON CONFLICT(chat_id, bot_id, dir) DO UPDATE SET session=excluded.session, updated=excluded.updated`, chat, bot, dir, session, now())
	return err
}
