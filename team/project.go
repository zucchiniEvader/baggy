package team

import (
	"fmt"
	"strings"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/store"
)

func botNames(bots []store.Bot) map[string]store.Bot {
	m := map[string]store.Bot{}
	for _, b := range bots {
		m[b.ID] = b
	}
	return m
}

func nameOf(byID map[string]store.Bot, id string) string {
	if b, ok := byID[id]; ok {
		return b.Name
	}
	return "a deleted bot"
}

// Project is a chat as one bot sees it: from the newest compaction summary
// on, its own messages as they are, the user's as user messages (with the
// quote they reply to and their mentions named by id), and other bots' words
// as user messages naming the speaker. Other bots' tool calls and results and
// failed turns are left out.
func Project(entries []store.Entry, botID string, bots []store.Bot) []agent.Message {
	byID := botNames(bots)
	var out []agent.Message
	es := compacted(entries)
	for i, e := range es {
		m := e.Msg
		switch {
		case m.Kind == KindNotice:
		case e.Meta.Routine != "":
			answered := false // the bot spoke after it: the run is over
			for _, later := range es[i+1:] {
				answered = answered || later.Author == botID
			}
			out = append(out, withID(textMessage(routineView(e.Meta.RoutineName, textOf(m), e.Meta.Found, !answered)), e))
		case m.Kind == KindCompaction:
			out = append(out, m)
		case e.Author == botID:
			out = append(out, m)
		case e.Author == store.User && m.Role == agent.RoleUser:
			out = append(out, userView(m, e.Meta, botID, byID))
		case m.Role == agent.RoleUser && e.Meta.From != "": // message_bot into this chat
			text := fmt.Sprintf("[Message from %s (id %s) via message_bot]\n%s", nameOf(byID, e.Meta.From), e.Meta.From, textOf(m))
			out = append(out, withID(textMessage(text), e))
		case m.Role == agent.RoleAssistant && m.StopReason != agent.StopError && m.StopReason != agent.StopAborted:
			if t := strings.TrimSpace(textOf(m)); t != "" {
				out = append(out, withID(textMessage(fmt.Sprintf("[%s]: %s", nameOf(byID, e.Author), t)), e))
			}
		}
	}
	return out
}

// compacted is what follows the newest compaction summary: the summary,
// then the messages from the one it kept (Meta.KeepID) on, older summaries
// left out.
func compacted(entries []store.Entry) []store.Entry {
	ci := -1
	for i, e := range entries {
		if e.Msg.Kind == KindCompaction {
			ci = i
		}
	}
	if ci < 0 {
		return entries
	}
	c := entries[ci]
	from := ci + 1
	for i, e := range entries[:ci] {
		if c.Meta.KeepID != "" && ProjectedID(e) == c.Meta.KeepID {
			from = i
			break
		}
	}
	out := []store.Entry{c}
	for i, e := range entries[from:] {
		if from+i != ci && e.Msg.Kind != KindCompaction {
			out = append(out, e)
		}
	}
	return out
}

// ProjectedID is the id of the message an entry projects to: its own, or
// one made of its sequence for a message made up for another bot's words.
func ProjectedID(e store.Entry) string {
	if e.Msg.ID != "" && (e.Msg.Role == agent.RoleUser || e.Msg.Kind == KindCompaction) {
		return e.Msg.ID
	}
	return fmt.Sprintf("seq-%d", e.Seq)
}

func withID(m agent.Message, e store.Entry) agent.Message {
	m.ID = ProjectedID(e)
	return m
}

func textMessage(s string) agent.Message {
	return agent.Message{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: s}}}
}

func textOf(m agent.Message) string {
	var b strings.Builder
	for _, p := range m.Parts {
		if p.Kind == agent.PartText && !p.IsError {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func userView(m agent.Message, meta store.Meta, botID string, byID map[string]store.Bot) agent.Message {
	m.Parts = append([]agent.Part(nil), m.Parts...)
	for i, p := range m.Parts {
		if p.Kind != agent.PartText {
			continue
		}
		text := p.Text
		for _, id := range meta.Mentions {
			if b, ok := byID[id]; ok {
				at := "@" + b.Name
				if j := strings.Index(text, at); j >= 0 {
					text = text[:j+len(at)] + " (id " + id + ")" + text[j+len(at):]
				}
			}
		}
		if q := meta.ReplyTo; q != nil && i == 0 {
			whose := nameOf(byID, q.Author) + "'s message"
			switch q.Author {
			case botID:
				whose = "your message"
			case store.User:
				whose = "their earlier message"
			}
			text = fmt.Sprintf("[Replying to %s: %q]\n%s", whose, q.Text, text)
		}
		m.Parts[i].Text = text
	}
	return m
}

// QuoteOf is the quote a reply to e keeps: its words on one line, at most 280
// characters.
func QuoteOf(e store.Entry) *store.Quote {
	text := strings.Join(strings.Fields(textOf(e.Msg)), " ")
	if r := []rune(text); len(r) > 280 {
		text = string(r[:279]) + "…"
	}
	return &store.Quote{Seq: e.Seq, Author: e.Author, Text: text}
}

// SystemPrompt is a bot's system prompt in a chat: its description, who and
// where it is, and its teammates. It changes only when those do, so the
// provider's prompt cache holds.
func SystemPrompt(b store.Bot, chat store.Chat, bots []store.Bot, workspace string) string {
	byID := botNames(bots)
	var s strings.Builder
	if d := strings.TrimSpace(b.Description); d != "" {
		s.WriteString(d + "\n\n")
	}
	fmt.Fprintf(&s, "You are %s (id %s), one of the user's bots in gobot, a desktop assistant. Your workspace folder is %s; your tools run there.\n", b.Name, b.ID, workspace)
	if chat.Kind == store.DM {
		s.WriteString("\nThis is your direct chat with the user.\n")
	} else {
		fmt.Fprintf(&s, "\nThis is the group %q. Its members, in turn order:\n", chat.Title)
		for _, id := range chat.Members {
			line := "- " + nameOf(byID, id) + " (id " + id + ")"
			if id == b.ID {
				line += " — you"
			}
			if id == chat.Owner {
				line += " — owner"
			}
			s.WriteString(line + "\n")
		}
		if d := strings.TrimSpace(chat.Description); d != "" {
			s.WriteString("The group is for: " + d + "\n")
		}
		s.WriteString("The owner holds the work: it answers for the outcome and may hand it to another member with hand_off. " +
			"Other members' words reach you as user messages that start with their name in brackets. " +
			"When you are offered a turn, reply to the group, or answer PASS alone when you have nothing new to add. " +
			"Talk to members here, not with message_bot.\n")
	}
	var others []string
	for _, o := range bots {
		if o.ID != b.ID {
			line := "- " + o.Name + " (id " + o.ID + ")"
			if d := firstLine(o.Description); d != "" {
				line += ": " + d
			}
			others = append(others, line)
		}
	}
	if len(others) > 0 {
		s.WriteString("\nThe user's other bots, reachable with message_bot (it lands in their direct chat with the user):\n" + strings.Join(others, "\n") + "\n")
	}
	s.WriteString("\nTo add a teammate, use create_bot, and only when the user agrees.")
	return s.String()
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > 120 {
		s = string(r[:119]) + "…"
	}
	return s
}

// KindCompaction marks a stored summary of what came before it.
const KindCompaction = "compaction"
