// Package team runs the bots: a DM's turns, a group's room exchange, and the
// tools bots use on each other (message_bot, create_bot, hand_off). It reads
// and writes the store and reports to a UI through Event.
package team

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/tool"
)

// MaxHops is how many bot-to-bot messages may follow a user message before
// message_bot refuses.
const MaxHops = 8

type EventKind string

const (
	EventRunStart EventKind = "run_start" // a chat started working
	EventRunEnd   EventKind = "run_end"   // and stopped
	EventTurn     EventKind = "turn"      // BotID starts a turn
	EventAgent    EventKind = "agent"     // Agent is set: a streamed event of BotID's turn
	EventEntry    EventKind = "entry"     // Entry was stored
	EventRemoved  EventKind = "removed"   // Entry (Seq) was taken out: a PASS
	EventPass     EventKind = "pass"      // BotID passed its turn
	EventChanged  EventKind = "changed"   // bots or chats changed (create_bot, hand_off)
	EventError    EventKind = "error"     // Err is set
)

type Event struct {
	Kind   EventKind
	ChatID string
	BotID  string
	Turn   string
	Agent  agent.Event
	Entry  *store.Entry
	Err    error
}

// Team runs every chat of a store.
type Team struct {
	Store *store.Store
	// Model resolves the model a bot runs on.
	Model func(store.Bot) (agent.Model, error)
	// Tools are the bot's own tools for a turn (the team's are added).
	Tools func(b store.Bot, workspace string) []tool.Tool
	// Emit receives events from any goroutine; it must not block for long.
	Emit func(Event)
	// Memory gives every bot its memory: MEMORY.md in the system prompt,
	// memory_update, memory_log and recall, a diary line per turn, and the
	// recent-work brief after the transcript.
	Memory bool
	// Compaction, when set, summarizes a chat's older part before a request
	// would outgrow the model's window.
	Compaction *Compaction
	// Retry asks a failed model call again (agent.DefaultRetry is a fair
	// choice); nil tries once.
	Retry *agent.Retry
	// Now is the clock (time.Now when nil).
	Now func() time.Time
	// Approve, when set, runs before each tool call of a turn and may block
	// it with a result (approval of shell commands); it may wait for the user
	// until ctx ends.
	Approve func(ctx context.Context, b store.Bot, chat store.Chat, call agent.Part) *tool.Result
	// Extra, when set, adds tools for a bot's turn in a chat (memory).
	Extra func(b store.Bot, chat store.Chat) []tool.Tool
	// Brief, when set, is a note after the transcript of each turn (the
	// recent-work brief).
	Brief func(b store.Bot, chat store.Chat) string
	// AfterTurn, when set, is told each finished turn (the memory log).
	AfterTurn func(b store.Bot, chat store.Chat, out []agent.Message)

	mu     sync.Mutex
	runs   map[string]*run
	active sync.WaitGroup
}

// run is one chat at work.
type run struct {
	cancel    context.CancelFunc
	steering  *agent.Queue // DM: user messages for the bot's next call
	interrupt *agent.Interrupt
	pending   map[string]store.Meta // DM: queued messages by id, with who sent them
	authors   map[string]string
	restart   bool     // group: a new user message arrived
	mentions  []string // group: its mentions
	hops      int
}

func (t *Team) emit(e Event) {
	if t.Emit != nil {
		t.Emit(e)
	}
}

// Busy reports whether a chat is working.
func (t *Team) Busy(chatID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.runs[chatID] != nil
}

// Stop cancels a chat's work at once.
func (t *Team) Stop(chatID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if r := t.runs[chatID]; r != nil {
		r.cancel()
		r.steering.Clear()
		r.restart = false
	}
}

// Interrupt cuts the step under way in a chat short, so what was just sent
// is read at once; it reports whether a step was under way.
func (t *Team) Interrupt(chatID string) bool {
	t.mu.Lock()
	r := t.runs[chatID]
	t.mu.Unlock()
	return r != nil && r.interrupt.Interrupt()
}

// Wait blocks until no chat is working (tests).
func (t *Team) Wait() { t.active.Wait() }

