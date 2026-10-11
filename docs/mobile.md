# Phone

Baggy on the phone is a companion of the desktop app: the bots run on the computer, and the phone reads and writes their chats over the local network. There is no relay; away from home, the phone and the computer reach each other on Tailscale or a similar network, whose addresses the desktop lists too. The phone app is iOS first (`mobile/`, Expo).

## The server (`remote`)

`remote.Server` serves a store and its `team.Team` over HTTP. The desktop makes one at start (`desktop/phone.go`), hands it every `team.Event` (`Publish`, which does nothing while no phone is connected), and listens on port 7841 (`DefaultPort`, the `phone.port` setting names another) on every interface while Settings › Phone › Connect a phone is on (`pref.phone`). Every route wants the token, as `Authorization: Bearer …` or, for the WebSocket and pictures, `?token=`; anything else is a 401.

| Route | Answers |
| --- | --- |
| `GET /v1/hello` | `{app, name, version}`: the computer's name and Baggy's version |
| `GET /v1/snapshot` | the bots, chats (newest first), `Store.Previews`, unread counts and the busy chats |
| `GET /v1/chats/{id}/messages?after=N` | the chat's entries after seq N |
| `POST /v1/chats/{id}/messages` | `{text, mentions, reply_to}` sends as the user (`team.Send`); a reply quotes the entry of that seq |
| `POST /v1/chats/{id}/stop` | `team.Stop` |
| `POST /v1/chats/{id}/read` | `Store.MarkRead`; the desktop recounts its unread badges (`Changed`), and a `changed` event tells the phones |
| `GET /v1/avatars/{name}` | a bot's picture |
| `GET /v1/events` | a WebSocket of events, one JSON text message each, a ping every 20 seconds |

Entries reach the phone slim (`Slim`): no image bytes, thinking signatures or vendor blocks, which only a model reads. An event is a `team.Event` as JSON (`Wire`): `kind`, `chat_id`, `bot_id`, `turn`, `since`, `entry`, `error`, and for `agent` the streamed event without tool output (`kind`, `delta`, `message`, and a tool call's `tool`, `call_id`, `is_error`); `tool_update` is not sent. A phone too slow to take its events (512 waiting) is dropped and reconnects.

The phone sees what the desktop sees as it happens: messages from either side, replies as they stream, routines and messages between bots, through the team's events. What the user changes in the desktop's own window goes past the team, so the desktop publishes a `changed` event for it (`app.edited`, `app.changed`): bots and groups made, edited or deleted, members and owners, pins, and a chat read there, which clears its count on the phone. A `changed` event has the phone load the snapshot again.

## Pairing

The token is 32 random bytes (`NewToken`) kept in the store's settings (`phone.token`), made when the switch is first turned on. The pane shows a QR code of `baggy://pair?host=…&port=…&token=…&name=…` (`PairURL`) for one of the computer's IPv4 addresses (`Addresses`: private ranges first, then others such as Tailscale's 100.64/10), a pop-up choosing the address when there are several. Reset pairing makes a new token and restarts the server, so every paired phone is disconnected and refused until it scans again. The traffic is plain HTTP: the token and the chats cross the network as they are, which a home network or Tailscale's encryption covers and a shared Wi-Fi does not.

## The app (`mobile/`)

Expo (SDK 57, expo-router, TypeScript), iOS first. `bun install`, then `bunx expo run:ios` (CocoaPods and Xcode; `DEVELOPER_DIR` names a beta Xcode); `bun run typecheck` and `bun test src` check it. `ios/` is made by prebuild and not kept.

- **Pairing** (`app/pair.tsx`): Scan Code opens VisionKit's scanner (`CameraView.launchScanner`), Paste Code reads the clipboard, and a `baggy://pair?…` link opened from the Camera app lands there with its fields. `pair` (`src/store.ts`) asks `/v1/hello` with the token first and keeps the pairing in the keychain (`expo-secure-store`) only when the computer answers.
- **The store** (`src/store.ts`, zustand) holds the snapshot, each opened chat's entries (`rooms`), the replies streaming (`live`, by turn, replaced by the stored entry when it comes), the bot taking its turn and the tool it runs. The WebSocket keeps it current; it reconnects with backoff up to 15 seconds and again whenever the app comes to the foreground (iOS kills the socket while the app is suspended), then loads the snapshot and what each open chat missed. A refused connection reads as "Pair again".
- **Chats** (`app/index.tsx`): a large-title list with search, pinned chats first, each row a bot's avatar (its picture, else its initial in white on its color, picked as `desktop/avatar.go` picks it) or a group's first two members, the name, when its last words came, those words ("Name: …" in a group) or "Working…", and an unread capsule. A banner above says "Connecting…", "Can't connect" (a tap tries again) or "Pair again".
- **A chat** (`app/chat/[id].tsx`, rows by `src/rows.ts`): the user's messages in a neutral bubble on the right with the quote they answer and their files' names; a bot's replies as text with fenced code in monospaced blocks, its name at the start of its turn in a group; a run of tool calls in a turn as one muted "Used shell, read_file" line, their results hidden; a stopped or failed reply, summaries and system notes as muted lines; the streaming reply as it comes and a working row ("Working…", "Chef is working…", "Running shell…"). Messages run up from the composer as they come, and a chat too short to fill the screen starts at the top. The composer is a pill with a round button: Send, or Stop while the chat works and the field is empty. Opening a chat marks it read.
- **Settings** (`app/settings.tsx`, a sheet from the list's gear): the computer, its address, the connection, and Unpair.

Chinese follows the phone's language (`src/i18n.ts`).

## Not yet

Approvals: under Ask every time or Auto-review, a command a phone message leads to waits for Allow on the computer. Notifications need a push service the computer cannot be without a relay. Attachments, markdown beyond code blocks, creating and editing bots and groups, `@` mentions and replies from the phone, and Android.
