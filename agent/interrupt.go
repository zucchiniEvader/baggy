package agent

import (
	"context"
	"sync"
)

// Interrupted is the result of a tool call cut off by an interrupt.
const Interrupted = "The user interrupted this step."

// Interrupt cuts the step under way short (the model's reply and its tool
// calls), so a steering message is read at once. Put one in
// Options.Interrupt; it is safe for concurrent use, and a nil *Interrupt
// does nothing.
type Interrupt struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

func NewInterrupt() *Interrupt { return &Interrupt{} }

// Interrupt cancels the step under way and reports whether there was one.
func (i *Interrupt) Interrupt() bool {
	if i == nil {
		return false
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.cancel == nil {
		return false
	}
	i.cancel()
	i.cancel = nil
	return true
}

// step starts a step under ctx.
func (i *Interrupt) step(ctx context.Context) (context.Context, func()) {
	sctx, cancel := context.WithCancel(ctx)
	if i == nil {
		return sctx, cancel
	}
	i.mu.Lock()
	i.cancel = cancel
	i.mu.Unlock()
	return sctx, func() {
		i.mu.Lock()
		i.cancel = nil
		i.mu.Unlock()
		cancel()
	}
}

// cutShort is a reply interrupted while it streamed, as said: its text
// alone, or nothing.
func cutShort(m Message) (Message, bool) {
	var parts []Part
	for _, p := range m.Parts {
		if p.Kind == PartText && !p.IsError && p.Text != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return Message{}, false
	}
	m.Parts, m.StopReason = parts, StopEnd
	return m, true
}
