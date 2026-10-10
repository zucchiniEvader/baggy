package tool

import (
	"context"
	"sync"
	"time"
)

type updateKey struct{}

// WithUpdate gives ctx a function that takes a running tool's partial
// results.
func WithUpdate(ctx context.Context, fn func(Result)) context.Context {
	return context.WithValue(ctx, updateKey{}, fn)
}

// Update reports what a running tool has so far, as a whole partial result
// that replaces the one before. It is for display: the model reads only the
// final result. Without WithUpdate it does nothing.
func Update(ctx context.Context, r Result) {
	if fn, ok := ctx.Value(updateKey{}).(func(Result)); ok {
		fn(r)
	}
}

// liveBuffer is a command's output as it comes, reported with Update at
// most every interval while the command runs.
type liveBuffer struct {
	mu      sync.Mutex
	buf     []byte
	changed bool
}

func (b *liveBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	b.buf = append(b.buf, p...)
	b.changed = true
	b.mu.Unlock()
	return len(p), nil
}

func (b *liveBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

// report sends the output's tail to Update every interval until stop is
// closed.
func (b *liveBuffer) report(ctx context.Context, interval time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			b.mu.Lock()
			changed := b.changed
			b.changed = false
			text := string(b.buf)
			b.mu.Unlock()
			if changed {
				tail, _ := truncateTail(text)
				Update(ctx, Result{Text: tail})
			}
		}
	}
}
