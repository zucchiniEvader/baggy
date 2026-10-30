package team

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/store"
)

// MaxRounds is how many rounds one room exchange runs at most.
const MaxRounds = 4

// TurnOrder is who is offered a turn first: the members a message mentions,
// in the order mentioned, then the author of the message it replies to, then
// the rest in chat order.
func TurnOrder(members, mentions []string, replyTo string) []string {
	in := map[string]bool{}
	for _, m := range members {
		in[m] = true
	}
	var out []string
	seen := map[string]bool{}
	add := func(id string) {
		if in[id] && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, m := range mentions {
		add(m)
	}
	add(replyTo)
	for _, m := range members {
		add(m)
	}
	return out
}

// IsPass reports whether a reply passes the turn.
func IsPass(m agent.Message) bool {
	t := strings.Trim(strings.TrimSpace(textOf(m)), ".!`*\"' ")
	return strings.EqualFold(t, "PASS")
}

// room runs one exchange in a group: members take turns, a turn replies or
// passes, and a round with a reply is followed by another for the members
// that heard something new, up to MaxRounds. A new user message makes the
// room yield after the current turn (work starts a new one).
func (t *Team) room(ctx context.Context, chat store.Chat, r *run, replyTo string) {
	t.mu.Lock()
	mentions := r.mentions
	r.restart = false
	t.mu.Unlock()

	entries, err := t.Store.Messages(chat.ID, 0)
	if err != nil {
		t.emit(Event{Kind: EventError, ChatID: chat.ID, Err: err})
		return
	}
	seen := map[string]int64{} // the newest seq each member has taken a turn after
	for _, e := range entries {
		seen[e.Author] = e.Seq
	}
	order := TurnOrder(chat.Members, mentions, replyTo)
	// forced: the head of order the message names (mentions, the author it
	// replies to), offered a turn before the rest are routed.
	forced := 0
	for _, id := range order {
		if id != replyTo && !contains(mentions, id) {
			break
		}
		forced++
	}
	for round := 1; round <= MaxRounds; round++ {
		spoke := false
		for i := 0; i < len(order); i++ {
			if t.Routing && i == forced {
				var rest []string
				for _, id := range order[i:] {
					if round == 1 || t.fresh(chat.ID, id, seen[id]) > 0 {
						rest = append(rest, id)
					}
				}
				order = append(order[:i:i], t.Route(ctx, chat, rest)...)
				if i >= len(order) {
					break
				}
			}
			id := order[i]
			t.mu.Lock()
			yield := r.restart
			t.mu.Unlock()
			if yield || ctx.Err() != nil {
				return
			}
			fresh := t.fresh(chat.ID, id, seen[id])
			if fresh < 0 {
				return
			}
			if fresh == 0 && round > 1 {
				continue
			}
			if c, err := t.Store.Chat(chat.ID); err == nil {
				chat = c // a hand_off or new member changes the prompt
			}
			if !member(chat, id) {
				continue
			}
			stored := t.turn(ctx, chat, id, r, cue(round, fresh))
			if latest, err := t.Store.Messages(chat.ID, 0); err == nil && len(latest) > 0 {
				seen[id] = latest[len(latest)-1].Seq
			}
			if n := len(stored); n > 0 && IsPass(stored[n-1].Msg) {
				t.pass(chat.ID, id, stored)
				continue
			}
			if len(stored) > 0 {
				spoke = true
			}
		}
		if !spoke {
			return
		}
		if c, err := t.Store.Chat(chat.ID); err == nil {
			chat = c
			order, forced = TurnOrder(chat.Members, nil, ""), 0
		}
	}
}

// fresh is how many messages a member has not had a turn after: from the
// user, or words from the others; -1 when the chat cannot be read.
func (t *Team) fresh(chatID, id string, since int64) int {
	entries, err := t.Store.Messages(chatID, since)
	if err != nil {
		t.emit(Event{Kind: EventError, ChatID: chatID, Err: err})
		return -1
	}
	n := 0
	for _, e := range entries {
		if e.Author != id && (e.Msg.Role == agent.RoleUser || textOf(e.Msg) != "") {
			n++
		}
	}
	return n
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

const routeSystem = `You pick who in a group chat speaks next. The members are bots, each with a role. Answer with JSON only: {"bots": ["<id>", ...]}, in the order they should speak.

Pick a member when the newest messages ask it something, call for its role, or need something only it can add. Leave out a member with nothing new to add: a reply that repeats others, agrees, or thanks is noise. Pick none when the conversation is settled or waits for the user. One member is usually enough; several only when the message needs several roles.`

// routeLines is how many of the newest messages Route reads.
const routeLines = 16

// Route picks which of the candidates a group's newest messages call for,
// in one short call to the owner's model with no tools; on any failure it
// offers them all, as without routing.
func (t *Team) Route(ctx context.Context, chat store.Chat, candidates []string) []string {
	if len(candidates) <= 1 {
		return candidates
	}
	ownerID := chat.Owner
	if ownerID == "" && len(chat.Members) > 0 {
		ownerID = chat.Members[0]
	}
	owner, err := t.Store.Bot(ownerID)
	if err != nil {
		return candidates
	}
	model, err := t.Model(owner)
	if err != nil {
		return candidates
	}
	entries, err := t.Store.Messages(chat.ID, 0)
	if err != nil {
		return candidates
	}
	bots, _ := t.Store.Bots()
	byID := map[string]store.Bot{}
	for _, b := range bots {
		byID[b.ID] = b
	}
	var p strings.Builder
	p.WriteString("Members who may speak:\n")
	for _, id := range candidates {
		b := byID[id]
		fmt.Fprintf(&p, "- %s (id %s): %s\n", b.Name, b.ID, oneLine(firstLine(b.Description), 200))
	}
	if d := strings.TrimSpace(chat.Description); d != "" {
		fmt.Fprintf(&p, "\nThe group is for: %s\n", oneLine(d, 300))
	}
	var lines []string
	for i := len(entries) - 1; i >= 0 && len(lines) < routeLines; i-- {
		e := entries[i]
		text := strings.TrimSpace(textOf(e.Msg))
		if text == "" || e.Msg.Role == agent.RoleTool || e.Msg.StopReason == agent.StopError {
			continue
		}
		who := "User"
		switch {
		case e.Author == store.System:
			who = "Note"
		case e.Author != store.User:
			who = byID[e.Author].Name
		}
		lines = append(lines, fmt.Sprintf("[%s]: %s", who, oneLine(text, 600)))
	}
	p.WriteString("\nThe conversation, newest last:\n")
	for i := len(lines) - 1; i >= 0; i-- {
		p.WriteString(lines[i] + "\n")
	}
	reply := model.Call(ctx, routeSystem, []agent.Message{textMessage(p.String())}, nil, func(agent.Event) {})
	text := strings.TrimSpace(textOf(reply))
	if i, j := strings.IndexByte(text, '{'), strings.LastIndexByte(text, '}'); i >= 0 && j > i {
		text = text[i : j+1]
	}
	var v struct{ Bots []string }
	if reply.StopReason == agent.StopError || json.Unmarshal([]byte(text), &v) != nil {
		return candidates
	}
	var out []string
	for _, ref := range v.Bots {
		ref = strings.TrimPrefix(strings.TrimSpace(ref), "@")
		for _, id := range candidates {
			if (id == ref || strings.EqualFold(byID[id].Name, ref)) && !contains(out, id) {
				out = append(out, id)
			}
		}
	}
	return out
}

func member(c store.Chat, id string) bool {
	for _, m := range c.Members {
		if m == id {
			return true
		}
	}
	return false
}

// pass takes a PASS out of the chat: the whole turn when it did nothing
// else, only the final reply when it ran tools first.
func (t *Team) pass(chatID, botID string, stored []store.Entry) {
	drop := stored[len(stored)-1:]
	if len(stored) == 1 {
		drop = stored
	}
	for _, e := range drop {
		if err := t.Store.DeleteEntry(chatID, e.Seq); err == nil {
			t.emit(Event{Kind: EventRemoved, ChatID: chatID, BotID: botID, Entry: &e})
		}
	}
	t.emit(Event{Kind: EventPass, ChatID: chatID, BotID: botID})
}

func cue(round, fresh int) string {
	s := fmt.Sprintf("[Group turn: round %d of at most %d; %d new message(s) since your last turn. Reply to the group, or answer PASS alone if you have nothing new to add.", round, MaxRounds, fresh)
	if round == MaxRounds {
		s += " This is the last round, winding down: add only what is essential."
	}
	return s + "]"
}
