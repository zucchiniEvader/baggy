# Remote runtime

The bots can run on another machine of the user's tailnet, a server that is always on, while the desktop app shows them: its window is the same, its bots, chats, settings, files and approvals the server's. By default the desktop runs them itself. Both ends speak plain HTTP and a WebSocket over Tailscale, whose WireGuard tunnel encrypts them; a desktop's routes answer only peers on the tailnet or the machine itself.

## The host (`host`)

`host.Host` is one runtime: the `store.Store`, the `team.Team`, `MCPPath`, and the approvals. `host.Open` opens the store in a folder (moving the workspaces to `Options.Workspaces`, seeding Chef) and runs its bots as the desktop always has (`NewTeam`: the shell and file tools in the workspace, `agent.DefaultRetry`, memory, compaction, routing), with codemode over the plugins of `mcp.json`, terminals and Claude Code, the login shell's environment for all three; `Team.StartRoutines` starts routines once whoever hears `Options.Emit` is ready. `host.New` wraps a store and team made elsewhere (tests). `Close` stops delegations, Claude Code, commands and plugins, and closes the store.

Every method of a host takes and gives plain values, so it can cross the network as JSON:

| Methods | Do |
| --- | --- |
| `Snapshot` | the bots, the chats newest first, their last words (`Store.Previews`), unread counts, the busy chats |
| `SendFiles`, `Stop`, `Interrupt`, `Continue`, `Busy`, `RunRoutine` | the team's; `Stop` and `Interrupt` also deny what the chat has waiting |
| `Config`, `HasFallback`, `ClaudeCodeMissing` | a bot's `connect.Config` (`team.Config` with the host's fallback); whether the environment gives a provider; why `claude` cannot run, "" when it can |
| `Mode`, `Rules`, `Waiting`, `Decide`, `DenyAll` | the approvals, below |
| `Tasks`, `StopShell`, `BackgroundShell`, `StopClaude`, `StopDelegation` | a chat's running commands (their last ten lines), Claude Code runs (last eight steps, last words) and delegations (each task's state and last four steps), as they stand, and their actions |
| `Plugins`, `CheckPlugins`, `PluginsError`, `RestartPlugins`, `SaveServer`, `DeleteServer`, `CheckServer` | the servers of `mcp.json`: as they stood, reached now, the file's error, read again; edits to the file; a server tried before it is saved |
| `Memory`, `SaveMemory` | a bot's `MEMORY.md` and how much of it loads; a save refused with `memory.ErrChanged` when the bot changed it meanwhile |
| `Policy` | `team.Compaction.Policy` from the settings `pref.compact_warm`, `pref.compact_cold`, `pref.cache_ttl` (0.8, 0.4, 5m) |

**Approvals** are the host's `Team.Approve`. The mode is the `pref.commands` setting (`run`, `ask`, `review`; with none, `ask` when the older `pref.approve_shell` is on), read before each action. What goes through approval: `shell`, Claude Code's tools (`claude_code:<tool>`), and plugin tools whose server does not mark them read-only. One that matches a rule of `allow_rules` runs (`Rule`: `shell:<command>`, `claude_code:Bash:<command>`, `tool:<name>`). Under `review`, `team.Review` asks the bot's model first. A held action is a `Waiting` (the call, chat, bot, the review's reason, since when) and a `team.EventApproval` event with `Waiting` set; `Decide(id, ok, always)` answers it, `always` adding its rule; a second `EventApproval` says it no longer waits. A denial, Stop, or the turn's end gives the bot `host.Declined`.

## The server (`remote`)

`remote.Server` serves a host behind one token (`Authorization: Bearer …`, or `?token=` for a WebSocket or a picture). Phones use the routes of docs/mobile.md. A desktop uses three more, which answer only `Tailnet` peers (100.64.0.0/10, fd7a:115c:a1e0::/48, loopback) and give anyone else a 403:

| Route | Answers |
| --- | --- |
| `POST /v1/call` | `{method, args}` runs one method (`Call`): `Store.<name>` of `StoreCalls`, `Host.<name>` of `HostCalls`, each argument JSON decoded into the parameter's type by reflection. The answer is `{results, error}`: the results but a trailing `error`, and its message. After a store method that changes something (any but `storeReads`) every phone and desktop gets a `changed` event |
| `GET /v1/events?full=1` | the events in full (`FullEvent`): entries with their signatures and vendor blocks, tool output, approvals; 4096 may wait before a slow desktop is dropped |
| `GET /v1/files/{id}/{name}` | an attached file's bytes |

