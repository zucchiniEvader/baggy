# Claude Code

A bot can hand its coding tasks to the user's own Claude Code: the `claude` command on this computer, signed in to the user's account, so the work runs on their plan (Pro, Max) rather than on an API key of Baggy's. The bot plans, writes the task, and checks the result; Claude Code reads the code, edits files and runs commands. Baggy never signs anyone in: it uses the sign-in `claude` already has.

## The protocol (`claudecode`)

After how Paseo and Orca drive it, through the protocol the Claude Agent SDK speaks, without the SDK. A run starts

```
claude -p --input-format stream-json --output-format stream-json --verbose \
       --permission-prompt-tool stdio --permission-mode acceptEdits [--resume <session>]
```

in the bot's folder, with the login shell's environment less `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, `CLAUDECODE` and `CLAUDE_CODE_ENTRYPOINT` (`Unset`), so it works on the account it is signed in to. One JSON object per line each way. Baggy writes a `control_request` `initialize`, then the task as a `user` message. Claude Code answers `initialize` with the account (`subscriptionType`, `Run.Account`), then sends `system`/`init` (the session id; `apiKeySource` other than `none` makes the account "API key"), `assistant` messages (text, and `tool_use` blocks that become `Step`s, a subagent's marked `Nested`), `user` messages with the `tool_result`s (a step done or failed), and at the end a `result` (`subtype`, `is_error`, `result`: its report, `num_turns`, `session_id`). Baggy then closes stdin and the command exits: a run is one process, and a follow-up is a new process resuming the session.

Under `acceptEdits` Claude Code edits files and runs file commands (`mkdir`, `rm`, `mv`) in its folder without asking. Anything else it asks about with a `control_request` `can_use_tool` (`tool_name`, `input`, `description`, `permission_suggestions`), which `Options.Approve` answers: `{"behavior":"allow","updatedInput":…}` or `{"behavior":"deny","message":…}` in a `control_response`. `AskUserQuestion` is denied at once with a note telling it to decide and say what it assumed; other requests are answered with an error.

`Manager.Start` refuses a second run of the same bot in a chat, more than `MaxRuns` (two) at once, a missing folder and a missing `claude` (`Find` looks on the environment's PATH). A resume whose session is gone (`No conversation found` on stderr, an error result after no turns) starts over without it (`Run.Fresh`). `Run.Stop` kills the process group; `Done` reports "finished", "failed: …" (the result's words, else the last line of stderr) or "stopped: …"; `OnEnd` hears every end. `Progress(n)` is the last steps as lines (`✓ Edit main.go`, `✗ Bash make`, `… Bash go test ./...`).

Checked against Claude Code 2.1.296 on a Pro plan: the account and the session come back, a write is accepted in the folder, a command outside the rules comes as `can_use_tool` (`GOBOT_CLAUDE_E2E=1 go test ./claudecode -run Real`).

## In a turn (`team`)

With `Team.ClaudeCode` set, a bot whose `Bot.ClaudeCode` is on gets three tools and a Claude Code section in its system prompt (code changes are Claude Code's; write the task in full; check the work before reporting it done):

- `claude_code(task, new_session?)`: starts a run in the bot's working folder (`Store.WorkDir`), continuing the session kept for the chat, the bot and the folder (`Store.ClaudeSession`) unless `new_session`. It waits `ClaudeCodeSettle` (three seconds): a run that ends by then (failed to start, not signed in, or done) is reported in the call's result; else the call says it is working and returns.
- `claude_code_status(run_id?, wait_seconds?)`: the run's recent steps and what it said last, or its report once ended; the bot's latest run in the chat when no id.
- `claude_code_stop(run_id?)`.

When a run ends by itself after the call returned, the report (`ClaudeCodeReport`: a first line on how it ended, how long and how many steps, which is all the desktop shows of it; then the result) is delivered into the chat from `store.System`, which starts a turn, as a background command's end does. A stopped run tells no one. Every end keeps the session for the next task (`SetClaudeSession`).

Each `can_use_tool` goes through `Team.Approve` as a call named `claude_code:` plus the tool (`ClaudeCodeCall`), its input the arguments, for the bot and the chat that started the run.

## On the desktop

- The bot sheet has a Claude Code switch, "Hand coding tasks to Claude Code", under Folder; while `claude` is not found it says so. The Engineer template has it on (with a folder to choose).
- The Commands preference covers Claude Code's requests as it does shell commands: under Run they are allowed; under Ask every time and Auto-review they wait in the bar above the composer ("Chef wants to run in Claude Code, Bash: make"), with Allow, Always Allow (the exact command for Bash, `claude_code:Bash:<command>`, else the tool) and Deny.
- Running tasks lists a chat's runs above its commands: Claude Code and the task, who and how long, the last eight steps, what it said last, and Stop. The window redraws every second while a run is at work. Quitting stops every run.
