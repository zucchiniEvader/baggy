package team

import (
	"context"
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
	for round := 1; round <= MaxRounds; round++ {
		spoke := false
		for _, id := range order {
			t.mu.Lock()
			yield := r.restart
			t.mu.Unlock()
			if yield || ctx.Err() != nil {
				return
			}
			entries, err := t.Store.Messages(chat.ID, seen[id])
			if err != nil {
				t.emit(Event{Kind: EventError, ChatID: chat.ID, Err: err})
				return
			}
			fresh := 0
			for _, e := range entries {
				if e.Author != id && (e.Msg.Role == agent.RoleUser || textOf(e.Msg) != "") {
					fresh++
				}
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
			order = TurnOrder(chat.Members, nil, "")
		}
	}
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
