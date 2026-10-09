# Desktop app

`desktop/` is a separate Go module (`github.com/spike/gobot/desktop`, `replace`d onto the repository root) so the core keeps no UI dependencies. It is a MyGo 0.3.6 app of native UI, drawn from Go with no web page; `go.mod` pins the `mygo` CLI as a Go tool, and goldmark parses markdown.

```
cd desktop
go tool mygo dev                           # builds, runs, relaunches on a change
go test ./...                              # views run in MyGo's offscreen Tester
GOBOT_RENDER=<folder> go test -run TestRender ./...   # writes group-light.png and group-dark.png
```

State is a `store` in `GOBOT_DATA` (default: the user config folder's `gobot`), seeded with Chef. Bots run on the providers added in Settings; with none, on the environment's (`connect.FromEnv`: `GOBOT_API`, `GOBOT_BASE_URL`, `GOBOT_API_KEY`, `GOBOT_MODEL`, or `ANTHROPIC_API_KEY`). A bot's tools are `shell`, `read_file`, `write_file`, `edit_file`, `find` and `grep` in its workspace, plus the team's and its memory's; failed model calls are retried (`agent.DefaultRetry`), the header reading "Retrying: …" meanwhile and the partial reply dropped.

## State and threads

Views are immediate: `app.root` builds the window every frame from the `app` struct, which belongs to the main thread. `team.Team` emits from its goroutines through `app.post` (the window's `Update`), so events apply on the main thread in order. Tests give `post` a channel and run it on the test goroutine.

Each opened chat has a `chatView`: rows built from its stored entries (`buildRows`), then kept current by events. A reply streams into a row keyed by its turn (`message_start`, deltas); its stored `entry` replaces that row with the final rows (text, thinking, a tool card per call); a tool entry fills the cards' results, and `tool_end` fills one as soon as it finishes; `removed` takes a passed turn's rows out. A chat not yet opened loads from the store when it is, so restarting the app restores every transcript.

## The window

- **Sidebar**: the chats, most recent first (a DM shows its bot, a group its title and size, "Working…" while busy); + makes a bot or a group; Settings at the bottom; a row's context menu edits or deletes. A chosen row is gray, and accent only while the arrows, Home or End move the choice: a key sets the list's `keyboard` flag, a click and losing the focus clear it, and moving with the keys keeps the focus in the list.
- **Chat**: a header with the title, the members or the bot's description ("Scout is working…" during a turn), Stop while busy, and Info. In a group each reply carries its author's name in the bot's color. A reply's web searches show above its text ("Searching the web for “…”…", then "Searched the web for “…”", "Read …", or the error), live from `server_tool` events and from the stored blocks after a restart. Replies render markdown (headings, emphasis, code spans and blocks, links that open web and mail URLs only, lists with task boxes, quotes, tables as lines, rules). A message's context menu has Reply above the editing commands.
- **Composer**: Enter sends, Shift+Enter starts a line. Typing `@` opens a menu of the group's members (in a DM, the other bots), the arrows and Enter or Tab pick one, and the pick is sent as a mention; an `@Name` typed out that only one bot answers to counts too. A reply shows "Replying to …" with its quote until sent, Escape or ✕. Images attach by pasting (Cmd+V with an image on the clipboard), dropping files on the composer, or ＋ (PNG, JPEG, GIF, WebP up to 5 MB). In a busy DM the button reads Steer, which waits for the current step; Interrupt (Cmd+Enter) sends and cuts the step short, denying a command waiting for approval.
- **Inspector** (Info): a DM's bot (description, provider and model, workspace, memory, what the chat has cost, Edit…, Delete Bot); a group's name and description (Edit…), what it has cost, its members with the owner marked and a menu each (Make Owner, Edit Bot…, Remove from Chat), Add Bot, Delete Group.
- **Sheets**: New/Edit Bot (name, description, provider, model, thinking), New/Edit Group (name, description, members), Settings (providers with Make Default, Edit…, Delete, Add Provider…, and the preferences), a provider (name, protocol, base URL, key, models, web search for the Messages API and Responses, on by default, and the protocol's reasoning switch), confirmations for deletes.
- **Command palette** (Cmd+K): the chats and the commands, filtered as typed; it owns the keyboard, so its highlight is the accent. Cmd+N new bot, Cmd+, Settings, Cmd+1–9 a chat, Escape stops the chat shown.

## Routines

The app starts the routine scheduler with the window; a bot's inspector lists its routines and opens a routine's sheet (`routines.md`).

## Plugins

The app keeps an `mcp.Manager` over `mcp.json` in its data folder, turns codemode on, and runs routine checks through it; Settings has the MCP Servers section (`plugins.md`).

## Memory

Bots run with memory and compaction on (`GOBOT_MEMORY=0` turns memory off, `GOBOT_MEMORY_FLUSH=0` the flush). The bot inspector's Memory rows show how much of `MEMORY.md` loads against the budget, Edit… opens it in an editor (saved through `memory.Save`, so a change the bot made meanwhile stops the save with Reload and Overwrite), and Show Folder opens the workspace. A compaction shows in the transcript as a note.

## Approvals and preferences

With "Ask before shell commands" on, every `shell` call waits on its card ("waiting for approval") for Allow or Deny; a denial, or Stop, gives the bot `Declined`. Preferences sit in the store's settings: show thinking, ask before shell commands, keep running when closed (closing hides the window; the tray icon's menu opens it again or quits), language (Auto follows the system's; English; 中文, from `l10n_zh.go`, keyed by the English string `L` takes), and the sidebar's width and the inspector, saved as they change. The window remembers its size, position and maximized state (MyGo's `StateKey: "main"`).

The built app has been started against a scratch `GOBOT_DATA` and ran without errors; its views are checked in the offscreen Tester and by the rendered PNGs, not by hand.
