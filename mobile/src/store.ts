// The phone's copy of the desktop's chats: a snapshot for the list, each opened chat's
// messages, and what its bots are doing, kept current by the events WebSocket.

import { AppState } from "react-native";
import * as SecureStore from "expo-secure-store";
import { create } from "zustand";
import { User, textOf, type Bot, type Chat, type Entry, type Event, type Preview, type Snapshot } from "./model";
import { baseURL, parsePairing, type Pairing } from "./pairing";

/** A reply as it streams, before the desktop stores it. */
export interface Live {
  turn: string;
  bot: string;
  text: string;
}

export interface Room {
  entries: Entry[];
  loaded: boolean;
  live: Live[];
  working?: string; // the bot taking its turn
  tool?: string; // the tool it is running
  retrying?: boolean;
  error?: string;
}

export type Status = "connecting" | "online" | "offline" | "unpaired";

interface State {
  ready: boolean;
  pairing: Pairing | null;
  status: Status;
  bots: Record<string, Bot>;
  chats: Chat[];
  previews: Record<string, Preview>;
  unread: Record<string, number>;
  busy: Record<string, boolean>;
  rooms: Record<string, Room>;
  open: string | null; // the chat on screen
}

const initial: Omit<State, "ready" | "pairing"> = {
  status: "connecting",
  bots: {},
  chats: [],
  previews: {},
  unread: {},
  busy: {},
  rooms: {},
  open: null,
};

export const useStore = create<State>(() => ({ ready: false, pairing: null, ...initial }));
const set = useStore.setState;
const get = useStore.getState;

const emptyRoom: Room = { entries: [], loaded: false, live: [] };

function room(chat: string, fn: (r: Room) => Partial<Room>) {
  set((s) => {
    const r = s.rooms[chat] ?? emptyRoom;
    return { rooms: { ...s.rooms, [chat]: { ...r, ...fn(r) } } };
  });
}

const pairingKey = "baggy.pairing";

class Unauthorized extends Error {}

async function api<T>(method: string, path: string, body?: unknown): Promise<T> {
  const p = get().pairing;
  if (!p) throw new Error("Not paired");
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), 15000);
  try {
    const res = await fetch(baseURL(p) + path, {
      method,
      headers: { Authorization: `Bearer ${p.token}`, "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: ctl.signal,
    });
    const data = await res.json().catch(() => ({}));
    if (res.status === 401) throw new Unauthorized("This phone is no longer paired. Scan the code in Baggy's Settings › Phone again.");
    if (!res.ok) throw new Error(data.error ?? `HTTP ${res.status}`);
    return data as T;
  } catch (e) {
    if (e instanceof Error && e.name === "AbortError") throw new Error("Baggy on your computer did not answer.");
    throw e;
  } finally {
    clearTimeout(timer);
  }
}

/** The URL of a bot's own picture. */
export function avatarURL(name: string): string | undefined {
  const p = get().pairing;
  if (!p || !name) return undefined;
  return `${baseURL(p)}/v1/avatars/${encodeURIComponent(name)}?token=${p.token}`;
}

let socket: WebSocket | null = null;
let retry: ReturnType<typeof setTimeout> | null = null;
let backoff = 1000;
let refreshTimer: ReturnType<typeof setTimeout> | null = null;

async function loadSnapshot() {
  const s = await api<Snapshot>("GET", "/v1/snapshot");
  const bots: Record<string, Bot> = {};
  for (const b of s.bots) bots[b.id] = b;
  const busy: Record<string, boolean> = {};
  for (const id of s.busy) busy[id] = true;
  const open = get().open;
  const unread = { ...s.unread };
  if (open) delete unread[open];
  set({ bots, chats: s.chats, previews: s.previews ?? {}, unread, busy });
}

/** Loads the snapshot again shortly, once for a burst of events. */
function refreshSoon() {
  if (refreshTimer) return;
  refreshTimer = setTimeout(() => {
    refreshTimer = null;
    loadSnapshot().catch(() => {});
  }, 400);
}

function failed(e: unknown) {
  if (e instanceof Unauthorized) {
    set({ status: "unpaired" });
    socket?.close();
    return true;
  }
  return false;
}

function connect() {
  if (retry) clearTimeout(retry);
  retry = null;
  const p = get().pairing;
  if (!p) return;
  socket?.close();
  set({ status: "connecting" });
  const ws = new WebSocket(`${baseURL(p).replace(/^http/, "ws")}/v1/events?token=${p.token}`);
  socket = ws;
  ws.onopen = async () => {
    backoff = 1000;
    try {
      await loadSnapshot();
      set({ status: "online" });
      // What the open chats missed while away.
      for (const id of Object.keys(get().rooms)) void loadMessages(id);
    } catch (e) {
      if (!failed(e)) set({ status: "offline" });
    }
  };
  ws.onmessage = (m) => {
    try {
      apply(JSON.parse(String(m.data)) as Event);
    } catch {}
  };
  ws.onclose = () => {
    if (socket !== ws) return;
    socket = null;
    if (get().status === "unpaired") return;
    // A refused socket says nothing of why: ask the API, which tells an unpaired phone.
    api("GET", "/v1/hello").catch(failed);
    set((s) => (s.status === "unpaired" ? {} : { status: "offline" }));
    retry = setTimeout(connect, backoff);
    backoff = Math.min(backoff * 2, 15000);
  };
}

