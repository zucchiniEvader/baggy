# Providers

A provider turns a `provider.Request` into a stream of `provider.Event`s for one assistant message and never fails the call: an error ends the stream with a `done` event whose message has `StopError` (the error text is its text part) or `StopAborted` (the context was cancelled).

## Anthropic Messages (`provider/anthropic`)

`anthropic.New(id, base, apiKey, model)` speaks `POST {base}/v1/messages` (`{base}/messages` when base ends in `/v1`) with `x-api-key` and `anthropic-version: 2023-06-01`. An empty base is Anthropic's; any server with the same API (DeepSeek's `/anthropic`, a proxy) works.

| Transcript | Request |
| --- | --- |
| System prompt | `system`, when not blank, marked `cache_control: ephemeral` |
| User text and images | `text` and base64 `image` blocks |
| Assistant text, tool calls | `text` and `tool_use` blocks; call ids reduced to `[a-zA-Z0-9_-]{1,64}` |
| Thinking | `thinking` with its `signature`; without one (cut off) it goes as text |
| Server blocks (`PartServer`) | `Raw`, verbatim |
| Tool results (`RoleTool`) | `tool_result` blocks in a user message, with `is_error` |

Consecutive same-role messages merge, so a tool result follows its call; assistant messages left empty are dropped. At most four `cache_control` marks go on a request: the system prompt, the last user block, then the newest `CachePoints`. `NoCache` turns them off. `ServerTools` are declared before the function tools; function tools carry `eager_input_streaming`.

Thinking: `Default` sends nothing; otherwise `thinking: {type: adaptive}` with `output_config.effort`, `Off` is `{type: disabled}`. `Budget` selects `{type: enabled, budget_tokens}` (1024, 2048, 8192 for low, medium, high; `max_tokens` is raised to leave 1024 for the answer) for Haiku 4.5 and earlier.

From the stream: `text_delta` and `thinking_delta` become events; `signature_delta` fills the thinking part's signature; `input_json_delta` builds a `tool_use` input, emitted as a `tool_call` event at `content_block_stop`. `server_tool_use` (with its streamed input) and every other block type (`*_tool_result`, `redacted_thinking`, unknown) are kept as `PartServer` parts. Usage: `input_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens` from `message_start`; cumulative `output_tokens` and `output_tokens_details.thinking_tokens` from `message_delta`. Cost is not computed yet.

| `stop_reason` | Becomes |
| --- | --- |
| `end_turn`, `stop_sequence`, `pause_turn` | `StopEnd` |
| `max_tokens` | `StopLength` |
| `tool_use` | `StopToolUse` |
| `refusal`, none, anything else | `StopError`, naming the reason |

A request that fails before it streams (connection error, 408, 409, 429, 5xx) is retried twice, waiting `retry-after` or a backoff from `Backoff` (500 ms) doubling to 8 s; a wait above `MaxRetryDelay` (60 s) fails instead. Tests run against `httptest` servers.

## Model bridge (`provider.Model`)

`provider.Model{Provider, MaxTokens, Thinking}` implements `agent.Model`, so `agent.Run` drives any provider. Each call builds a `Request` from the system prompt, transcript and tools (`tool.Tool` becomes a `ToolSpec` with its schema), and marks the whole transcript as a cache point, since the loop's next call sends it again. `text_delta` and `thinking_delta` stream events are forwarded as `agent.Event`s of the same kinds; `tool_call` events are not (the final message carries the calls); the `done` message is returned. A stream that ends without `done` yields a `StopError` message, or `StopAborted` when the context is cancelled.

`go run ./cmd/smoke [model]` runs a multi-tool task (see `tools.md`) against a real Anthropic-protocol API (`ANTHROPIC_API_KEY`, optional `ANTHROPIC_BASE_URL`). It has been run against DeepSeek's endpoint (`ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic`, model `deepseek-chat`): streamed text, parallel and sequential tool calls, cache reads. Thinking signature replay is not covered by that run.

## Transcript transform (`agent.Transform`)

Every adapter runs the transcript through `agent.Transform(msgs, TransformOptions{Provider, Model, SupportsImages, NormalizeToolCallID})` before converting it; the input is not modified. A message is "own" when its `Provider` and `Model` equal the target's.

- Thinking: own and signed is kept; empty is dropped; own and unsigned is kept (the adapter decides how to send it); another model's becomes a text part.
- `PartServer` blocks are kept only when own.
- Failed turns (`StopError`, `StopAborted`) are left out.
- Every tool call gets a result before the next assistant message, the next user message, or the end; a missing one is an error result "No result provided", in one `RoleTool` message.
- With `SupportsImages` false, each run of images becomes one note (`NonVisionUserImage`, or `NonVisionToolImage` in a tool message).
- Another model's tool call ids go through `NormalizeToolCallID` (when set) and their results follow. The Anthropic adapter leaves this nil because it reduces every id itself.

`Transform` also returns `origins`, the index in the input each output message comes from (a made-up result takes the index of the message it follows). `agent.PrefixLen(origins, n)` turns a prefix length of the original transcript, such as a `Request.CachePoints` entry, into the same prefix of the transformed one.

## Not yet

Limiting the images one request carries (`../lorca`'s `MAX_REQUEST_IMAGES`), thinking bound to the system prompt and tools on newer models (`drop_bound_thinking` in `../lorca`), tool result images, the model catalog and cost.