## baggyd

`cmd/baggyd` is a host with no window. Each release carries it for macOS and Linux (arm64, amd64) and an install script (docs/release.md):

```sh
curl -fsSL https://github.com/zucchiniEvader/baggy/releases/latest/download/baggyd-install.sh | sh
```

The script (`scripts/install-baggyd.sh`) downloads `baggyd-<os>-<arch>.tar.gz`, checks it against `baggyd-checksums.txt`, installs the binary in `~/.local/bin` (`BAGGYD_BIN`), and runs it as a service of the user: a launch agent on macOS (`io.github.zucchinievader.baggyd`, its log in `~/Library/Logs/baggyd.log`, running while the user is logged in), a systemd user service on Linux (lingering asked for, so it outlives the login), both restarting it after 10 seconds when it stops, as when Tailscale is not up yet. It ends by printing the link. `sh -s -- --no-service` installs only the binary, `--uninstall` stops and removes it (the data stays); `BAGGYD_VERSION` picks a release. `baggyd link` prints the links again; `baggyd version` its version. From source: `go run ./cmd/baggyd`. Its flags are `-data` (default as the desktop's: `GOBOT_DATA`, else the user config folder's `Baggy`), `-workspaces` (default `~/.baggy/workspaces`), `-port` (7841), `-listen` (default this machine's first Tailscale address, `TailnetAddresses`; with none it stops and says so), and `-reset-token`. The token is the `phone.token` setting, made on the first start. It prints a `baggy://pair?host=…&port=…&token=…&name=…` link (`PairURL`) for each address. The provider, with none stored, is the environment's (`connect.FromEnv`). A data folder takes one runtime at a time: stop the app's own (switch it to the server) before baggyd opens the same folder.

## The desktop's end

`desktop/runtime.json`, beside the database, holds the link (`{"link": "baggy://pair?…"}`), read before anything opens. Without one the window opens a host in its own process (`host.Open`), as always. With one, it opens no store: `remote.ParseLink` makes a `remote.Client`, and the window's `Store` and `Runtime` (`desktop/backend.go`, the interfaces `*store.Store` and `*host.Host` meet) are `remoteStore` and `remoteHost` over a `remoteBackend`:

- **Reads** (`Store.Bots`, `Host.Snapshot`, a chat's messages, a setting) are kept by method and arguments until a write or an event other than a streamed `agent` one, so a frame that reads asks the server once. `Host.Tasks`, which changes without events, is kept a second. A read waits at most 10 seconds, a write two minutes.
- **Offline**: a call that cannot reach the server (a dial waits 3 seconds) marks it offline. Calls then fail at once, one trying again every 5 seconds, so frames never wait on a dead peer. The sidebar's foot shows the server's name, or "Cannot reach X" in red with the reason as its tooltip.
- **Events** come over the full WebSocket (`Client.Events`), connecting again with backoff up to 15 seconds and pinging every 20 seconds, since a peer gone from the tailnet closes nothing. Each connection reads everything again (`app.resync`): the preferences, the snapshot, which chats work, every open transcript and its held actions.
- **Files**: an avatar's or an attachment's path is one under the user cache folder's `Baggy/remote/<host_port>`, downloaded the first time a picture reads it (`readFile`).
- **What stays local**: the window, the clipboard, notifications, the Dock's badge. Show Folder, Show in Finder, Open mcp.json and a bot's Choose… folder hide, since those paths are the server's (a working folder can still be typed). Attached files are read here and sent with the message.

Settings › General › Runtime switches it: Bots run on This computer or A server on Tailscale, the server's link (Connect asks `/v1/hello` first, then keeps it), the state of the connection, and Restart Now (`mygo.App.Relaunch`), since the switch takes a restart. Going back to This computer clears the link. The Phone pane, on a server, shows the server's own link as the pairing code: phones pair with the server, not with this window.

The preferences live in the server's settings either way, so two desktops on one server share them, the sidebar's width and the language too.
