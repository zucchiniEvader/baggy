# The loop

`agent.Run(ctx, model, msgs, Options) []Message` runs one agent run over a transcript you provide and returns the messages it added (replies, tool results, injected queue messages). It keeps no state between runs; the caller owns the transcript and appends the result. The loop's only dependency is `agent.Model`; `provider.Model` adapts a `provider.Provider` to it.

## Steps

1. `turn_start`; messages from `Options.Steering` are taken (queued before the run, they follow the prompt).
2. `Options.Compact`, when set, sees the messages the call would send; when it returns a replacement (a summary and what it kept), this call and the rest of the run send that instead, followed by the messages the run adds from then on. Then one model call: `message_start` before it, `message_end` after, `text_delta` and `thinking_delta` while it streams.
3. A reply that stopped for `StopError` or `StopAborted`, or a cancelled `ctx`, ends the run. Queues are not read, so what they hold waits for the next run.
4. `StopToolUse`: the reply's calls run in parallel (`BeforeToolCall`, schema validation, `Execute`, `AfterToolCall`), one `tool_start` and `tool_end` each, then one `RoleTool` message with the results. Steering is read and the loop goes to step 2.
5. `StopPause` (the server paused a long turn of server tools): the reply is sent back as it is, at most five times in a row.
6. Otherwise the model would stop: steering is read, and when it is empty, follow-up. Messages found continue the run (step 2); none ends it with `turn_end`.

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
| `retry` | `Delta`: why the reply streaming failed; it is asked again |
| `server_tool` | `Call`: a server tool's block (web search or fetch, call or result), as it completes; `provider.DescribeServer` reads it |

## Steering and follow-ups

`agent.Queue` (`NewQueue(mode)`, `Push`, `Drain`, `Len`, `Clear`) is safe for concurrent use. Keep a pointer where input arrives and put it in `Options.Steering` or `Options.FollowUp`.

- Steering: a message for the model's next call. It is read before the first call and after each batch of tool results. It waits for the step under way (the reply and its tool calls), unless the step is interrupted (below).
- Follow-up: a message for when the model would otherwise stop.
- `QueueOneAtATime` (the zero value) drains the oldest message per read, `QueueAll` everything.
- A queue outlives runs: a message pushed while no run is active is taken by the next one.

Cancel a run with its `ctx`; the call in flight ends with `StopAborted`.

## Interrupting a step

`Options.Interrupt` takes an `agent.NewInterrupt()`; after queueing a steering message, `Interrupt()` cuts the step under way short and reports whether there was one (false between steps, where the queue is read anyway). Each step (a model call and the tools it asks for) runs under a child of the run's context, and only that one is cancelled:

- A reply interrupted while it streams keeps its text as said, as a `StopEnd` message without its thinking or tool calls (one with no text is ended as `StopAborted` and not kept). The loop reads steering and calls the model again, or ends when nothing is queued.
- Running tools get the cancelled context; a call that returns nothing then reads `Interrupted`. The results go in as usual and the loop goes on.

Cancelling the run's context still ends the run.

## Retry

`Options.Retry` (`agent.DefaultRetry`: three retries from a second, doubling, capped at a minute) asks a model call again when its reply failed for a transient reason: `IsRetryable` matches overloaded, rate limits, 5xx, a dropped connection or a stream that ended early, and never exhausted quota or billing. The failed reply is not kept; the loop emits `retry` with the reason (Delta) and then a new `message_start`, so a UI drops what streamed. The adapters already retry a request that fails before it streams (`providers.md`); this covers the rest. A reply saying the prompt no longer fits (`IsContextOverflow`) calls `Compact` with `force` set, once, and goes again on its replacement.

## Token estimates

`agent.EstimateTokens(msgs)` guesses a request's size: the `Usage.Context()` the newest reply reported for everything up to it, plus about a token per four characters of what came after (an image counts `ImageTokens`, 1600). `MessageTokens` and `TextTokens` are the same guess for one message or a string. Compaction (`team.md`) decides with it.

## Not yet

An `Agent` that owns the transcript.