// Send is a user message to a chat. A DM at work takes it at the bot's next
// model call; a group at work starts over with it after the current turn.
func (t *Team) Send(chatID, text string, mentions []string, replyTo *store.Quote) error {
	return t.SendMessage(chatID, text, nil, mentions, replyTo)
}

// SendMessage is Send with images (PartImage parts) after the text.
func (t *Team) SendMessage(chatID, text string, images []agent.Part, mentions []string, replyTo *store.Quote) error {
	if strings.TrimSpace(text) == "" && len(images) == 0 {
		return errors.New("empty message")
	}
	msg := agent.Message{ID: store.NewID("msg"), Role: agent.RoleUser}
	if strings.TrimSpace(text) != "" {
		msg.Parts = append(msg.Parts, agent.Part{Kind: agent.PartText, Text: text})
	}
	msg.Parts = append(msg.Parts, images...)
	return t.deliver(chatID, store.User, store.Meta{Mentions: mentions, ReplyTo: replyTo}, msg)
}

// deliver hands a message to a chat and starts it working.
func (t *Team) deliver(chatID, author string, meta store.Meta, msg agent.Message) error {
	chat, err := t.Store.Chat(chatID)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.runs == nil {
		t.runs = map[string]*run{}
	}
	r := t.runs[chatID]
	if r != nil && chat.Kind == store.DM {
		r.pending[msg.ID] = meta
		r.authors[msg.ID] = author
		r.steering.Push(msg)
		return nil
	}
	e, err := t.Store.Append(chatID, author, "", meta, msg)
	if err != nil {
		return err
	}
	t.emit(Event{Kind: EventEntry, ChatID: chatID, Entry: &e})
	if r != nil { // a group at work
		r.restart, r.mentions = true, meta.Mentions
		if author != store.User {
			r.hops = meta.Hops
		} else {
			r.hops = 0
		}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	r = &run{cancel: cancel, steering: agent.NewQueue(agent.QueueAll), interrupt: agent.NewInterrupt(), pending: map[string]store.Meta{}, authors: map[string]string{}, mentions: meta.Mentions, hops: meta.Hops}
	t.runs[chatID] = r
	t.emit(Event{Kind: EventRunStart, ChatID: chatID})
	t.active.Add(1)
	go t.work(ctx, chatID, r, replyAuthor(meta))
	return nil
}

func replyAuthor(m store.Meta) string {
	if m.ReplyTo != nil {
		return m.ReplyTo.Author
	}
	return ""
}

func (t *Team) work(ctx context.Context, chatID string, r *run, replyTo string) {
	defer func() {
		r.cancel()
		t.emit(Event{Kind: EventRunEnd, ChatID: chatID})
		t.active.Done()
	}()
	for {
		chat, err := t.Store.Chat(chatID)
		if err != nil {
			t.emit(Event{Kind: EventError, ChatID: chatID, Err: err})
		} else if chat.Kind == store.DM {
			t.turn(ctx, chat, chat.Owner, r, "")
		} else {
			t.room(ctx, chat, r, replyTo)
		}
		// Deciding to stop and leaving go together, so a message delivered
		// meanwhile either is taken here or starts a new run.
		t.mu.Lock()
		again := err == nil && ctx.Err() == nil && (r.steering.Len() > 0 || r.restart)
		if !again {
			delete(t.runs, chatID)
		}
		t.mu.Unlock()
		if !again {
			return
		}
		replyTo = ""
	}
}

// turn runs one bot's turn in a chat and stores what it says. cue, when set,
// is a note after the transcript that is not stored. It returns the stored
// entries of the turn.
func (t *Team) turn(ctx context.Context, chat store.Chat, botID string, r *run, cue string) []store.Entry {
	b, err := t.Store.Bot(botID)
	if err != nil {
		t.emit(Event{Kind: EventError, ChatID: chat.ID, BotID: botID, Err: err})
		return nil
	}
	turnID := store.NewID("turn")
	t.emit(Event{Kind: EventTurn, ChatID: chat.ID, BotID: botID, Turn: turnID})
	fail := func(err error) []store.Entry {
		msg := agent.Message{Role: agent.RoleAssistant, StopReason: agent.StopError, Parts: []agent.Part{{Kind: agent.PartText, Text: err.Error(), IsError: true}}}
		e, serr := t.Store.Append(chat.ID, botID, turnID, store.Meta{}, msg)
		if serr == nil {
			t.emit(Event{Kind: EventEntry, ChatID: chat.ID, BotID: botID, Turn: turnID, Entry: &e})
		}
		return []store.Entry{e}
	}
	model, err := t.Model(b)
	if err != nil {
		return fail(err)
	}
	entries, err := t.Store.Messages(chat.ID, 0)
	if err != nil {
		return fail(err)
	}
	bots, err := t.Store.Bots()
	if err != nil {
		return fail(err)
	}
	msgs := Project(entries, b.ID, bots)
	var notes []string
	if t.Memory {
		if s := t.brief(b, chat); s != "" {
			notes = append(notes, s)
		}
	}
	if cue != "" {
		notes = append(notes, cue)
	}
	if len(notes) > 0 {
		msgs = append(msgs, agent.Message{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: strings.Join(notes, "\n\n")}}})
	}

	ws := t.Store.Workspace(b.ID)
	var tools []tool.Tool
	if t.Tools != nil {
		tools = t.Tools(b, ws)
	}
	tools = append(tools, t.teamTools(b, chat, r)...)
	system := SystemPrompt(b, chat, bots, ws)
	if t.Memory {
		tools = append(tools, t.MemoryTools(b, chat)...)
		system += "\n\n" + t.memoryOf(b).Prompt()
	}
	var compact func(context.Context, []agent.Message, bool) ([]agent.Message, bool)
	if t.Compaction != nil {
		compact = t.compactor(b, chat, model, modelID(model, b), system, tools)
	}

	var stored []store.Entry
	var storeErr error
	out := agent.Run(ctx, model, msgs, agent.Options{
		System:    system,
		Tools:     tools,
		Compact:   compact,
		Hooks:     t.hooks(ctx, b, chat),
		Steering:  r.steering,
		Interrupt: r.interrupt,
		Emit: func(e agent.Event) {
			t.emit(Event{Kind: EventAgent, ChatID: chat.ID, BotID: botID, Turn: turnID, Agent: e})
			if e.Kind != agent.EventMessageEnd || storeErr != nil {
				return
			}
			author, meta, turn := botID, store.Meta{}, turnID
			if e.Message.Role == agent.RoleUser {
				t.mu.Lock()
				meta, author = r.pending[e.Message.ID], r.authors[e.Message.ID]
				delete(r.pending, e.Message.ID)
				delete(r.authors, e.Message.ID)
				if author == "" {
					author = store.User
				}
				if author == store.User {
					r.hops = 0
				} else {
					r.hops = meta.Hops
				}
				t.mu.Unlock()
				turn = ""
			}
			en, err := t.Store.Append(chat.ID, author, turn, meta, *e.Message)
			if err != nil {
				storeErr = err
				return
			}
			stored = append(stored, en)
			t.emit(Event{Kind: EventEntry, ChatID: chat.ID, BotID: botID, Turn: turnID, Entry: &en})
		},
	})
	if storeErr != nil {
		t.emit(Event{Kind: EventError, ChatID: chat.ID, BotID: botID, Err: storeErr})
	}
	if t.Memory {
		t.logTurn(b, chat, out)
	}
	return stored
}

func (t *Team) hooks(ctx context.Context, b store.Bot, chat store.Chat) agent.Hooks {
	if t.Approve == nil {
		return agent.Hooks{}
	}
	return agent.Hooks{BeforeToolCall: func(call agent.Part) *tool.Result { return t.Approve(ctx, b, chat, call) }}
}

// modelID is the model a bot's turn runs on, for its context window.
func modelID(m agent.Model, b store.Bot) string {
	if pm, ok := m.(interface{ ModelID() string }); ok {
		return pm.ModelID()
	}
	return b.Model
}
