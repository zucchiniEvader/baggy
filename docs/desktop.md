# Desktop app

`desktop/` is a separate Go module (`github.com/spike/gobot/desktop`, `replace`d onto the repository root) so the core keeps no dependencies. It is a MyGo 0.3.6 app of native UI: the window is drawn by `github.com/egoist/mygo/ui` from Go, with no web page. `go.mod` pins the `mygo` CLI as a Go tool.

```
cd desktop
ANTHROPIC_API_KEY=... go tool mygo dev     # builds, runs, relaunches on a change
go test ./...                              # views run in MyGo's offscreen Tester
GOBOT_RENDER=<folder> go test -run TestRender ./...   # writes chat-light.png and chat-dark.png
```

The provider comes from the environment through `connect.FromEnv` (see `providers.md`): `GOBOT_API` picks the protocol (`anthropic`, `chat` or `responses`), `GOBOT_MODEL` the model (default `claude-sonnet-5-5`), `GOBOT_BASE_URL` and `GOBOT_API_KEY` the server; for the Anthropic API, `ANTHROPIC_API_KEY` and `ANTHROPIC_BASE_URL` work too (DeepSeek's `https://api.deepseek.com/anthropic`). Tools are `shell`, `read_file` and `write_file` in the directory the app starts in. There is no settings screen yet.

## State and threads

MyGo views are immediate: `chat.view` builds the window every frame from the `chat` struct, and the struct belongs to the main thread. The run goroutine reaches it only through `chat.post`, the window's `Update`, which runs a function on the main thread and draws a frame. `agent.Run`'s `Emit` is serialized, and `post` queues in order, so events are applied in the order they happened. Tests give `post` a channel and run its functions on the test goroutine.

## Events to rows

`chat.apply(agent.Event)` folds events into `rows`:

| Event | Effect |
| --- | --- |
| `message_start` without a message | a new assistant row starts streaming |
| `text_delta`, `thinking_delta` | appended to the streaming row |
| `tool_start` | a tool row, "running" |
| `tool_end` | its result and status, "done" or "failed" |
| `message_end` | the message joins the transcript (`chat.msgs`); an assistant row takes the final text, is marked failed or stopped, and is dropped when the reply was only tool calls |

An injected steering message arrives as a user `message_end` and becomes a user row then, not when typed.

## Sending

Enter sends, Shift+Enter starts a line (not while an input method composes). Idle, the draft is a prompt: it joins the transcript and `agent.Run` starts in a goroutine over a copy of it. Running, the draft goes to the steering queue and the button reads "Steer" with the count queued. Steering that arrives after the run's last read is drained in `finish` and starts the next run. Stop cancels the run's context and clears the queue; the interrupted reply shows "Stopped".

## Not yet

Markdown in replies, a settings screen (provider, model, key), several chats and bots, a persisted transcript, window state, the tray, Windows testing.
