package team

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/memory"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/tool"
)

const (
	briefWindow = 48 * time.Hour
	briefLines  = 10
	briefChars  = 1400 // about 350 tokens
	recallHits  = 20
)

func (t *Team) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

func (t *Team) memoryOf(b store.Bot) *memory.Memory {
	m := memory.New(t.Store.Workspace(b.ID))
	m.Now = t.now
	return m
}

// where names a chat as the bot's memory and brief name it.
func (t *Team) where(chat store.Chat) string {
	if chat.Kind == store.DM {
		return "your chat with the user"
	}
	return fmt.Sprintf("group %q", chat.Title)
}

// MemoryTools are memory_update, memory_log and recall for a bot in a chat.
func (t *Team) MemoryTools(b store.Bot, chat store.Chat) []tool.Tool {
	mem := t.memoryOf(b)
	return []tool.Tool{
		Func{N: "memory_update", D: "Change your long-term memory (MEMORY.md, which opens every turn) by one dated entry. append adds a durable fact (preferences, decisions, people, how things are done); replace rewrites one passage (old) with text; supersede strikes an outdated entry (old) through and adds text; remove deletes a passage (old). Keep entries short and one fact each.",
			S: `{"type":"object","properties":{"op":{"type":"string","enum":["append","replace","supersede","remove"]},"text":{"type":"string"},"old":{"type":"string","description":"the exact passage replace, supersede and remove act on"}},"required":["op"],"additionalProperties":false}`,
			F: func(ctx context.Context, raw json.RawMessage) tool.Result {
				var a struct{ Op, Text, Old string }
				json.Unmarshal(raw, &a)
				entry, err := mem.Update(a.Op, a.Text, a.Old, t.where(chat))
				if err != nil {
					return errResult("%v", err)
				}
				if a.Op == memory.Append || a.Op == memory.Supersede {
					return tool.Result{Text: "Saved: " + entry}
				}
				return tool.Result{Text: "MEMORY.md updated."}
			}},
		Func{N: "memory_log", D: "Add an event to your diary (memory/log/<today>.md): what happened, not what is true. The diary never loads into a prompt; recall finds it.",
			S: `{"type":"object","properties":{"text":{"type":"string"}},"required":["text"],"additionalProperties":false}`,
			F: func(ctx context.Context, raw json.RawMessage) tool.Result {
				var a struct{ Text string }
				json.Unmarshal(raw, &a)
				if strings.TrimSpace(a.Text) == "" {
					return errResult("nothing to log")
				}
				if err := mem.Log(t.where(chat) + " · " + a.Text); err != nil {
					return errResult("%v", err)
				}
				return tool.Result{Text: "Logged."}
			}},
		Func{N: "recall", D: "Search your memory files, your diary, and every chat you are in for messages holding all the words of query, newest first. days limits it to the last days.",
			S: `{"type":"object","properties":{"query":{"type":"string"},"days":{"type":"integer","minimum":1}},"required":["query"],"additionalProperties":false}`,
			F: func(ctx context.Context, raw json.RawMessage) tool.Result {
				var a struct {
					Query string
					Days  int
				}
				json.Unmarshal(raw, &a)
				return tool.Result{Text: t.recall(b, a.Query, a.Days)}
			}},
	}
}

