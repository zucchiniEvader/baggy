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

From the stream: `text_delta` and `thinking_delta` become events; `signature_delta` fills the thinking part's signature; `input_json_delta` builds a `tool_use` input, emitted as a `tool_call` event at `content_block_stop`. `server_tool_use` (with its streamed input) and every other block type (`*_tool_result`, `redacted_thinking`, unknown) are kept as `PartServer` parts. Usage: `input_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens` from `message_start`; cumulative `output_tokens` and `output_tokens_details.thinking_tokens` from `message_delta`. Cost comes from the catalog (below).

| `stop_reason` | Becomes |
| --- | --- |
| `end_turn`, `stop_sequence`, `pause_turn` | `StopEnd` |
| `max_tokens` | `StopLength` |
| `tool_use` | `StopToolUse` |
| `refusal`, none, anything else | `StopError`, naming the reason |

A request that fails before it streams (connection error, 408, 409, 429, 5xx) is retried twice, waiting `retry-after` or a backoff from `Backoff` (500 ms) doubling to 8 s; a wait above `MaxRetryDelay` (60 s) fails instead. Tests run against `httptest` servers.

## What the adapters share (`provider/internal/wire`)

`wire.Post` sends the request with the retries above (`wire.Retry{Max, Backoff, MaxDelay}`, `DefaultRetry` is 2, 500 ms, 60 s) and turns an HTTP error into `"<status>: <error.message>"`. `wire.ReadSSE` reads server-sent events. `wire.Fail` makes the `StopError`/`StopAborted` message. `wire.Args` turns streamed tool arguments into a part's `Args`: empty is `{}`, and text that is not JSON (local models emit it) is kept as a JSON string, so the transcript always marshals and argument validation gives the model an error result. `wire.CallID` reduces a call id to `[a-zA-Z0-9_-]{1,64}`, which every API takes.

## OpenAI Chat Completions (`provider/openai`, `Chat`)

`openai.NewChat(id, base, apiKey, model)` speaks `POST {base}/chat/completions` with bearer auth, `stream: true` and `stream_options.include_usage`. An empty base is `https://api.openai.com/v1`; any server with the API works (DeepSeek's `https://api.deepseek.com`, OpenRouter, Ollama's `/v1`, vLLM).

| Transcript | Request |
| --- | --- |
| System prompt | A `system` message, when not blank |
| User text | `content` as a string (text parts joined by newlines) |
| User text and images | `content` parts, images as `image_url` `data:` URLs |
| Assistant text, tool calls | `content` (null when there is none) and `tool_calls`; arguments as the JSON text, or the model's original text when it was not JSON |
| Own thinking | `reasoning_content`, only with `ReplayReasoning`; otherwise not sent |
| Server blocks | Not sent |
| Tool results | One `tool` message per result, `(no tool output)` when the text is blank |

`NoImages` swaps images for a note (the shared transform). `MaxTokensField` names the output cap: `max_tokens` by default, `max_completion_tokens` for OpenAI's reasoning models; it is sent only when `MaxTokens` is set. A thinking level of low, medium or high is `reasoning_effort`; default and off send nothing.

From the stream: `content` deltas are text, `reasoning_content` or `reasoning` deltas thinking (an unsigned thinking part first), `tool_calls` deltas collect by `index` (id and name arrive once), and each call is emitted as a `tool_call` event at the end. Usage comes in a last chunk with no choices: `prompt_cache_hit_tokens` (DeepSeek) or `prompt_tokens_details.cached_tokens` is `CacheRead` and is taken off `prompt_tokens` for `Input`; `completion_tokens_details.reasoning_tokens` is `Reasoning`. An `error` object in a chunk ends the call with its message.

| `finish_reason` | Becomes |
| --- | --- |
| `length` | `StopLength` |
| `tool_calls`, `function_call`, or any reason when the reply has calls | `StopToolUse` |
| `content_filter` | `StopError` |
| anything else | `StopEnd` |
| none before `[DONE]` or the end | `StopError` |

