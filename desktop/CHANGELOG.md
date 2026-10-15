# Changelog

## 0.1.2

- Bots can hand coding tasks to your own Claude Code, signed in to your plan: turn on "Hand coding tasks to Claude Code" in a bot's sheet, under its folder. Claude Code works in the bot's folder in the background, its commands go through your Commands approvals, and the bot checks its report when it is done. The Engineer template has it on.
- Running tasks shows each Claude Code run: the task, its latest steps, what it said last, and Stop.

## 0.1.1

- New Bot starts from templates: Chef, Scout (research with sources), Writer, Coder, Secretary, Organizer and Critic, each with its own first messages to try. Two group templates set a team up at once: Writing Room and Research Team.
- Chef can create teammates from the same templates.
- Copy a reply as plain text or as Markdown.

## 0.1.0

The first release of Baggy: a desktop assistant whose bots chat with you one to one or together in groups.

- Bots with their own description, model, avatar, memory and working folder; Chef leads, and creates teammates when you agree.
- Group chats where members take turns, reply or pass, and hand work to each other.
- Providers on the Anthropic Messages API, OpenAI Chat Completions and Responses, with the server's model list to pick from.
- Tools for files, search and shell commands in real terminals, with approval or auto-review; MCP servers through codemode scripts, with their resources.
- Routines on a schedule, with checks that only start a turn when something is new.
- Attachments of any file, pictures fitted for the model.
- Updates from within the app.
