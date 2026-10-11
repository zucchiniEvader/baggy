import { t } from "./i18n";
import { System, textOf, toolsOf, User, type Bot, type Chat, type Entry } from "./model";
import type { Room } from "./store";

export type Row =
  | { kind: "user"; key: string; entry: Entry }
  | { kind: "bot"; key: string; bot: string; turn: string; name: boolean; text: string; tools: string[]; error?: boolean }
  | { kind: "note"; key: string; text: string }
  | { kind: "working"; key: string; label: string };

/** The rows a chat shows, oldest first: its stored messages, the replies streaming, and who is working. */
export function rowsOf(chat: Chat, room: Room, busy: boolean, bots: Record<string, Bot>): Row[] {
  const rows: Row[] = [];
  const group = chat.kind === "group";
  const lastBot = () => {
    const r = rows.at(-1);
    return r?.kind === "bot" ? r : undefined;
  };
  for (const e of room.entries) {
    const m = e.msg;
    const key = String(e.seq);
    if (m.kind === "compaction") rows.push({ kind: "note", key, text: t("Summarized the earlier conversation") });
    else if (e.author === System) rows.push({ kind: "note", key, text: textOf(m) });
    else if (e.author === User) {
      if (m.role === "user") rows.push({ kind: "user", key, entry: e });
    } else if (m.role === "assistant") {
      const text = textOf(m);
      const tools = toolsOf(m);
      const prev = lastBot();
      if (!text && m.stop_reason === "aborted") {
        rows.push({ kind: "note", key, text: t("Stopped") });
        continue;
      }
      if (m.stop_reason === "error") {
        rows.push({ kind: "bot", key, bot: e.author, turn: e.turn, name: group && prev?.bot !== e.author, text: text || t("The reply failed"), tools: [], error: true });
        continue;
      }
      // A run of tool calls in one turn reads as one line.
      if (!text && prev && prev.turn === e.turn && !prev.text) {
        prev.tools = [...new Set([...prev.tools, ...tools])];
        continue;
      }
      if (!text && !tools.length) continue;
      rows.push({ kind: "bot", key, bot: e.author, turn: e.turn, name: group && prev?.bot !== e.author, text, tools });
    }
  }
  for (const l of room.live) {
    if (!l.text) continue;
    rows.push({ kind: "bot", key: `live-${l.turn}`, bot: l.bot, turn: l.turn, name: group && lastBot()?.bot !== l.bot, text: l.text, tools: [] });
  }
  if (busy) {
    const who = room.working ? bots[room.working]?.name : undefined;
    const label = room.tool ? t("Running %s…", room.tool) : room.retrying ? t("Retrying…") : group && who ? t("%s is working…", who) : t("Working…");
    rows.push({ kind: "working", key: "working", label });
  }
  return rows;
}