`GOBOT_API=chat GOBOT_BASE_URL=https://api.deepseek.com GOBOT_API_KEY=… go run ./cmd/smoke deepseek-chat` has passed against DeepSeek, and so has `deepseek-reasoner` with `ReplayReasoning` on and off (DeepSeek takes both).

## OpenAI Responses (`provider/openai`, `Responses`)

`openai.NewResponses(id, base, apiKey, model)` speaks `POST {base}/responses` with bearer auth, stateless: `store: false`, the whole transcript as `input` each time.

| Transcript | Request |
| --- | --- |
| System prompt | `instructions`, when not blank |
| User text and images | A `user` `message` of `input_text` and `input_image` (`data:` URL) parts |
| Assistant text | An `assistant` `message` of `output_text` per run of text, in order with the items below |
| Tool calls | A `function_call` item with `call_id`, `name`, `arguments` (no `id`) |
| Own reasoning | The reasoning item verbatim, only with `EncryptedReasoning` and when it has `encrypted_content` |
| Tool results | One `function_call_output` per result, `(no tool output)` when blank |
| Tools | `function` tools with `strict: false`, `tool_choice: auto`, `parallel_tool_calls: true` |

`MaxTokens` is `max_output_tokens`. A thinking level of low, medium or high is `reasoning: {effort, summary: "auto"}` (the summary is what streams as thinking); default and off send nothing, since a model without reasoning refuses the field. `EncryptedReasoning` adds `include: ["reasoning.encrypted_content"]`; the finished reasoning item is kept as the thinking part's `Raw`, its `encrypted_content` as `Signature`, so it replays to the same model only. It is off by default because not every Responses server takes `include`.

From the stream, item by item in `output_item.added` order: `output_text.delta` is text, `reasoning_summary_text.delta` and `reasoning_text.delta` thinking (summary parts separated by a blank line), `function_call_arguments.delta` a call's arguments (the `arguments` of `output_item.done` when no delta came). `response.completed` ends the call with `StopEnd`, or `StopToolUse` when the reply has calls, or `StopLength` when its status is `incomplete`; `response.incomplete` is `StopLength`; `response.failed` and `error` are `StopError` with their message; a stream that ends before any of them is `StopError`. Usage: `input_tokens_details.cached_tokens` is `CacheRead`, taken off `input_tokens`; `output_tokens_details.reasoning_tokens` is `Reasoning`.

This adapter is tested against `httptest` servers only: it has not been run against a live Responses server, and encrypted reasoning replay is unchecked.

## Model catalog (`provider/catalog`)

`catalog.json` is a models.dev snapshot (embedded) of the models the adapters offer: context window, output cap, reasoning, images, rates in dollars per million tokens (input, output, cache read, cache write), and long-context tiers. `catalog.Find(id)` ignores a gateway's `vendor/` prefix and finds a dated id's base entry (`claude-haiku-4-5-20251001`). `Model.Cost(usage)` prices every token at the tier the request's input lands in; `provider.Model` stamps it on each reply's `Usage.Cost` (unknown models cost 0). `catalog.Window` (128k when unknown) sizes compaction, and `catalog.Images` (true when unknown) sets each adapter's `NoImages` in `connect.New`; a provider's `no_images` option marks a text-only model the catalog lacks, such as `deepseek-chat`.

## Choosing a provider (`provider/connect`)

`connect.New(Config{API, ID, BaseURL, APIKey, Model, ReplayReasoning, EncryptedReasoning})` builds the adapter for `API`: `anthropic` (or empty), `chat`, or `responses`. `connect.FromEnv(defaultModel)` fills a Config from `GOBOT_API`, `GOBOT_PROVIDER`, `GOBOT_BASE_URL`, `GOBOT_API_KEY`, `GOBOT_MODEL`, `GOBOT_REPLAY_REASONING=1` and `GOBOT_ENCRYPTED_REASONING=1`; for the Anthropic API the key and base URL fall back to `ANTHROPIC_API_KEY` and `ANTHROPIC_BASE_URL`. `cmd/smoke` and the desktop app use it until providers are stored settings.

