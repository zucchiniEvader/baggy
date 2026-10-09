# gobot

A desktop personal assistant with group chat, written in Go: the agent core is our own, the UI is [MyGo](https://github.com/egoist/mygo) native. The design follows `../lorca`, a Rust project with the same shape; read it, don't copy it.

## Reference: `../lorca`

Read these in `../lorca` before designing the part they cover.

| For | Read |
| --- | --- |
| Overall shape (UI ↔ core split, Runners, constraints) | `../lorca/ARCHITECTURE.md` |
| The agent loop, events, hooks, steering, `Agent` | `../lorca/docs/agent/loop.md`, `agent.md`, `hooks.md`, `how-it-works.md` |
| Provider wire formats, thinking levels, catalog, cost | `../lorca/docs/agent/providers.md`, `../lorca/docs/architecture/providers.md` |
| Tools, codemode | `../lorca/docs/agent/tools.md`, `codemode.md` |
| Compaction and memory flush | `../lorca/docs/agent/compaction.md`, `../lorca/docs/architecture/bots.md` |
| Group chats, lead bot, handoff | `../lorca/docs/architecture/bots.md` |
| Terminal sessions | `../lorca/docs/architecture/terminal-sessions.md` |
| MyGo desktop app (model, host, views, builds) | `../lorca/docs/architecture/desktop-app.md`, code in `../lorca/desktop/` |
| Rust sources of the loop | `../lorca/crates/agent/src/` (`agent_loop.rs`, `transform.rs`, `compaction.rs`, `providers/`) |

`../lorca/docs/agent/coming-from-pi.md` lists where the design departs from pi. The `../lorca` checkout is read-only reference: change nothing there.

## Layout

```
agent/      transcript types (Message, Part, Usage) and the loop (Run, Hooks, Event)
provider/   Provider interface, Request, stream Events; one adapter per wire protocol
  anthropic/  the Messages API adapter (docs/providers.md)
  openai/     the Chat Completions and Responses adapters
  connect/    a provider from a protocol name and settings, or the environment
  internal/wire/  posting with retries, SSE reading, shared by the adapters
tool/       Tool interface and Result, argument validation, shell and file tools
store/      SQLite state: providers, bots, chats, members, messages, settings (docs/store.md)
team/       runs DMs and group rooms, projection, system prompts, team tools (docs/team.md)
desktop/    the MyGo app, its own module (docs/desktop.md)
docs/       one doc per subject, written as the system is
```

## Rules

- Write the current system in the docs, in the present tense, in the same change as the code.
- The agent package imports no vendor code and no UI. Providers convert to and from `agent.Message`; the UI observes `agent.Event`s.
- The transcript keeps thinking signatures and vendor server blocks verbatim (`Part.Signature`, `PartServer`) so a turn can continue after a tool call.
- Tool arguments are checked against the tool's JSON Schema before a call runs; a failure is an error result the model reads.
- `go vet ./... && go test ./...` pass before a change is done, in the root and in `desktop/`.

## Roadmap

One machine, one user; no Runners, relay, phone app or marketplace. Milestones run in order; each ends with tests and its docs.

1. **Persistence and bots.** `store` over SQLite (`modernc.org/sqlite`, no cgo): bots, chats (DM and group, owner, description), members, messages (`agent.Message` as JSON plus author and sequence, written on `message_end`), settings, providers (a `connect.Config`: protocol, base URL, key, models). A bot is a name, a description (its system prompt), a model, a workspace folder keyed by id. A first bot, **Chef**, on first run. Desktop: sidebar of chats, new-bot and provider/model settings sheets, transcript restored on start.
2. **Group chat.** A room engine after `../lorca/docs/architecture/bots.md`: members take turns in order (mentioned first), a turn replies or says `PASS`, rounds repeat while someone has something new, at most four. Group transcripts project to each bot (its own messages as assistant, others as user messages naming the speaker). `message_bot` with a hop limit, `create_bot`, owner and handoff, replies and `@` mentions, Stop. Tested with scripted models; desktop: group view, author names and colors, `@` menu, members inspector.
3. **Context and memory.** Token estimate, compaction (`kind: "compaction"` summary), a silent memory-flush turn first. Per-bot `MEMORY.md` plus logs, `memory_update`, `memory_log` and `recall` tools, the recent-work brief. A model catalog (context window, vision, price) so `Usage.Cost` is real.
4. **Desktop polish.** Markdown in replies, interrupting a step for steering, approval cards for shell commands (`BeforeToolCall`), image attach and paste, tray and keep-running, command palette, Chinese l10n, window and pane preferences.
5. **Reliability and breadth.** Retry of failed model calls in the loop, the Responses adapter checked against a live server, `drop_bound_thinking`, the request image limit, more tools (edit, find, grep), thinking replay checked against Anthropic.
6. **Ship.** `mygo build` for macOS and Windows, a shell for Windows (Git Bash, else PowerShell), updater, icon, Windows testing.

Decisions made: SQLite through `modernc.org/sqlite`; markdown through goldmark folded into `ui.RichText` as `../lorca/desktop/markdown_view.go` does; API keys in the app's config file until a keychain is worth it.
