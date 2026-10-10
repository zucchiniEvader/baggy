package agent

import (
	"sync"
	"time"

	"github.com/spike/gobot/tool"
)

// updateEvery is the most often a tool's partial results become events.
const updateEvery = 150 * time.Millisecond

// updates passes a tool's partial results on at most every updateEvery, the
// newest one last, and none after the tool ends.
type updates struct {
	mu      sync.Mutex
	emit    func(tool.Result)
	last    time.Time
	pending *tool.Result
	timer   *time.Timer
	done    bool
}

func newUpdates(emit func(tool.Result)) *updates { return &updates{emit: emit} }

func (u *updates) send(r tool.Result) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.done {
		return
	}
	if wait := updateEvery - time.Since(u.last); wait > 0 {
		u.pending = &r
		if u.timer == nil {
			u.timer = time.AfterFunc(wait, u.flush)
		}
		return
	}
	u.last = time.Now()
	u.emit(r)
}

func (u *updates) flush() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.timer = nil
	if u.done || u.pending == nil {
		return
	}
	r := *u.pending
	u.pending, u.last = nil, time.Now()
	u.emit(r)
}

func (u *updates) stop() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.done = true
	if u.timer != nil {
		u.timer.Stop()
	}
}
