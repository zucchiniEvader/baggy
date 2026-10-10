package main

import (
	"strings"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/provider"
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
	at       int64  // when the entry was stored, Unix ms; 0 while streaming
	turn     string
	text     string
	thinking string
	failed   bool // a reply that ended in an error, or a tool that failed
	stopped  bool // a reply cut off by Stop
	quote    *store.Quote
	call     agent.Part
	result   *tool.Result
	pending  bool         // a tool call waiting for approval
	images   int          // images in a user message with no files listed (older ones)
	files    []store.File // files attached to a user message
	searches []search
}

// search is a server tool's work in a reply: a web search or a page read.
type search struct {
	id, tool, detail string
	done             bool
	err              string
	count            int
}

// addServer folds a server block into a reply's searches.
func addServer(ss []search, p agent.Part) []search {
	b, ok := provider.DescribeServer(p)
	if !ok {
		return ss
	}
	if !b.Result {
		for _, s := range ss {
			if s.id == b.ID && b.ID != "" {
				return ss
			}
		}
		s := search{id: b.ID, tool: b.Tool, detail: b.Detail}
		if b.Error != "" { // a Responses item is the call and its outcome
			s.done, s.err = true, b.Error
		} else if b.Tool == provider.WebSearch && p.Name == "web_search_call" {
			s.done = true
		}
		return append(ss, s)
	}
	for i := range ss {
		if ss[i].id == b.ID {
			ss[i].done, ss[i].err, ss[i].count = true, b.Error, b.Count
		}
	}
	return ss
}

// searchText is how a search reads.
func searchText(s search) string {
	switch {
	case s.tool == provider.WebFetch && s.err != "":
		return L("Could not read %s: %s", s.detail, s.err)
	case s.tool == provider.WebFetch && s.done:
		return L("Read %s", s.detail)
	case s.tool == provider.WebFetch:
		return L("Reading %s…", s.detail)
	case s.err != "":
		return L("Web search failed: %s", s.err)
	case s.done && s.detail != "":
		return L("Searched the web for “%s”", s.detail)
	case s.done:
		return L("Searched the web")
	case s.detail != "":
		return L("Searching the web for “%s”…", s.detail)
	}
	return L("Searching the web…")
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
	case m.Kind == team.KindNotice:
		return []row{{kind: rowNotice, seq: e.Seq, text: messageText(m)}}
	case e.Meta.Routine != "":
		return []row{{kind: rowNotice, seq: e.Seq, text: L("Routine · %s", e.Meta.RoutineName)}}
	case e.Author == store.System && m.Role == agent.RoleUser: // a note for the bot
		text := messageText(m)
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[:i]
		}
		return []row{{kind: rowNotice, seq: e.Seq, text: strings.Trim(text, "[]")}}
	case m.Role == agent.RoleUser:
		r := row{kind: rowUser, author: e.Author, from: e.Meta.From, seq: e.Seq, at: e.Created, text: messageText(m), quote: e.Meta.ReplyTo, files: e.Meta.Files}
		for _, p := range m.Parts {
			if p.Kind == agent.PartImage && len(r.files) == 0 {
				r.images++
			}
		}
		return []row{r}
	case m.Role == agent.RoleAssistant:
		var out []row
		r := row{kind: rowAssistant, author: e.Author, seq: e.Seq, at: e.Created, turn: e.Turn, text: messageText(m)}
		for _, p := range m.Parts {
			switch p.Kind {
			case agent.PartThinking:
				r.thinking += p.Text
			case agent.PartServer:
				r.searches = addServer(r.searches, p)
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
		if strings.TrimSpace(r.text) != "" || strings.TrimSpace(r.thinking) != "" || r.failed || r.stopped || len(r.searches) > 0 {
			out = append(out, r)
		}
		for _, p := range m.Parts {
			if p.Kind == agent.PartToolCall {
				out = append(out, row{kind: rowTool, author: e.Author, seq: e.Seq, at: e.Created, turn: e.Turn, call: p})
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