/** Starts the store: the saved pairing, then the connection. */
export async function start() {
  const saved = await SecureStore.getItemAsync(pairingKey).catch(() => null);
  let pairing: Pairing | null = null;
  try {
    if (saved) pairing = JSON.parse(saved);
  } catch {}
  set({ ready: true, pairing });
  if (pairing) connect();
  AppState.addEventListener("change", (state) => {
    // iOS kills the socket while the app is suspended.
    if (state === "active" && get().pairing && get().status !== "online") connect();
  });
}

/** Pairs with the desktop whose code this is: checks the token, then keeps it. */
export async function pair(code: string) {
  const p = parsePairing(code);
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), 8000);
  try {
    const res = await fetch(`${baseURL(p)}/v1/hello`, { headers: { Authorization: `Bearer ${p.token}` }, signal: ctl.signal });
    if (res.status === 401) throw new Error("The code is out of date. Open Baggy's Settings › Phone on your computer and scan the code shown there.");
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
  } catch (e) {
    if (e instanceof Error && e.name === "AbortError") {
      throw new Error(`Can't reach ${p.name} at ${p.host}:${p.port}. Check that the phone is on the same network and Baggy is running.`);
    }
    throw e;
  } finally {
    clearTimeout(timer);
  }
  await SecureStore.setItemAsync(pairingKey, JSON.stringify(p));
  set({ pairing: p, ...initial });
  connect();
}

export async function unpair() {
  await SecureStore.deleteItemAsync(pairingKey).catch(() => {});
  const ws = socket;
  socket = null;
  ws?.close();
  if (retry) clearTimeout(retry);
  set({ pairing: null, ...initial });
}

export function reconnect() {
  connect();
}

async function loadMessages(chat: string) {
  const r = get().rooms[chat];
  const after = r?.loaded ? (r.entries.at(-1)?.seq ?? 0) : 0;
  const es = await api<Entry[]>("GET", `/v1/chats/${chat}/messages?after=${after}`);
  room(chat, (r) => ({ entries: after ? merge(r.entries, es) : es, loaded: true }));
}

function merge(a: Entry[], b: Entry[]): Entry[] {
  const seen = new Set(a.map((e) => e.seq));
  const out = a.concat(b.filter((e) => !seen.has(e.seq)));
  out.sort((x, y) => x.seq - y.seq);
  return out;
}

/** Shows a chat: its messages loaded, marked read. */
export async function openChat(chat: string) {
  set((s) => {
    const unread = { ...s.unread };
    delete unread[chat];
    return { open: chat, unread };
  });
  try {
    await loadMessages(chat);
    await api("POST", `/v1/chats/${chat}/read`);
  } catch (e) {
    if (!failed(e)) room(chat, () => ({ error: e instanceof Error ? e.message : String(e) }));
  }
}

export function closeChat(chat: string) {
  if (get().open === chat) set({ open: null });
}

export async function send(chat: string, text: string, mentions: string[] = []) {
  room(chat, () => ({ error: undefined }));
  await api("POST", `/v1/chats/${chat}/messages`, { text, mentions });
  if (get().status !== "online") await loadMessages(chat);
}

export async function stop(chat: string) {
  await api("POST", `/v1/chats/${chat}/stop`);
}

function apply(e: Event) {
  const chat = e.chat_id;
  switch (e.kind) {
    case "run_start":
      set((s) => ({ busy: { ...s.busy, [chat]: true } }));
      return;
    case "run_end":
      set((s) => ({ busy: { ...s.busy, [chat]: false } }));
      room(chat, () => ({ live: [], working: undefined, tool: undefined, retrying: false }));
      refreshSoon();
      return;
    case "changed":
      refreshSoon();
      return;
    case "turn":
      room(chat, () => ({ working: e.bot_id, tool: undefined }));
      return;
    case "error":
      room(chat, () => ({ error: e.error }));
      return;
    case "removed":
      room(chat, (r) => ({ entries: r.entries.filter((x) => x.seq !== e.entry?.seq) }));
      return;
    case "entry": {
      const en = e.entry!;
      if (get().rooms[chat]?.loaded) {
        room(chat, (r) => ({
          entries: merge(r.entries, [en]),
          // The stored reply takes the place of the streamed one.
          live: en.author !== User && en.msg.role === "assistant" ? r.live.filter((l) => l.turn !== en.turn) : r.live,
        }));
      }
      const text = textOf(en.msg);
      if (text) set((s) => ({ previews: { ...s.previews, [chat]: { Author: en.author, Text: text.split("\n")[0].slice(0, 160), At: en.created } } }));
      if (get().open === chat) void api("POST", `/v1/chats/${chat}/read`).catch(() => {});
      else if (en.author !== User && text) set((s) => ({ unread: { ...s.unread, [chat]: (s.unread[chat] ?? 0) + 1 } }));
      refreshSoon();
      return;
    }
    case "agent": {
      const a = e.agent!;
      const turn = e.turn ?? "";
      switch (a.kind) {
        case "message_start":
          if (!a.message) room(chat, (r) => ({ retrying: false, live: [...r.live.filter((l) => l.turn !== turn), { turn, bot: e.bot_id ?? "", text: "" }] }));
          return;
        case "text_delta":
          room(chat, (r) => ({ live: r.live.map((l) => (l.turn === turn ? { ...l, text: l.text + (a.delta ?? "") } : l)) }));
          return;
        case "tool_start":
          room(chat, () => ({ tool: a.tool }));
          return;
        case "tool_end":
          room(chat, () => ({ tool: undefined }));
          return;
        case "retry":
          room(chat, () => ({ retrying: true }));
          return;
      }
    }
  }
}
