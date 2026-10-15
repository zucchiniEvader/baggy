# Store

`store.Open(dir)` keeps the app's state in `dir/baggy.db` (a `gobot.db` from the app's old name is renamed first), one SQLite file through `modernc.org/sqlite` (pure Go, no cgo, so Windows builds cross-compile), with foreign keys, WAL, and one connection. Migrations are a list run up to `PRAGMA user_version`. Bot workspaces are folders keyed by id, so a rename never moves one: `Store.Workspace(id)` is `<root>/<bot id>`, the root `dir/workspaces` until `UseWorkspaces(root)` names another (the app's `~/.gobot/workspaces`). `UseWorkspaces` moves each workspace still in the old root to the new one (a rename, or a copy across volumes), keeps one the new root has already and leaves that bot's old folder where it was, removes the old root once it is empty, and reports the ones it could not move. Attached files are kept at `dir/attachments/<file id>/<name>` (`SaveFile`, `FilePath`) and copied to the same path in a bot's workspace by `Place`.

| Table | Holds |
| --- | --- |
| `providers` | `store.Provider`: id, name, `api` (`connect.Anthropic`, `Chat`, `Responses`), base URL, API key, models (JSON list, the first is the default), options (`replay_reasoning`, `encrypted_reasoning`) |
| `bots` | `store.Bot`: id, name, description (its system prompt), provider id (empty: the default), model (empty: the provider's first), thinking level, look (an icon name, kept in the `emoji` column), its avatar's color (a name; empty: one its id picks), its working folder (`Workdir`, empty for its workspace; `WorkDir(bot)` is the folder its tools run in, and creating or updating a bot refuses one that is not a full path to a folder), its avatar picture (`Avatar`, a file in `dir/avatars/` written by `SaveAvatar` under a new name each time; `UpdateBot` and `DeleteBot` remove the one it replaces), whether it hands coding tasks to Claude Code (`ClaudeCode`, the `claude_code` column), and the template it was made from (`Template`, set once when created; Chef's, `chef`, given to a bot named Chef by the migration that added it) |
| `chats` | `store.Chat`: id, kind (`dm` or `group`), title and description (groups), owner, created, updated (the last message), read mark (`ReadSeq`), when the user pinned it (`Pinned`, 0 when not; `SetPinned`) |
| `members` | the bots of a chat in turn order |
| `messages` | `store.Entry`: chat, sequence, author (`store.User` or a bot id), turn id, `Meta` (mentions, the quote replied to, `From` and `Hops` for `message_bot`, the attached `Files`), the `agent.Message` as JSON, created |
| `routines` | `store.Routine` (`routines.md`) |
| `script_store` | codemode's `store`/`load` values by chat, bot and key |
| `claude_sessions` | the Claude Code session a bot's next task continues, by chat, bot and folder (`ClaudeSession`, `SetClaudeSession`; docs/claude-code.md) |
| `settings` | key and value: the default provider, the desktop's preferences and Always Allow rules, `seeded` |

API keys sit in the providers table in plain text, as the app's config file would hold them, until a keychain is worth it.

`Previews()` gives each chat's last words for a list, from its newest twelve user and assistant messages: the newest with text (collapsed to one line, at most 160 characters) or attached files (their names), past failed and stopped turns, notes for the bots and compaction summaries; a routine's run shows its name.

## Rules

- `CreateBot` makes the bot's DM and workspace in the same step: there is exactly one DM per bot, whose owner is the bot. A DM never gains or loses members.
- A group holds one to `MaxMembers` (six) bots; `CreateGroup` makes the first the owner. `SetOwner` takes only a member. `RemoveMember` passes the owner's seat to the first remaining member, and removes a group left empty.
- `DeleteBot` removes the bot's DM and its seat in every group by the same rules; the workspace folder stays. `DeleteChat` removes a group only.
- `Append` gives a message the chat's next sequence and touches the chat's `updated`, so `Chats` lists the most recent first. `DeleteTurn` and `DeleteEntry` take out a passed turn. `Since(bot, t, n)` lists a bot's own newest messages across chats, for the recent-work brief.
- `MarkRead` moves a chat's read mark to its newest message; `Unread` counts the bots' finished replies after it (not tool calls, not the user's or the system's).
- `Seed` creates **Chef** from its template (`ChefDescription`: a chief of staff who plans, delegates with `message_bot`, and proposes `create_bot`) when there are no bots and the store was never seeded, so deleting every bot does not bring Chef back.
- **Templates** (`templates.go`): the bots New Bot and `create_bot` start from, each a name, a one-line summary, an icon and a color, a description (its system prompt, naming no one, so a bot named in another language reads true), first messages for its empty chat, and what it needs: Chef (plans and hands out), Scout (`Web`: researches with sources), Writer, Coder (`Folder`, `Commands`), Engineer (`Folder`, `Commands`, `ClaudeCode`: hands coding tasks to Claude Code and checks them), Secretary (routines, briefs), Organizer (`~/Downloads` when there, `Folder`, `Commands`; plans before it moves and never deletes) and Critic. `Template.Bot` is the new bot, its folder kept only when it is there. `GroupTemplates` are groups of them with a way of working: Writing Room (Writer, Critic) and Research Team (Chef, Scout, Writer), whose description names members as `{id}`, filled by `Named` with the bots' names; the first member owns the work.