func (t *Team) recall(b store.Bot, query string, days int) string {
	var since time.Time
	if days > 0 {
		since = t.now().Add(-time.Duration(days) * 24 * time.Hour)
	}
	words := memory.Words(query)
	if len(words) == 0 {
		return "Give some words to look for."
	}
	hits := t.memoryOf(b).Search(query, since)
	bots, _ := t.Store.Bots()
	byID := botNames(bots)
	chats, _ := t.Store.ChatsOf(b.ID)
	for _, c := range chats {
		es, err := t.Store.Messages(c.ID, 0)
		if err != nil {
			continue
		}
		for _, e := range es {
			when := time.UnixMilli(e.Created)
			if (!since.IsZero() && when.Before(since)) || e.Msg.Role == agent.RoleTool {
				continue
			}
			text := textOf(e.Msg)
			if !memory.Matches(text, words) {
				continue
			}
			who := "the user"
			switch {
			case e.Author == b.ID:
				who = "you"
			case e.Author != store.User:
				who = nameOf(byID, e.Author)
			}
			hits = append(hits, memory.Hit{Where: t.where(c), Who: who, When: when, Text: oneLine(text, 300)})
		}
	}
	if len(hits) == 0 {
		return "Nothing found for " + fmt.Sprintf("%q", query) + "."
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].When.After(hits[j].When) })
	if len(hits) > recallHits {
		hits = hits[:recallHits]
	}
	var s strings.Builder
	for _, h := range hits {
		s.WriteString("- ")
		if !h.When.IsZero() {
			s.WriteString(h.When.Format("2006-01-02 15:04") + " · ")
		}
		s.WriteString(h.Where)
		if h.Who != "" {
			s.WriteString(" · " + h.Who)
		}
		s.WriteString(": " + h.Text + "\n")
	}
	return s.String()
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// logTurn adds the turn's diary line: where, what was said, which tools ran.
func (t *Team) logTurn(b store.Bot, chat store.Chat, out []agent.Message) {
	var said string
	var used []string
	seen := map[string]bool{}
	for _, m := range out {
		if m.Role != agent.RoleAssistant {
			continue
		}
		if s := strings.TrimSpace(textOf(m)); s != "" {
			said = s
		}
		for _, p := range m.Parts {
			if p.Kind == agent.PartToolCall && !seen[p.Name] {
				seen[p.Name] = true
				used = append(used, p.Name)
			}
		}
	}
	if said == "" && len(used) == 0 {
		return
	}
	if IsPass(agent.Message{Parts: []agent.Part{{Kind: agent.PartText, Text: said}}}) && len(used) == 0 {
		return
	}
	line := "in " + t.where(chat)
	if said != "" {
		line += fmt.Sprintf(" · said %q", oneLine(said, 100))
	}
	if len(used) > 0 {
		line += " · used " + strings.Join(used, ", ")
	}
	t.memoryOf(b).Log(line)
}

// brief is the recent-work brief: the newest thing the bot said in each of
// its other chats over the last two days, newest first.
func (t *Team) brief(b store.Bot, chat store.Chat) string {
	now := t.now()
	es, err := t.Store.Since(b.ID, now.Add(-briefWindow).UnixMilli(), 200)
	if err != nil {
		return ""
	}
	chats := map[string]store.Chat{}
	if cs, err := t.Store.ChatsOf(b.ID); err == nil {
		for _, c := range cs {
			chats[c.ID] = c
		}
	}
	seen := map[string]bool{chat.ID: true}
	var lines []string
	size := 0
	for _, e := range es {
		c, ok := chats[e.ChatID]
		text := strings.TrimSpace(textOf(e.Msg))
		if seen[e.ChatID] || !ok || e.Msg.Role != agent.RoleAssistant || text == "" {
			continue
		}
		seen[e.ChatID] = true
		when := time.UnixMilli(e.Created)
		day := when.Format("Jan 2")
		switch y, m, d := now.Date(); {
		case when.Year() == y && when.Month() == m && when.Day() == d:
			day = "today"
		case now.Add(-24*time.Hour).Format("2006-01-02") == when.Format("2006-01-02"):
			day = "yesterday"
		}
		where := t.where(c)
		if c.Kind == store.DM {
			where = "your chat with the user"
		}
		line := fmt.Sprintf("- %s %s · %s · you said: %q", day, when.Format("15:04"), where, oneLine(text, 120))
		if size+len(line) > briefChars || len(lines) >= briefLines {
			break
		}
		size += len(line)
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	return "[Your recent work in other chats, for context:\n" + strings.Join(lines, "\n") + "]"
}
