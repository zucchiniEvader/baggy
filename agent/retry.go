package agent

import (
	"regexp"
	"time"
)

// Retry says how the loop asks again after a model call failed for a
// transient reason: up to Max times, waiting Base doubling to MaxDelay.
type Retry struct {
	Max      int
	Base     time.Duration
	MaxDelay time.Duration
}

// DefaultRetry is three retries from a second.
var DefaultRetry = &Retry{Max: 3, Base: time.Second, MaxDelay: 60 * time.Second}

// Delay is the wait before retry attempt (1-based).
func (r *Retry) Delay(attempt int) time.Duration {
	d := r.Base << (attempt - 1)
	if d > r.MaxDelay || d <= 0 {
		d = r.MaxDelay
	}
	return d
}

var (
	retryable = regexp.MustCompile(`(?i)overloaded|rate.?limit|too many requests|\b(429|500|502|503|504|529)\b|server error|internal error|unavailable|timed? ?out|timeout|connection (reset|refused|closed)|broken pipe|unexpected EOF|\bEOF\b|stream ended|no stop reason|temporarily`)
	// Exhausted quota and billing are not transient.
	exhausted = regexp.MustCompile(`(?i)insufficient_quota|quota exceeded|usage limit|billing|out of budget|available balance|insufficient balance|credit`)
	overflow  = regexp.MustCompile(`(?i)prompt is too long|context.?length|context window|maximum context|too many tokens|context_length_exceeded|input is too long|exceeds the (model's )?(maximum|context)|reduce the length`)
)

// IsRetryable reports whether an error reads like a transient failure.
func IsRetryable(text string) bool {
	return retryable.MatchString(text) && !exhausted.MatchString(text) && !overflow.MatchString(text)
}

// IsContextOverflow reports whether an error says the prompt no longer fits.
func IsContextOverflow(text string) bool { return overflow.MatchString(text) }

// errorOf is the text of a failed reply's error parts.
func errorOf(m Message) string {
	s := ""
	for _, p := range m.Parts {
		if p.Kind == PartText && p.IsError {
			s += p.Text
		}
	}
	return s
}
