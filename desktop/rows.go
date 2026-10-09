package main

import (
	"strings"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/team"
	"github.com/spike/gobot/tool"
)

type rowKind int

const (
	rowUser rowKind = iota
	rowAssistant
	rowTool
	rowNotice
)

// row is one line of a chat as drawn: built from stored entries, and while a
// turn streams, from its events.
type row struct {
	kind     rowKind
	author   string // store.User or a bot id
	from     string // a user row delivered by message_bot: the sending bot
	seq      int64  // the entry it shows; 0 while streaming
	turn     string
	text     string
	thinking string
	failed   bool // a reply that ended in an error, or a tool that failed
	stopped  bool // a reply cut off by Stop
	quote    *store.Quote
	call     agent.Part
	result   *tool.Result
	pending  bool // a tool call waiting for approval
	images   int  // images attached to a user message
}

func messageText(m agent.Message) string {
	var b strings.Builder
	for _, p := range m.Parts {
		if p.Kind == agent.PartText && !p.IsError {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func errorText(m agent.Message) string {
	var b strings.Builder
	for _, p := range m.Parts {
		if p.Kind == agent.PartText && p.IsError {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// entryRows are the rows of one entry. A tool entry has none: it fills the
// results of the tool rows before it (see fillResults).
func entryRows(e store.Entry) []row {
	m := e.Msg
	switch {
	case m.Kind == team.KindCompaction:
		return []row{{kind: rowNotice, seq: e.Seq, text: L("Earlier messages were summarized to save context.")}}
	case m.Role == agent.RoleUser:
		r := row{kind: rowUser, author: e.Author, from: e.Meta.From, seq: e.Seq, text: messageText(m), quote: e.Meta.ReplyTo}
		for _, p := range m.Parts {
			if p.Kind == agent.PartImage {
				r.images++
			}
		}
		return []row{r}
	case m.Role == agent.RoleAssistant:
		var out []row
		r := row{kind: rowAssistant, author: e.Author, seq: e.Seq, turn: e.Turn, text: messageText(m)}
		for _, p := range m.Parts {
			if p.Kind == agent.PartThinking {
				r.thinking += p.Text
			}
		}
		switch m.StopReason {
		case agent.StopError:
			r.failed = true
			if t := errorText(m); t != "" {
				r.text = strings.TrimSpace(r.text + "\n" + t)
			}
		case agent.StopAborted:
			r.stopped = true
		}
		if strings.TrimSpace(r.text) != "" || strings.TrimSpace(r.thinking) != "" || r.failed || r.stopped {
			out = append(out, r)
		}
		for _, p := range m.Parts {
			if p.Kind == agent.PartToolCall {
				out = append(out, row{kind: rowTool, author: e.Author, seq: e.Seq, turn: e.Turn, call: p})
			}
		}
		return out
	}
	return nil
}

// fillResults sets the results of a tool entry on the rows of its calls.
func fillResults(rows []row, m agent.Message) {
	for _, p := range m.Parts {
		if p.Kind != agent.PartToolResult {
			continue
		}
		for i := len(rows) - 1; i >= 0; i-- {
			if rows[i].kind == rowTool && rows[i].call.CallID == p.CallID {
				rows[i].result = &tool.Result{Text: p.Text, IsError: p.IsError}
				rows[i].failed = p.IsError
				rows[i].pending = false
				break
			}
		}
	}
}

// buildRows are the rows of a chat's stored entries.
func buildRows(es []store.Entry) []row {
	var rows []row
	for _, e := range es {
		if e.Msg.Role == agent.RoleTool {
			fillResults(rows, e.Msg)
			continue
		}
		rows = append(rows, entryRows(e)...)
	}
	return rows
}
