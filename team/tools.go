package team

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/store"
	"github.com/zucchiniEvader/baggy/tool"
)

// Func is a tool made of a function.
type Func struct {
	N, D string
	S    string // JSON Schema
	F    func(ctx context.Context, args json.RawMessage) tool.Result
}

func (f Func) Name() string                                               { return f.N }
func (f Func) Description() string                                        { return f.D }
func (f Func) Schema() json.RawMessage                                    { return json.RawMessage(f.S) }
func (f Func) Execute(ctx context.Context, a json.RawMessage) tool.Result { return f.F(ctx, a) }

func errResult(format string, a ...any) tool.Result {
	return tool.Result{Text: fmt.Sprintf(format, a...), IsError: true}
}

// findBot resolves a bot by id or by a name only one bot has.
func (t *Team) findBot(ref string) (store.Bot, error) {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "@")
	if b, err := t.Store.Bot(ref); err == nil {
		return b, nil
	}
	named, err := t.Store.BotsNamed(ref)
	if err != nil {
		return store.Bot{}, err
	}
	switch len(named) {
	case 0:
		return store.Bot{}, fmt.Errorf("no bot is called %q", ref)
	case 1:
		return named[0], nil
	}
	return store.Bot{}, fmt.Errorf("%d bots are called %q; use the id", len(named), ref)
}

func (t *Team) teamTools(b store.Bot, chat store.Chat, r *run) []tool.Tool {
	tools := []tool.Tool{
		Func{N: "message_bot", D: "Send a message to another of the user's bots. It lands in that bot's direct chat with the user, where it answers (and can message you back). Use it to hand a task to the teammate suited to it.",
			S: `{"type":"object","properties":{"bot":{"type":"string","description":"the bot's id or name"},"text":{"type":"string"}},"required":["bot","text"],"additionalProperties":false}`,
			F: func(ctx context.Context, raw json.RawMessage) tool.Result {
				var a struct{ Bot, Text string }
				json.Unmarshal(raw, &a)
				to, err := t.findBot(a.Bot)
				if err != nil {
					return errResult("%v", err)
				}
				if to.ID == b.ID {
					return errResult("that is you")
				}
				if chat.Kind == store.Group && member(chat, to.ID) {
					return errResult("%s is in this group: talk to it here", to.Name)
				}
				t.mu.Lock()
				hops := r.hops + 1
				t.mu.Unlock()
				if hops > MaxHops {
					return errResult("refused: %d bot-to-bot messages since the user last spoke; ask the user first", MaxHops)
				}
				dm, err := t.Store.DMOf(to.ID)
				if err != nil {
					return errResult("%v", err)
				}
				msg := agent.Message{ID: store.NewID("msg"), Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: a.Text}}}
				if err := t.deliver(dm.ID, b.ID, store.Meta{From: b.ID, Hops: hops}, msg); err != nil {
					return errResult("%v", err)
				}
				return tool.Result{Text: fmt.Sprintf("Delivered to %s, who answers in its direct chat with the user.", to.Name)}
			}},
		Func{N: "create_bot", D: "Create a new bot, a teammate with its own name, description (its system prompt: what it does and how) and workspace. Only when the user agreed to it. A template fills in what is left out: " + templateList() + ".",
			S: `{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},"template":{"type":"string","enum":` + templateIDs() + `,"description":"start from this template: its description, look and suggestions"},"workdir":{"type":"string","description":"a full path to an existing folder its tools run in, as a project the user named; its own workspace when left out"},"join":{"type":"boolean","description":"also add it to this group"}},"required":["name"],"additionalProperties":false}`,
			F: func(ctx context.Context, raw json.RawMessage) tool.Result {
				var a struct {
					Name, Description, Template, Workdir string
					Join                                 bool
				}
				json.Unmarshal(raw, &a)
				nb := store.Bot{Name: a.Name, Description: a.Description, Workdir: a.Workdir}
				if tp, ok := store.TemplateOf(a.Template); ok {
					nb = tp.Bot()
					nb.Name = cmp.Or(strings.TrimSpace(a.Name), nb.Name)
					nb.Description = cmp.Or(strings.TrimSpace(a.Description), nb.Description)
					nb.Workdir = cmp.Or(a.Workdir, nb.Workdir)
				} else if a.Template != "" {
					return errResult("there is no template %q", a.Template)
				}
				if strings.TrimSpace(nb.Description) == "" {
					return errResult("a new bot needs a description or a template")
				}
				nb.Provider, nb.Model, nb.Thinking = b.Provider, b.Model, b.Thinking
				nb, err := t.Store.CreateBot(nb)
				if err != nil {
					return errResult("%v", err)
				}
				note := ""
				if a.Join && chat.Kind == store.Group {
					if err := t.Store.AddMember(chat.ID, nb.ID); err != nil {
						note = " It could not join this group: " + err.Error()
					} else {
						note = " It joined this group."
					}
				}
				t.emit(Event{Kind: EventChanged, ChatID: chat.ID, BotID: nb.ID})
				return tool.Result{Text: fmt.Sprintf("Created %s (id %s).%s", nb.Name, nb.ID, note)}
			}},
	}
	if chat.Kind == store.Group {
		tools = append(tools, Func{N: "hand_off", D: "Hand this group's work to another member, who becomes its owner. Only the owner can.",
			S: `{"type":"object","properties":{"bot":{"type":"string","description":"the member's id or name"}},"required":["bot"],"additionalProperties":false}`,
			F: func(ctx context.Context, raw json.RawMessage) tool.Result {
				var a struct{ Bot string }
				json.Unmarshal(raw, &a)
				c, err := t.Store.Chat(chat.ID)
				if err != nil {
					return errResult("%v", err)
				}
				if c.Owner != b.ID {
					return errResult("only the owner can hand off the work")
				}
				to, err := t.findBot(a.Bot)
				if err != nil {
					return errResult("%v", err)
				}
				if err := t.Store.SetOwner(chat.ID, to.ID); err != nil {
					return errResult("%v", err)
				}
				t.emit(Event{Kind: EventChanged, ChatID: chat.ID, BotID: to.ID})
				return tool.Result{Text: to.Name + " owns the work now."}
			}})
	}
	tools = append(tools, t.listTeammates(b), t.editBot(b))
	return tools
}

