# Tools

A tool is a `tool.Tool`: a name, a description, a JSON Schema for its arguments, and `Execute(ctx, args) Result`. A failure is a `Result` with `IsError` set, never a Go error; the model reads the text.

## Argument validation

The loop calls `tool.ValidateArgs(name, schema, args)` before every `Execute`, after `BeforeToolCall` and the tool lookup. Empty args are `{}`. Values are first coerced the way a model tends to get them wrong: a numeric or boolean string where the schema wants a number, integer or boolean, and `null` for an optional property that does not accept null (the key is dropped). Then the result is validated; the tool runs with the coerced, re-encoded arguments.

Checked keywords: `type` (a number accepts an integer), `enum`, `properties`, `required`, `additionalProperties: false`, `items`, `minimum`, `maximum`. Other keywords are ignored; a schema that does not parse skips validation (the tool's problem, not the model's). A failure is an error result listing each problem by path (`root`, `n`, `xs.1`) followed by the received arguments.

## Built-in tools

Each takes a `Dir`; relative paths resolve against it.

| Tool | Arguments | Behaviour |
| --- | --- | --- |
| `shell` | `command`, `timeout` (seconds, default 120) | On pipes (Windows, or without `Team.Terminals`; see `terminal.md` for the terminal sessions that stand in for it): `bash -c`, stdout and stderr combined. Runs in its own process group; timeout or cancel kills the group. Non-zero exit, timeout and cancel are errors that keep the output. Output past 2000 lines or 50 KB keeps the end. |
| `read_file` | `path`, `offset` (1-based line), `limit` | Text only (a NUL in the first 8 KB is "binary file"). At most 2000 lines or 50 KB from `offset`; a cut ends with a note naming the offset to continue from. |
| `write_file` | `path`, `content` | Replaces the file, creating parent directories. |
| `edit_file` | `path`, `old`, `new`, `all` | Replaces the exact passage `old`, which must appear once unless `all`; none or several is an error asking for a copied or longer passage. A file with Windows line ends takes a passage written with Unix ones. The file keeps its mode. |
| `find` | `pattern`, `path` | Files under `path` whose slash path matches a glob (`*` within a name, `**` across folders, `?`, `{a,b}`; a pattern without a slash matches in any folder), newest first, at most 200. |
| `grep` | `pattern`, `path`, `glob` | Lines matching a Go regular expression in the text files under `path` (or that one file), `glob` limiting the files: `path:line: text`, at most 200 lines, each cut at 300 characters. |

`find` and `grep` skip `.git`, `node_modules`, `.hg`, `.svn`, `__pycache__`, `.venv`, `target` and `.cache`, and `grep` skips binary files.

`go run ./cmd/smoke [model]` gives a model these tools in a scratch directory and asks for a multi-step task; `GOBOT_TASK=edit` asks for one with `find`, `grep` and `edit_file` and no shell, which DeepSeek (`deepseek-chat`) did in five calls. Asking permission before a shell command is the desktop's (`desktop.md`, through `team.Team.Approve`).

## Not yet

`ls`; streaming partial output from a running tool; images in results.
