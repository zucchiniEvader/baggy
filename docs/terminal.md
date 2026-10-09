# Terminal sessions

`terminal` runs a bot's commands in pseudo-terminals of their own on macOS and Linux (`terminal.Supported`), after `../lorca/docs/architecture/terminal-sessions.md`. A computer the bots work on is often one nobody is watching, so a command can ask a question and be answered, keep running in the background, and outlive the call that started it. Windows keeps the pipe-based `tool.Shell` for now.

## The terminal

`Manager.Start` runs `bash -c` (else `sh`) on an 80×24 terminal (`creack/pty`), leading its own session and process group, which is what stopping it kills. The environment is `Manager.Env` (the desktop gives the login shell's, `shellenv.Env`) with `TERM=xterm-256color`, `PAGER=cat`, `GIT_PAGER=cat` and `GOBOT_SESSION`. The command runs after `stty -echo`, so what is typed (a `y`, a password) never comes back as output, and with SIGHUP ignored. A reader drains the terminal into the session's last 200 KB whether anyone reads it or not. What the model reads is text (`Sanitize`): escape sequences go, a carriage return redraws its line, a backspace takes back a character, other control characters go.

## When a call returns

A `shell` call returns when the command exits, or while it still runs: two seconds (`PromptQuiet`) after it stops on an open line that reads like a question (`password`, `passphrase`, `[Y/n]`, `(yes/no`, `Enter `, `Press `, `PIN`, `token`, `code`, `OTP`, `Continue?`, or a line ending in `?`, `:`, `>`, `)`, `]`, `$` or `#`) that nothing was typed after, or after 20 seconds without output (`Silence`), which says the command may be working or asking where the terminal does not show it. The result is the output since the bot last read (its last 50 KB), the session id, and a note on where the command stands. `timeout` still kills a command that runs that long. A cancelled call (Stop) ends its command, unless the user sent it to the background meanwhile.

- `shell_input { session_id, text, enter? }` types into a running command, Enter after the text unless `enter` is false (or the text is `\u0003`, Ctrl-C; spelled-out escapes such as `\u001b[B` type their keys), and waits as `shell` does.
- `shell_output { session_id, wait_seconds? }` reads what it printed since the bot last read, waiting up to `wait_seconds` (5 by default, 60 at most) for more or for its end.

## Background commands

`shell { …, background: true }` is for what should keep running: a dev server, a watcher, a long build. The call waits two seconds (`BackgroundSettle`), so a command that fails or asks at once reads as usual, and returns with its output so far and the session id. Stop in the chat (`Manager.StopChat`, from `team.Stop`) ends the chat's other commands and leaves background ones running. When one ends by itself, `Team.WakeOnExit` delivers `[Your background command … exited by itself. Its last lines: …]` from `store.System` into the chat it started in, which starts a turn, so a bot hears that its server crashed or its build finished. The system prompt tells bots to start such commands this way, never with `&` or `nohup`.

## Limits

At most `MaxSessions` (eight) run at once: a ninth stops the oldest that is not in the background, and a background one only when all are. A command with neither output nor input for 30 minutes (`IdleLimit`) stops, a background one only while it sits at a question. Ended sessions are forgotten after an hour; quitting the app stops every command.

## In a turn and on the desktop

With `Team.Terminals` set, a turn's `shell` from `Team.Tools` is replaced by `shell`, `shell_input` and `shell_output` for that chat and bot; approval of shell commands and codemode's `tools.shell` go through them too. A running shell card shows the command's last lines as they come (the window redraws every second while any command runs). While a chat has commands that have run two seconds or more, the header has Tasks (n), a popover with each command's description, who runs it in a group, how long it has run, its command, its last ten lines, Run in Background (for one a call still waits on) and Stop.

Checked against DeepSeek: asked to run a deploy script that asks `Continue? [y/N]`, the bot's `shell` call returned at the question and its `shell_input` `y` finished the deploy.

## shellenv

`shellenv.Env()` runs the user's shell once as an interactive login shell (`$SHELL -l -i -c 'env -0'`, ten seconds at most) and keeps what it exports, since an app started from the Dock gets launchd's bare `PATH` and would not find `npx`, `uvx`, Homebrew's tools or a version manager's. It falls back to the process's environment, and is the process's own on Windows. MCP server commands start with it too (`mcp.Env`).