func (t *Team) listTeammates(b store.Bot) tool.Tool {
	return Func{N: "list_teammates", D: "List the user's bots: id, name, description, the provider, model and thinking each runs with, and whether it is working now.",
		S: `{"type":"object","properties":{},"additionalProperties":false}`,
		F: func(ctx context.Context, raw json.RawMessage) tool.Result {
			bots, err := t.Store.Bots()
			if err != nil {
				return errResult("%v", err)
			}
			var s strings.Builder
			for _, o := range bots {
				model := o.Model
				if cfg, err := Config(t.Store, o, connectFallback(t)); err == nil {
					model = cfg.Model
				}
				fmt.Fprintf(&s, "- %s (id %s)", o.Name, o.ID)
				if o.ID == b.ID {
					s.WriteString(" — you")
				}
				fmt.Fprintf(&s, ": model %s", model)
				if o.Thinking != "" {
					s.WriteString(", thinking " + o.Thinking)
				}
				if dm, err := t.Store.DMOf(o.ID); err == nil && t.Busy(dm.ID) {
					s.WriteString(", working now")
				}
				if d := strings.TrimSpace(o.Description); d != "" {
					s.WriteString("\n  " + strings.ReplaceAll(d, "\n", "\n  "))
				}
				s.WriteString("\n")
			}
			return tool.Result{Text: s.String()}
		}}
}

func (t *Team) editBot(b store.Bot) tool.Tool {
	return Func{N: "edit_bot", D: "Change a teammate's profile, or your own: only the fields given change. A description is the complete account of the bot's job and how it works; it applies from that bot's next turn. Only when the user asked for it.",
		S: `{"type":"object","properties":{"bot":{"type":"string","description":"id or name"},"name":{"type":"string"},"description":{"type":"string"},"model":{"type":"string"},"thinking":{"type":"string","enum":["","off","low","medium","high"]},"workdir":{"type":"string","description":"a full path to an existing folder its tools run in; \"\" puts it back in its own workspace"}},"required":["bot"],"additionalProperties":false}`,
		F: func(ctx context.Context, raw json.RawMessage) tool.Result {
			var a struct {
				Bot, Name, Description, Model string
				Thinking, Workdir             *string
			}
			json.Unmarshal(raw, &a)
			o, err := t.findBot(a.Bot)
			if err != nil {
				return errResult("%v", err)
			}
			if a.Name != "" {
				o.Name = a.Name
			}
			if a.Description != "" {
				o.Description = a.Description
			}
			if a.Model != "" {
				o.Model = a.Model
			}
			if a.Thinking != nil {
				o.Thinking = *a.Thinking
			}
			if a.Workdir != nil {
				o.Workdir = *a.Workdir
			}
			if err := t.Store.UpdateBot(o); err != nil {
				return errResult("%v", err)
			}
			t.emit(Event{Kind: EventChanged, BotID: o.ID})
			return tool.Result{Text: fmt.Sprintf("Updated %s (id %s); its tools run in %s.", o.Name, o.ID, t.Store.WorkDir(o))}
		}}
}

// templateList names the bot templates for create_bot's description.
func templateList() string {
	var parts []string
	for _, tp := range store.Templates {
		parts = append(parts, tp.ID+" ("+strings.TrimSuffix(tp.Summary, ".")+")")
	}
	return strings.Join(parts, ", ")
}

func templateIDs() string {
	var ids []string
	for _, tp := range store.Templates {
		ids = append(ids, tp.ID)
	}
	b, _ := json.Marshal(ids)
	return string(b)
}
