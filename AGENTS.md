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
tool/       Tool interface and Result, argument validation, shell and file tools
docs/       one doc per subject, written as the system is
```

## Rules

- Write the current system in the docs, in the present tense, in the same change as the code.
- The agent package imports no vendor code and no UI. Providers convert to and from `agent.Message`; the UI observes `agent.Event`s.
- The transcript keeps thinking signatures and vendor server blocks verbatim (`Part.Signature`, `PartServer`) so a turn can continue after a tool call.
- Tool arguments are checked against the tool's JSON Schema before a call runs; a failure is an error result the model reads.
- `go vet ./... && go test ./...` pass before a change is done.

## Next

1. A shared transcript transform (`../lorca/crates/agent/src/transform.rs`), then `provider/openaicompat`.
2. More tools (edit, find, grep).
3. SQLite store; bots and DMs; then group chat and handoff.
4. MyGo UI over `agent.Event`; compaction and memory.
