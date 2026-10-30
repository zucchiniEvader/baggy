# Changelog

## 0.1.4

- Replies render with MyGo's own Markdown view: code blocks with syntax colors and a Copy button, proper tables, quotes and task lists.
- A reply streams in smoothly, a word at a time, and half-written formatting already shows as it will end up.
- Built on MyGo 0.7.3.

## 0.1.3

- A bot's reply now shows on its own, its thinking, searches and tool calls folded under one line above it. While the bot works, the line says what it is doing now; once done, what it did. Click it to see every step.
- Group chats ask only the members a message calls for, rather than every member in turn: those you mention answer first, then the group's owner picks who else should speak. Quicker, and fewer "pass" turns.
- When a bot messages a teammate, the teammate's answer comes back to the chat it was asked from.
- Bots can delegate: hand up to six tasks to workers that run side by side, each with a fresh context, and get their reports back together. Good for research, going through many files, or comparing options. Running tasks shows each task and lets you stop it.

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
