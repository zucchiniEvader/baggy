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
agent/      transcript types, the loop (Run, Hooks, Event, Interrupt, Compact), Transform
provider/   Provider interface, Request, stream Events; one adapter per wire protocol
  anthropic/  the Messages API adapter (docs/providers.md)
  openai/     the Chat Completions and Responses adapters
  connect/    a provider from a protocol name and settings, or the environment
  internal/wire/  posting with retries, SSE reading, shared by the adapters
  catalog/    models.dev snapshot: windows, images, prices, cost
tool/       Tool interface and Result, argument validation, shell, file, edit, find, grep
store/      SQLite state: providers, bots, chats, members, messages, settings (docs/store.md)
team/       runs DMs and group rooms, projection, prompts, team tools, compaction (docs/team.md)
memory/     a bot's MEMORY.md, notes and diary, scrubbing, search (docs/memory.md)
schedule/   routine schedules: intervals, cron in a timezone, words (docs/routines.md)
mcp/        MCP client (stdio, Streamable HTTP) and the mcp.json manager (docs/plugins.md)
codemode/   scripts in a goja sandbox that call tools (docs/plugins.md)
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

1. ~~Persistence and bots~~: `store`, Chef, the desktop's sidebar, sheets and restored transcripts.
2. ~~Group chat~~: `team`'s room engine, projection, `message_bot`, `create_bot`, `hand_off`; group views, `@` menu, replies, inspector.
3. ~~Context and memory~~: `provider/catalog` and cost, compaction with a memory flush, `memory`, the brief, `recall`.
4. ~~Desktop polish~~: markdown, step interrupt, shell approvals, images, tray, palette, Chinese, window and pane preferences.
5. ~~Reliability and breadth~~: loop retry and overflow compaction, `provider/openai` (Chat Completions, Responses), bound thinking, the request image limit, `edit_file`, `find`, `grep`, thinking replay checked on DeepSeek's Anthropic endpoint. Still open, waiting for keys: the Responses adapter and Anthropic's own API live.
6. ~~Web search.~~ The server tools of the Anthropic endpoint (Anthropic's `web_search`/`web_fetch`, DeepSeek's `web_search`) declared per provider, their blocks replayed, and the searches shown in the transcript.
7. ~~Routines.~~ A bot's scheduled tasks after `../lorca/docs/architecture/routines.md`: the `routines` tool, schedules (`every 2h`, cron in a timezone), missed-run policy, read-only checks, runs in the bot's DM, the desktop's list and sheet.
8. ~~Plugins.~~ The user's MCP servers in `mcp.json` (Claude Desktop's format: stdio and HTTP), their tools reached from **codemode** scripts in a goja sandbox (`tools.<server>__<tool>`, only the output reaching the model), the desktop's MCP section.
9. **Terminal sessions.** A command in a pseudo-terminal of its own on macOS and Linux: `bash_input`, `bash_output`, background commands, the login shell's environment, the command's card and Running tasks.
10. **Review and reach.** Auto-review of commands and plugin calls by a small model with Always allow; system notifications and unread counts; `list_teammates` and `edit_bot`; `ls`, several edits per `edit_file`, `.gitignore` in `find`/`grep`, `recall` by time; bot looks; context size per chat.
11. **Ship.** `mygo build` for macOS and Windows, a shell for Windows (Git Bash, else PowerShell), updater, icon, onboarding, Windows testing, and a look at the window by hand.

Decisions made: codemode on goja (pure Go); no ChatGPT or Grok subscription sign-in for now; SQLite through `modernc.org/sqlite`; markdown through goldmark folded into `ui.RichText` as `../lorca/desktop/markdown_view.go` does; API keys in the app's config file until a keychain is worth it.
