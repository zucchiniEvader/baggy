package store

import (
	"database/sql"
	"errors"
	"strings"
)

// Missed-run policies.
const (
	Coalesce = "coalesce" // one run for every due time missed
	Skip     = "skip"     // a due time more than a minute late is skipped
)

// Run outcomes.
const (
	OutcomeSent  = "sent"
	OutcomePass  = "pass"
	OutcomeError = "error"
)

// Routine is a task a bot runs on a schedule in its DM.
type Routine struct {
	ID       string `json:"id"`
	BotID    string `json:"bot_id"`
	Name     string `json:"name"`
	Prompt   string `json:"prompt"`   // an instruction to the bot itself
	Schedule string `json:"schedule"` // schedule.Parse's spec
	Timezone string `json:"timezone"` // IANA; "" is the computer's
	Missed   string `json:"missed"`   // Coalesce or Skip
	Check    string `json:"check"`    // JavaScript run first; what it returns starts the run

	Enabled      bool   `json:"enabled"`
	PausedReason string `json:"paused_reason"` // "away", or "" (the user)

	LastScheduledAt int64  `json:"last_scheduled_at"` // the due time last taken (ms)
	LastRunAt       int64  `json:"last_run_at"`
	LastOutcome     string `json:"last_outcome"` // OutcomeSent, OutcomePass, OutcomeError
	LastCheckAt     int64  `json:"last_check_at"`
	LastCheck       string `json:"last_check"` // quiet, ready, failed
	Created         int64  `json:"created"`
	EnabledAt       int64  `json:"enabled_at"`
}

const routineCols = `id, bot_id, name, prompt, schedule, timezone, missed, check_code, enabled, paused_reason,
	last_scheduled_at, last_run_at, last_outcome, last_check_at, last_check, created, enabled_at`

func scanRoutine(sc interface{ Scan(...any) error }) (Routine, error) {
	var r Routine
	err := sc.Scan(&r.ID, &r.BotID, &r.Name, &r.Prompt, &r.Schedule, &r.Timezone, &r.Missed, &r.Check, &r.Enabled, &r.PausedReason,
		&r.LastScheduledAt, &r.LastRunAt, &r.LastOutcome, &r.LastCheckAt, &r.LastCheck, &r.Created, &r.EnabledAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return r, err
}

// SaveRoutine inserts or replaces r; an empty ID makes a new one.
func (s *Store) SaveRoutine(r Routine) (Routine, error) {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" || strings.TrimSpace(r.Prompt) == "" || strings.TrimSpace(r.Schedule) == "" {
		return Routine{}, errors.New("a routine needs a name, a prompt and a schedule")
	}
	if r.Missed == "" {
		r.Missed = Coalesce
	}
	if r.Missed != Coalesce && r.Missed != Skip {
		return Routine{}, errors.New("missed_run_policy is coalesce or skip")
	}
	if r.ID == "" {
		r.ID = NewID("rtn")
	}
	if r.Created == 0 {
		r.Created = now()
	}
	if r.EnabledAt == 0 {
		r.EnabledAt = r.Created
	}
	_, err := s.db.Exec(`INSERT INTO routines(`+routineCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, prompt=excluded.prompt, schedule=excluded.schedule,
		timezone=excluded.timezone, missed=excluded.missed, check_code=excluded.check_code, enabled=excluded.enabled,
		paused_reason=excluded.paused_reason, last_scheduled_at=excluded.last_scheduled_at, last_run_at=excluded.last_run_at,
		last_outcome=excluded.last_outcome, last_check_at=excluded.last_check_at, last_check=excluded.last_check,
		enabled_at=excluded.enabled_at`,
		r.ID, r.BotID, r.Name, r.Prompt, r.Schedule, r.Timezone, r.Missed, r.Check, r.Enabled, r.PausedReason,
		r.LastScheduledAt, r.LastRunAt, r.LastOutcome, r.LastCheckAt, r.LastCheck, r.Created, r.EnabledAt)
	return r, err
}

func (s *Store) Routine(id string) (Routine, error) {
	return scanRoutine(s.db.QueryRow(`SELECT `+routineCols+` FROM routines WHERE id = ?`, id))
}

// Routines lists a bot's routines ("" for every bot's), oldest first.
func (s *Store) Routines(botID string) ([]Routine, error) {
	q, args := `SELECT `+routineCols+` FROM routines ORDER BY created, rowid`, []any{}
	if botID != "" {
		q, args = `SELECT `+routineCols+` FROM routines WHERE bot_id = ? ORDER BY created, rowid`, []any{botID}
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Routine
	for rows.Next() {
		r, err := scanRoutine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) DeleteRoutine(id string) error {
	res, err := s.db.Exec(`DELETE FROM routines WHERE id = ?`, id)
	return notFound(res, err)
}

// LastUserMessage is when the user last wrote in any chat (ms), 0 if never.
func (s *Store) LastUserMessage() int64 {
	var t sql.NullInt64
	s.db.QueryRow(`SELECT MAX(created) FROM messages WHERE author = ?`, User).Scan(&t)
	return t.Int64
}