## Model bridge (`provider.Model`)

`provider.Model{Provider, MaxTokens, Thinking}` implements `agent.Model`, so `agent.Run` drives any provider. Each call builds a `Request` from the system prompt, transcript and tools (`tool.Tool` becomes a `ToolSpec` with its schema), and marks the whole transcript as a cache point, since the loop's next call sends it again. `text_delta` and `thinking_delta` stream events are forwarded as `agent.Event`s of the same kinds; `tool_call` events are not (the final message carries the calls); the `done` message is returned. A stream that ends without `done` yields a `StopError` message, or `StopAborted` when the context is cancelled.

`go run ./cmd/room [model]` runs one three-bot group exchange (Chef, Scout, Critic) the same way; it has been run against DeepSeek over Chat Completions. `go run ./cmd/smoke [model]` runs a multi-tool task (see `tools.md`) against a real API chosen by `connect.FromEnv`; `GOBOT_THINKING` sets the thinking level. It has been run against DeepSeek's endpoint (`ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic`, model `deepseek-chat`): streamed text, parallel and sequential tool calls, cache reads; and with `deepseek-reasoner` and `GOBOT_THINKING=high`, which streamed three signed thinking blocks and sent two back in the turn's later calls (smoke prints `thinking`, `signed` and `replayed`). Anthropic's own API has not been run: there is no key here.

## Transcript transform (`agent.Transform`)

Every adapter runs the transcript through `agent.Transform(msgs, TransformOptions{Provider, Model, SupportsImages, NormalizeToolCallID})` before converting it; the input is not modified. A message is "own" when its `Provider` and `Model` equal the target's.

- Thinking: own and signed is kept; empty is dropped; own and unsigned is kept (the adapter decides how to send it); another model's becomes a text part.
- `PartServer` blocks are kept only when own.
- Failed turns (`StopError`, `StopAborted`) are left out.
- Every tool call gets a result before the next assistant message, the next user message, or the end; a missing one is an error result "No result provided", in one `RoleTool` message.
- With `SupportsImages` false, each run of images becomes one note (`NonVisionUserImage`, or `NonVisionToolImage` in a tool message).
- Another model's tool call ids go through `NormalizeToolCallID` (when set) and their results follow. The Anthropic adapter leaves this nil because it reduces every id itself.
- A request carries only its latest images: at most `MaxRequestImages` (20) and `MaxRequestImageBytes` (16 MB of base64). Older ones become `OlderImageOmitted`. Past the count they go ten at a time, so the same ones stay out from one request to the next and the cached prefix breaks once in ten images; past the bytes, only as many as must, never the newest.

## Bound thinking (`anthropic.DropBoundThinking`)

Opus 5.5, Fable 5.1 and Sonnet 5.5 (`anthropic.Bound`, dated ids too) bind a thinking block to the system prompt, the tools and the messages before it, and refuse one replayed after any of them changed; a bot's system prompt does change between turns (a new teammate, a memory update). The adapter keeps on each thinking part it streams a key of its request's system prompt and tools (`Part.Raw` `{"ctx": …}`, never sent). Before converting a request for a bound model, it drops every thinking block when any signed one carries another key or none, and keeps them all otherwise; dropping only some would change what the rest were bound to. A compaction drops the thinking of what it keeps, since that now follows a summary (`team.md`).

`Transform` also returns `origins`, the index in the input each output message comes from (a made-up result takes the index of the message it follows). `agent.PrefixLen(origins, n)` turns a prefix length of the original transcript, such as a `Request.CachePoints` entry, into the same prefix of the transformed one.

## Not yet

A live check of the Responses adapter and of Anthropic's own API (both need a key this machine lacks), tool result images, scaling an image down before it goes.
