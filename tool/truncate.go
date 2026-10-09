package tool

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	maxLines = 2000
	maxBytes = 50 * 1024
)

// truncateTail keeps the end of s (command output: the error is last).
func truncateTail(s string) (string, bool) {
	cut := false
	if lines := strings.Split(s, "\n"); len(lines) > maxLines {
		s, cut = strings.Join(lines[len(lines)-maxLines:], "\n"), true
	}
	if len(s) > maxBytes {
		s, cut = s[len(s)-maxBytes:], true
		for !utf8.ValidString(s[:1]) && len(s) > 1 { // do not start inside a rune
			s = s[1:]
		}
	}
	return s, cut
}

// truncateHead keeps the start of s (file content: read on with an offset).
// It returns the text and the number of lines kept.
func truncateHead(s string) (string, int, bool) {
	cut := false
	lines := strings.Split(s, "\n")
	if len(lines) > maxLines {
		lines, cut = lines[:maxLines], true
	}
	var b strings.Builder
	n := 0
	for i, l := range lines {
		add := len(l)
		if i > 0 {
			add++
		}
		if b.Len()+add > maxBytes {
			cut = true
			break
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(l)
		n++
	}
	return b.String(), n, cut
}

func note(format string, a ...any) string { return fmt.Sprintf("\n[%s]", fmt.Sprintf(format, a...)) }
