# The loop

`agent.Run(ctx, model, msgs, Options) []Message` runs one agent run over a transcript you provide and returns the messages it added (replies, tool results, injected queue messages). It keeps no state between runs; the caller owns the transcript and appends the result. The loop's only dependency is `agent.Model`; `provider.Model` adapts a `provider.Provider` to it.

## Steps

1. `turn_start`; messages from `Options.Steering` are taken (queued before the run, they follow the prompt).
2. `Options.Compact`, when set, sees the messages the call would send; when it returns a replacement (a summary and what it kept), this call and the rest of the run send that instead, followed by the messages the run adds from then on. Then one model call: `message_start` before it, `message_end` after, `text_delta` and `thinking_delta` while it streams.
3. A reply that stopped for `StopError` or `StopAborted`, or a cancelled `ctx`, ends the run. Queues are not read, so what they hold waits for the next run.
4. `StopToolUse`: the reply's calls run in parallel (`BeforeToolCall`, schema validation, `Execute`, `AfterToolCall`), one `tool_start` and `tool_end` each, then one `RoleTool` message with the results. Steering is read and the loop goes to step 2.
5. Otherwise the model would stop: steering is read, and when it is empty, follow-up. Messages found continue the run (step 2); none ends it with `turn_end`.

## Events

`Options.Emit` gets every `agent.Event`, one at a time and in order, even while tools run in parallel, so it needs no locking of its own. A slow `Emit` slows the run. Persist on `message_end`: it fires for every message the run adds, tool results and injected messages included.

| Kind | Fields |
| --- | --- |
| `turn_start`, `turn_end` | |
| `message_start` | `Message` for an injected message; nil for a model reply (the stream is starting) |
| `text_delta`, `thinking_delta` | `Delta` |
| `message_end` | `Message`, final |
| `tool_start` | `Call` (id, name, arguments) |
| `tool_end` | `Call`, `Result` |

## Steering and follow-ups

`agent.Queue` (`NewQueue(mode)`, `Push`, `Drain`, `Len`, `Clear`) is safe for concurrent use. Keep a pointer where input arrives and put it in `Options.Steering` or `Options.FollowUp`.

- Steering: a message for the model's next call. It is read before the first call and after each batch of tool results. It waits for the step under way (the reply and its tool calls); interrupting a step is not built yet.
- Follow-up: a message for when the model would otherwise stop.
- `QueueOneAtATime` (the zero value) drains the oldest message per read, `QueueAll` everything.
- A queue outlives runs: a message pushed while no run is active is taken by the next one.

Cancel a run with its `ctx`; the call in flight ends with `StopAborted`.

## Token estimates

`agent.EstimateTokens(msgs)` guesses a request's size: the `Usage.Context()` the newest reply reported for everything up to it, plus about a token per four characters of what came after (an image counts `ImageTokens`, 1600). `MessageTokens` and `TextTokens` are the same guess for one message or a string. Compaction (`team.md`) decides with it.

## Not yet

Interrupting a step for a steering message, retry of a failed call, an `Agent` that owns the transcript.
