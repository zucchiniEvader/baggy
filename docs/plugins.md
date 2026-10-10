# Plugins and codemode

## MCP servers (`mcp`)

The user's MCP servers live in `mcp.json` in the data folder, in the format Claude Desktop, Cursor and Claude Code share: `{ "mcpServers": { "<name>": { … } } }` (VS Code's `servers` reads too). An entry is a command (`command`, `args`, `env`, `cwd`) or a remote server (`type: "http"`, `url`, `headers`, where `${VAR}` reads the environment), with `description`, `disabled`, and `timeout` (seconds, or milliseconds from 1000; ten minutes unset).

`mcp.Connect` speaks JSON-RPC 2.0, protocol `2025-06-18`, written from the spec (no SDK):

- **stdio**: the command starts with the environment (`mcp.Env`, the login shell's when set) plus its `env`; one JSON message per line; lines that are not JSON (a server's logs) are skipped; a request from the server is answered "method not found"; a cancelled call sends `notifications/cancelled`; stderr's last line explains an exit.
- **Streamable HTTP**: each message is a POST accepting JSON or an event stream (progress notifications are skipped until the response); `mcp-session-id` from `initialize` goes on every later request and a DELETE ends it; 401 and 403 say the server wants a sign-in, which for now means headers in `mcp.json` (OAuth sign-in is not built).

`initialize` (with the server's name and instructions, and whether its capabilities offer `resources`: `Client.HasResources`), `notifications/initialized`, `tools/list` (every page), `tools/call` (`content`, `structuredContent`, `isError`), and for a server with resources `resources/list` and `resources/templates/list` a page at a time (`cursor`, `nextCursor`; MCP Apps' `ui://` interfaces left out) and `resources/read` (`text`, or a base64 `blob`).

`mcp.Manager` keeps the file's servers by `ID` (the name lowercased, runs of other characters as `_`: `Fake Tools` → `fake_tools`). It reads the file again when it changes, connects a server on first use (a minute at most) and keeps it and its tool list, waits 30 seconds before trying a failed one again, and reconnects once when a call finds the connection broken. `Statuses` connects every server that is on; `Known` reports without connecting; `Call("<id>__<tool>", args)` runs one; `Lookup` finds a tool's declaration; `Resources`, `ResourceTemplates` and `ReadResource` reach a server's resources by its id, and a `Status` says whether it offers them.

Tested against a fake stdio server (the test binary) and an `httptest` Streamable HTTP server, and live against a server built on the official TypeScript SDK (1.30) over stdio and the reference `@modelcontextprotocol/server-everything` (seven resources, two templates, a document read).

## Codemode (`codemode`)

A bot uses plugins by writing a script, after pi's codemode: `codemode.Run(ctx, code, host, options)` runs JavaScript, the body of an async function, in a fresh goja VM with no timers, network, file system or modules. Its one way out is the host:

- `await tools.<name>(args)`: a tool by name. An unknown name rejects with the closest names. Calls run in parallel (eight at once by default; more than 1000 not yet awaited stop the script); each resolution runs on the VM's goroutine.
- `console.log` and `print` write the output; what the script returns is added after it (strings as they are, anything else as indented JSON).
- `searchTools(query)` ranks the tools by the words of the query, `describeTool(name)` gives one's declaration.
- `store(key, value)` and `load(key)` keep JSON values (1 MB each) for later scripts.
- A first line `// @options: {"max_output_tokens": …, "timeout_ms": …}` changes the budget (10,000 tokens by default, at most 50,000; the output keeps its start and end) or shortens the timeout (30 minutes by default). A timeout or Stop interrupts the VM wherever it stands.

A script that throws, times out, waits on a promise nothing will settle, or is stopped ends with the error after what it printed.

## In a turn (`team`)

With `Team.Plugins` (or `Team.Codemode`) set, every turn has the `codemode` tool. A script reaches the bot's file, search, shell and memory tools (argument checks and the turn's `BeforeToolCall`, so shell approval applies to a script's commands too) and every plugin tool as `tools.<server>__<tool>`, which resolves to `{content, structuredContent, isError}`. The model never gets plugin tools as tools of its own, so the tool list stays the same from turn to turn. Values a script stores are kept per chat and bot (`store.ScriptSave`). When a ready server offers resources, the turn also has three tools of its own, named as Codex's and pi's: `list_mcp_resources { server?, cursor? }` and `list_mcp_resource_templates { server?, cursor? }` (every such server's first page as JSON without `server`; a cursor needs one) and `read_mcp_resource { server, uri }`, whose text comes back as text (at most 100,000 characters) and whose binary content (a picture too, since a tool result carries only text) is saved to a private temporary file the result names. They only read, so scripts and routine checks may call them. The system prompt's Plugins section lists each ready server with its description, whether it offers resources, and its tools (first description line, "reads only" for `readOnlyHint`), and says why a failed one is not available.

**Routine checks** (`Team.RunCheck`, which the desktop sets as `Team.Checks`): a routine's `check` runs as a script whose calls may only read: the bot's `read_file`, `find`, `grep` and `recall`, and plugin tools marked `readOnlyHint`; anything else fails the check. Five minutes at most; `store`/`load` reach the bot's DM's values, so a check keeps what it has seen and returns only what is new. A returned undefined, null, false, or empty string, list or object is a quiet check; anything else starts the run with it (at most 4,000 characters). See `routines.md`.

## The desktop

The Settings window has an MCP Servers pane: each server with its state (its number of tools, the error, Off, Not connected yet), Open mcp.json (written from a template first when missing), and Reconnect, which reads the file again and connects every server in the background. A codemode card in the transcript shows the script's last lines.
