# Memory

`memory.New(workspace)` is a bot's long-term memory, plain markdown in its workspace (keyed by bot id, so a rename never moves it), after `../lorca/docs/architecture/bots.md`:

- `MEMORY.md`, the curated facts. Its first `LoadLines` (200) lines or `LoadBytes` (24 KB), whichever cuts first, open every turn's system prompt (`Prompt`); past that the prompt says how many lines are cut off and asks the bot to consolidate. The bot changes it only through `Update`, one dated entry per call (`- 2026-10-09 · from your chat with the user · the user prefers short replies`), never a whole-file write: `append` adds a fact (an exact duplicate is refused), `replace` rewrites one unique passage, `supersede` strikes the line holding a unique passage through with the date and appends the new fact, `remove` deletes a unique passage.
- `memory/<topic>.md`, longer notes the bot reads with `read_file`; the prompt lists them.
- `memory/log/YYYY-MM-DD.md`, the diary: what happened, not what is true (`- 09:05 · in group "Trip" · said "…" · used shell`). `Log` appends; a team turn adds a line as it ends, and the bot adds events with `memory_log`. The diary never loads into a prompt.

Every write is atomic (a temporary file renamed over the old) and scrubbed first (`Scrub`): API keys (`sk-…`, `sk-ant-…`), AWS keys, GitHub and Slack tokens, JWTs, bearer tokens, private key blocks, and the values of `password:`, `token=`, `api_key:` and the like become `«redacted N chars»`. One lock per folder keeps two turns of a bot from interleaving a write.

`Search(query, since)` finds the lines holding every word of the query in `MEMORY.md`, the topic notes and the diary (only diary days from `since` on when it is set). `Save(was, text)` replaces `MEMORY.md` with a person's edit unless the file no longer holds `was` (`ErrChanged`); `Loaded` reports how much of it loads.

The team's tools, the flush before a compaction, the recent-work brief and `recall` over chats are in `team.md`. The desktop's bot inspector shows how much of `MEMORY.md` loads and opens an editor that refuses to save over the bot's own change until Reload or Overwrite.
