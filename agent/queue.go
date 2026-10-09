package agent

import "sync"

// QueueMode decides how much one Drain takes.
type QueueMode int

const (
	QueueOneAtATime QueueMode = iota // the oldest message; the rest wait for the next poll
	QueueAll                         // everything queued
)

// Queue holds messages that arrive while a run is in flight. It is safe for
// concurrent use, and a nil *Queue is empty. Messages outlive runs: one queued
// while no run is active is taken by the next run.
type Queue struct {
	mu   sync.Mutex
	mode QueueMode
	msgs []Message
}

func NewQueue(mode QueueMode) *Queue { return &Queue{mode: mode} }

func (q *Queue) Push(msgs ...Message) {
	q.mu.Lock()
	q.msgs = append(q.msgs, msgs...)
	q.mu.Unlock()
}

// Drain removes and returns what the mode allows.
func (q *Queue) Drain() []Message {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.msgs) == 0 {
		return nil
	}
	n := len(q.msgs)
	if q.mode == QueueOneAtATime {
		n = 1
	}
	out := append([]Message(nil), q.msgs[:n]...)
	q.msgs = q.msgs[n:]
	return out
}

func (q *Queue) Len() int {
	if q == nil {
		return 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.msgs)
}

func (q *Queue) Clear() {
	q.mu.Lock()
	q.msgs = nil
	q.mu.Unlock()
}
