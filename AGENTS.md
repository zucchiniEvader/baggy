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
terminal/   commands in pseudo-terminals: questions, input, background (docs/terminal.md)
shellenv/   the login shell's environment
desktop/    the MyGo app, its own module (docs/desktop.md)
docs/       one doc per subject, written as the system is
```

## Rules

- Write the current system in the docs, in the present tense, in the same change as the code.
- The agent package imports no vendor code and no UI. Providers convert to and from `agent.Message`; the UI observes `agent.Event`s.
- The transcript keeps thinking signatures and vendor server blocks verbatim (`Part.Signature`, `PartServer`) so a turn can continue after a tool call.
- Tool arguments are checked against the tool's JSON Schema before a call runs; a failure is an error result the model reads.
- `go vet ./... && go test ./...` pass before a change is done, in the root and in `desktop/`.

## Desktop design: native, restrained, quiet

After `../tsubomi` (whose `AGENTS.md`, `theme.go` and `controls.go` set it out, Kopi's style). The app should feel like the system's own apps (Finder, Notes, Xcode), not a web page or Electron: quiet, layered by space and light and shade rather than borders and color blocks. The content leads; the chrome steps back.

**Principles**
1. **The system's things**: the system font (SF Pro; code in SF Mono), the system accent (no brand blue), light and dark mode, native menus and context menus, native pop-up menus for every choice (a pill or button with `Menu`, never a drawn drop-down or `ui.Select`), the sidebar's vibrancy, a hidden title bar whose content keeps clear of the traffic lights (`c.TitleBar()`, never fixed coordinates).
2. **Neutral gray for states**: hover and choice lay a translucent neutral gray `rgba(127,127,127,α)` over whatever is below, the same in light and dark: hover α 0.08, pressed 0.16, chosen with the pointer or without the focus 0.20. The accent marks only the row the keyboard is moving the choice to (the list has the focus and the arrows moved it); a click shows gray, so moving the focus never flashes blue. Popups that own the keyboard (the palette, the `@` menu) may keep the accent.
3. **Few borders**: areas part by their background. A border, when one is needed, is 1px `rgba(17,17,17,0.10)` (dark `rgba(230,230,230,0.10)`).
4. **Few colors, fixed meanings**: red is an error or a delete, green is added or done, orange is a reference, a type or a warning, teal is tests. Other text has two levels only: text and muted (about 48% opacity). Bots are told apart by their icon and name, not by colors.

**Tokens** (`desktop/theme.go`)

| Token | Light | Dark |
| --- | --- | --- |
| appBg (window, chat) | #f8f8f6 | #141414 |
| surface (cards, composer) | #ffffff | #1c1c1c |
| header | #ffffff | #232323 |
| sidebar (no vibrancy) | #ececea | #1f1f1f |
| text | #111111 | #c8c8c8 |
| muted | rgba(17,17,17,.48) | rgba(230,230,230,.48) |
| border | rgba(17,17,17,.10) | rgba(230,230,230,.10) |
| pill, field fill | rgba(127,127,127,.11) | rgba(127,127,127,.16) |
| code background | #f0f0ee | #202020 |
| user bubble | #ececea | #262626 |
| red (text / bar) | #b8312f / #ff2e3f | #ee7e7e / #ff6762 |
| green (text / bar) | #1f7a44 / #0dbe4e | #6fd094 / #5ecc71 |
| orange | #c56e0e | #eb9a3d |
| teal | #13867a | #4ec9b0 |

**Sizes**: type 13 for body and lists, 12 secondary, 11 captions and paths, 10 badges, 15–17 titles; weights 400, 600, 700 only. Rows 28, compact buttons 26, buttons and fields 30, the title bar 40. Radii 5–6 for rows and small buttons, 8–12 for cards and popovers, 14–16 for large panels, capsules for counts. Spacing on multiples of 4 (gaps 2, 4, 6, 8, 10), list rows 1px apart, the sidebar's padding 8. Icons are Lucide line icons at 12–14, muted, taking the text's color when chosen.

**Components** (`desktop/controls.go`; every field, button, row and pill comes from there)
- **Sidebar**: vibrancy (or the sidebar color), a title-bar row with the sidebar toggle and New, a search field, the chats as rounded rows (not full-width highlights), names shrinking to one line with "…", counts as monospaced capsules on the right, Settings at the foot. Resizable between a minimum and a maximum, collapsing below it, sliding open and closed with a clip.
- **Buttons**: primary (the accent, the action a dialog is for, last in its row), secondary (pill gray), ghost (no fill until hovered: the icon buttons of bars), round (the composer's send and stop). A disabled button is gray with a muted label, never a faded accent.
- **Fields**: filled with the pill gray, borderless, a focus ring around the box.
- **The conversation**: at most 760 wide, centered. The user's messages in a neutral bubble on the right; a bot's replies as text, under its icon and name at the start of a turn; thinking closed under a "Thinking" line that opens it; a run of tool calls as one card of rows (disclosure arrow, icon, label, a muted summary, the status: a spinner, a check, a red mark), each opening on its arguments and result in code blocks; an error as a pill-gray row of red text with an alert icon, not a red block.
- **Composer**: a card (surface, border, radius 12) holding the text, a hint line ("↩ Send · ⇧↩ New line"), and a round send button that becomes stop while the chat works.
- **Empty states**: centered: a small icon in a gray tile, a line, one or two actions or a few suggestions. No long explanations.
- **Dialogs**: a 17 bold title over a 12 muted line, fields in a `ui.Form`, Cancel and the primary action at the bottom right; confirmations are `ui.AlertDialog`.

**Motion**: 150–220ms, ease-in-out; the sidebar slides and clips; under Reduce Motion everything completes at once; no bounces, no scaled entrances, no skeletons.

**Layout**: all text shrinks in a narrow space (`MinWidth(0)` and one line with an ellipsis), never spilling over other controls; secondary text that does not fit hides whole rather than leaving a lone "…".

**Don't**: gradients, big shadows, colored background blocks, thick borders, brand-colored buttons, web-style hover underlines, drawn scroll bars, a toast for everything, emoji as icons, a card around every section.

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
9. ~~Terminal sessions.~~ A command in a pseudo-terminal of its own on macOS and Linux: `bash_input`, `bash_output`, background commands, the login shell's environment, the command's card and Running tasks.
10. ~~Review and reach.~~ Auto-review of commands and plugin calls by a small model with Always allow; system notifications and unread counts; `list_teammates` and `edit_bot`; `ls`, several edits per `edit_file`, `.gitignore` in `find`/`grep`, `recall` by time; bot looks; context size per chat.
11. **Ship.** `mygo build` for macOS and Windows, a shell for Windows (Git Bash, else PowerShell), updater, icon, onboarding, Windows testing, and a look at the window by hand.

Decisions made: codemode on goja (pure Go); no ChatGPT or Grok subscription sign-in for now; SQLite through `modernc.org/sqlite`; markdown through goldmark folded into `ui.RichText` as `../lorca/desktop/markdown_view.go` does; API keys in the app's config file until a keychain is worth it.
