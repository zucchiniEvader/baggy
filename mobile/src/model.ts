// The desktop's types as its remote API sends them (remote/remote.go, store, agent).

export const User = "user";
export const System = "system";

export interface Bot {
  id: string;
  name: string;
  description: string;
  icon: string;
  color: string;
  avatar: string; // a file the server's /v1/avatars serves; "" for the icon
}

export interface Chat {
  id: string;
  kind: "dm" | "group";
  title: string;
  description: string;
  owner: string;
  members: string[];
  updated: number;
  read_seq: number;
  pinned: number;
}

export interface Preview {
  Author: string;
  Text: string;
  At: number;
}

export interface Snapshot {
  bots: Bot[];
  chats: Chat[];
  previews: Record<string, Preview>;
  unread: Record<string, number>;
  busy: string[];
}

export interface Part {
  kind: "text" | "thinking" | "image" | "tool_call" | "tool_result" | "server";
  text?: string;
  media_type?: string;
  call_id?: string;
  name?: string;
  is_error?: boolean;
}

export interface Message {
  id: string;
  role: "user" | "assistant" | "tool";
  parts: Part[] | null;
  stop_reason?: string;
  kind?: string;
}

export interface FileRef {
  id: string;
  name: string;
  size: number;
  type?: string;
  image?: boolean;
}

export interface Entry {
  chat_id: string;
  seq: number;
  author: string;
  turn: string;
  meta: {
    mentions?: string[];
    reply_to?: { seq: number; author: string; text: string };
    from?: string;
    routine_name?: string;
    files?: FileRef[];
  };
  msg: Message;
  created: number;
}

export interface AgentEvent {
  kind: string;
  delta?: string;
  message?: Message;
  tool?: string;
  call_id?: string;
  is_error?: boolean;
}

export interface Event {
  kind: "run_start" | "run_end" | "turn" | "agent" | "entry" | "removed" | "pass" | "changed" | "error";
  chat_id: string;
  bot_id?: string;
  turn?: string;
  since?: number;
  agent?: AgentEvent;
  entry?: Entry;
  error?: string;
}

/** The text a message shows: its text parts, joined. */
export function textOf(m: Message): string {
  return (m.parts ?? [])
    .filter((p) => p.kind === "text")
    .map((p) => p.text ?? "")
    .join("")
    .trim();
}

/** The tools a message called, by name. */
export function toolsOf(m: Message): string[] {
  return (m.parts ?? []).filter((p) => p.kind === "tool_call").map((p) => p.name ?? "");
}

const botColors: Record<string, string> = {
  blue: "#007aff",
  indigo: "#5856d6",
  purple: "#af52de",
  pink: "#ff2d55",
  red: "#ff3b30",
  orange: "#ff9500",
  green: "#34c759",
  teal: "#30b0c7",
  brown: "#a2845e",
  graphite: "#8e8e93",
};
const colorNames = Object.keys(botColors);

/** A bot's avatar color: the one it was given, else one its id picks (FNV-1a, as the desktop). */
export function colorOf(b: Pick<Bot, "id" | "color">): string {
  if (botColors[b.color]) return botColors[b.color];
  let h = 0x811c9dc5;
  for (const byte of new TextEncoder().encode(b.id)) {
    h ^= byte;
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return botColors[colorNames[h % colorNames.length]];
}

/** A chat's name: a DM's bot, else the group's title. */
export function chatTitle(c: Chat, bots: Record<string, Bot>): string {
  return c.kind === "dm" ? (bots[c.members[0]]?.name ?? "") : c.title;
}
