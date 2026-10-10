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

`Model` resolves a bot through `team.Config`: its provider, else the `default_provider` setting, else the first stored one, else the fallback config when it has a key; its model, else the provider's first. Optional fields: `Memory` turns on every bot's memory (`memory.md`), `Compaction` summarizes a chat's older part before a request outgrows the model's window (below), `Retry` is each turn's `agent.Retry`, `Now` is the clock, and `Approve` runs before each tool call with the bot and chat and may block it (the desktop's shell approval).

## Runs and events

A chat at work has one run. A message delivered to an idle chat is stored and starts a run in a goroutine; deciding to stop and leaving the run map happen under one lock, so a message arriving at that moment is either taken or starts a new run. `Stop` cancels the run's context at once. `Continue(chat)` runs a chat again from its transcript with no new message, a retry after a failed reply (which providers leave out). A bot with no provider to run on fails with `ErrNoProvider`. `Busy` and `Wait` report on runs.

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

Its bot answers. A message sent while it works goes to the run's steering queue and reaches the bot at its next model call; it is stored when the loop injects it (with its author and meta kept by message id), and one that arrives after the last read starts the next turn. `Interrupt(chat)` after a send cuts the bot's step short so it reads the message at once.

## Group: the room

After `../lorca/docs/architecture/bots.md`. A user message offers every member a turn, one at a time: `TurnOrder` puts the members it mentions first (in the order mentioned), then the author of the message it replies to, then the rest in chat order. A turn gets the transcript plus a cue that is not stored: the round, how many messages are new to the member, and "reply, or answer PASS alone". A reply `IsPass` (PASS, any case, stray punctuation) is taken out of the chat: the whole turn when it did nothing else, only the final reply when it ran tools first. A round with a reply is followed by another, offered only to members that heard something new since their last turn; the exchange ends after a silent round or after `MaxRounds` (four), the last cued as winding down. A user message arriving meanwhile is stored at once, and the room yields after the current turn; a new room starts with its mentions first.

## What a bot sees

`Project(entries, bot, bots)` is the chat from the newest `compaction` entry on: the bot's own messages as they are; the user's as user messages, the first `@Name` of each mention followed by `(id …)` and a reply prefixed `[Replying to your message: "…"]` (or `their earlier message`, or `Scout's message`); a `message_bot` delivery as `[Message from Chef (id …) via message_bot]`; other bots' replies as user messages `[Name]: text`, without their tool calls, results, or failed turns.

`SystemPrompt` is the bot's description, then who and where it is (its id and workspace), the chat (a DM, or the group's members in turn order with "you" and "owner", its description, and how the room works), the user's other bots with their first description line, and when to use `create_bot`. It changes only when those do, so the provider's prompt cache holds.

## Compaction

With `Team.Compaction` set, every model call of a turn goes through `agent.Options.Compact`. It estimates the request (system prompt, tool schemas, `agent.EstimateTokens` of the messages) against the model's window (`Compaction.Window`, else the catalog's, else 128k). Under `Threshold` (0.8) of it nothing happens. Over it:

1. The cut: the oldest user message, made by the user or from a stored entry (it has an id; never a tool result), such that what follows fits `Keep` (0.25) of the window. A tool call and its result are never parted. Nothing to cut, nothing happens.
2. The memory flush, with `Memory` on and `NoFlush` off: a silent turn over the same request (same system prompt and tools, so the prompt cache serves it) plus a prompt naming where the kept part starts and asking to save what is durable; `BeforeToolCall` refuses every tool but `memory_update` and `memory_log`, nothing it says is stored, and a failure or `FlushTimeout` (two minutes) goes on to the summary.
3. The summary: one call (`summarySystem`, no tools) over the cut part as a plain-text `Transcript`, whoever wrote it (so thinking and seals from any model never matter), asking for Goal, Decisions and facts, Work done, Open items and the user's preferences, folding in an earlier summary.
4. It is stored as a user message of `Kind: "compaction"` whose `Meta.KeepID` is the cut message's id, and the call goes on with the summary and the kept messages, their thinking dropped (it was bound to what came before). A model's "prompt is too long" forces this past the threshold (`force`).

`Project` then starts every bot's view of the chat at the newest summary, followed by the entries from the kept one on (`ProjectedID`: a message's own id, or `seq-N` for one made up for another bot's words), older summaries left out. A summary in a group serves every member. `go run` of a six-message DM against DeepSeek with a 6000-token window compacted once, flushed two facts to `MEMORY.md`, and answered a question about the cut part from the summary.

## Memory in turns

With `Memory` on, a turn gets `MemoryTools` (`memory_update`, `memory_log`, `recall`) and the bot's `memory.Prompt()` after its system prompt; after the transcript, the **recent-work brief**: the newest thing the bot said in each of its other chats over two days, newest first, at most ten lines and about 350 tokens (`- today 09:05 · your chat with the user · you said: "…"`), kept out of the system prompt so the cache holds. Each finished turn adds a diary line (`in group "Trip" · said "…" · used shell, read_file`); a pass with no tools adds none. `recall(query, days)` searches the memory files, the diary, and every chat the bot is in for messages holding every word, newest first, at most 20, naming the chat and the speaker.

## Routines

`StartRoutines` runs the bots' scheduled tasks in their DMs, and every turn has the `routines` tool and a Routines section in its system prompt: see `routines.md`.

## Commands

With `Terminals` set, a turn's shell commands run in terminal sessions (`shell`, `shell_input`, `shell_output`), Stop ends the chat's commands but those in the background, and `WakeOnExit` tells a bot when a background command ends by itself: see `terminal.md`. `Project` gives the bot such a note from `store.System` as it is.

## Plugins

With `Plugins` (an `mcp.Manager`) or `Codemode` set, every turn has the `codemode` tool and a Plugins section in its system prompt: see `plugins.md`.

## Team tools

- `message_bot(bot, text)`: by id or a name only one bot has; delivers into that bot's DM, where it answers. It refuses the bot itself and a member of the current group (talk there). Each delivery carries `Hops`, one more than the message that started the sender's turn (a user message resets it); past `MaxHops` (eight) it refuses, so two bots cannot loop.
- `create_bot(name, description, join)`: a new bot on the creator's provider and model; `join` also seats it in the current group.
- `hand_off(bot)` (groups): the owner makes another member the owner.
- `list_teammates`: every bot with its id, the model and thinking it runs with, whether it is working, and its description.
- `edit_bot(bot, name?, description?, model?, thinking?)`: changes a teammate's profile or the bot's own, only the fields given, from that bot's next turn.

## Review

`team.Review(ctx, model, bot, request, action, workspace)` asks a model (the desktop gives the bot's own) whether an action may run without the user: the user's last request (`LastRequest`), the bot's workspace, and the action. It answers JSON `{decision: allow | ask, reason}`: allow reading, listing, searching, building, testing, and changes in the workspace that follow from the request; ask before deleting or overwriting outside it, `sudo`, installs, messages and posts, `git push`, publishing, payments, uploads, anything the request does not call for, or when unsure. An answer that does not read is an ask. Against DeepSeek it allowed `ls -la` and `go test ./...` and asked for `rm -rf ~/Documents`, a force push, and an email the request did not call for.

Tests drive all of it with scripted models (`team/team_test.go`): DM steering and restore, a three-bot room with mentions and passes, the round limit, the hop limit, `create_bot` with `hand_off`, and Stop.
