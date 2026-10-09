# Team

`team.Team` runs every chat of a store: a DM's turns, a group's room exchange, and the tools bots use on each other. A UI calls `Send`/`SendMessage` and `Stop` and observes `team.Event`s; the store holds the result.

```go
tm := &team.Team{
	Store: st,
	Model: team.Models(st, connect.FromEnv("claude-sonnet-5-5")), // bot → agent.Model through the stored providers
	Tools: func(b store.Bot, ws string) []tool.Tool { return []tool.Tool{tool.Shell{Dir: ws}, ...} },
	Emit:  func(e team.Event) { ... },
}
tm.Send(chatID, "plan the launch", mentions, replyTo)
```

`Model` resolves a bot through `team.Config`: its provider, else the `default_provider` setting, else the first stored one, else the fallback config when it has a key; its model, else the provider's first. Optional fields: `Prepare` rewrites a turn's messages first (compaction), `Extra` adds tools per bot and chat (memory), `Brief` is a note after the transcript (the recent-work brief), `AfterTurn` sees each finished turn (the memory log), and `Approve` runs before each tool call with the bot and chat and may block it (the desktop's shell approval).

## Runs and events

A chat at work has one run. A message delivered to an idle chat is stored and starts a run in a goroutine; deciding to stop and leaving the run map happen under one lock, so a message arriving at that moment is either taken or starts a new run. `Stop` cancels the run's context at once. `Busy` and `Wait` report on runs.

| Event | Means |
| --- | --- |
| `run_start`, `run_end` | the chat started or stopped working |
| `turn` | `BotID` begins a turn (`Turn` is its id) |
| `agent` | a streamed `agent.Event` of that turn |
| `entry` | `Entry` was stored |
| `removed` | `Entry` was taken out (a pass) |
| `pass` | `BotID` passed |
| `changed` | bots or chats changed (`create_bot`, `hand_off`) |
| `error` | `Err` |

Every message a turn produces is stored on its `message_end`, with the turn id; a failed model resolution is stored as an error reply.

## DM

Its bot answers. A message sent while it works goes to the run's steering queue and reaches the bot at its next model call; it is stored when the loop injects it (with its author and meta kept by message id), and one that arrives after the last read starts the next turn.

## Group: the room

After `../lorca/docs/architecture/bots.md`. A user message offers every member a turn, one at a time: `TurnOrder` puts the members it mentions first (in the order mentioned), then the author of the message it replies to, then the rest in chat order. A turn gets the transcript plus a cue that is not stored: the round, how many messages are new to the member, and "reply, or answer PASS alone". A reply `IsPass` (PASS, any case, stray punctuation) is taken out of the chat: the whole turn when it did nothing else, only the final reply when it ran tools first. A round with a reply is followed by another, offered only to members that heard something new since their last turn; the exchange ends after a silent round or after `MaxRounds` (four), the last cued as winding down. A user message arriving meanwhile is stored at once, and the room yields after the current turn; a new room starts with its mentions first.

## What a bot sees

`Project(entries, bot, bots)` is the chat from the newest `compaction` entry on: the bot's own messages as they are; the user's as user messages, the first `@Name` of each mention followed by `(id …)` and a reply prefixed `[Replying to your message: "…"]` (or `their earlier message`, or `Scout's message`); a `message_bot` delivery as `[Message from Chef (id …) via message_bot]`; other bots' replies as user messages `[Name]: text`, without their tool calls, results, or failed turns.

`SystemPrompt` is the bot's description, then who and where it is (its id and workspace), the chat (a DM, or the group's members in turn order with "you" and "owner", its description, and how the room works), the user's other bots with their first description line, and when to use `create_bot`. It changes only when those do, so the provider's prompt cache holds.

## Team tools

- `message_bot(bot, text)`: by id or a name only one bot has; delivers into that bot's DM, where it answers. It refuses the bot itself and a member of the current group (talk there). Each delivery carries `Hops`, one more than the message that started the sender's turn (a user message resets it); past `MaxHops` (eight) it refuses, so two bots cannot loop.
- `create_bot(name, description, join)`: a new bot on the creator's provider and model; `join` also seats it in the current group.
- `hand_off(bot)` (groups): the owner makes another member the owner.

Tests drive all of it with scripted models (`team/team_test.go`): DM steering and restore, a three-bot room with mentions and passes, the round limit, the hop limit, `create_bot` with `hand_off`, and Stop.
