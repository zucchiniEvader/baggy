import { expect, mock, test } from "bun:test";

mock.module("expo-localization", () => ({ getLocales: () => [{ languageCode: "en" }] }));
const { rowsOf } = await import("./rows");

const say = (text: string) => ({ kind: "text" as const, text });
const callTool = (name: string) => ({ kind: "tool_call" as const, name, call_id: name });
let seq = 0;
const entry = (author: string, role: "user" | "assistant" | "tool", parts: any[], turn = "", extra: any = {}) => ({
  chat_id: "c",
  seq: ++seq,
  author,
  turn,
  meta: {},
  msg: { id: String(seq), role, parts, ...extra },
  created: 0,
});
const bots = { a: { id: "a", name: "Chef" } as any, b: { id: "b", name: "Scout" } as any };
const group = { id: "c", kind: "group", members: ["a", "b"] } as any;

test("a group's rows: names at a new speaker, tool calls folded, results hidden", () => {
  const room = {
    loaded: true,
    live: [{ turn: "t3", bot: "a", text: "stream" }],
    entries: [
      entry("user", "user", [say("plan it")]),
      entry("a", "assistant", [callTool("shell")], "t1"),
      entry("a", "tool", [{ kind: "tool_result", text: "ok" }], "t1"),
      entry("a", "assistant", [callTool("read_file")], "t1"),
      entry("a", "assistant", [say("done")], "t1"),
      entry("b", "assistant", [say("agreed")], "t2"),
      entry("b", "assistant", [], "t2b", { stop_reason: "aborted" }),
    ],
  };
  const rows = rowsOf(group, room, true, bots);
  expect(rows.map((r) => r.kind)).toEqual(["user", "bot", "bot", "bot", "note", "bot", "working"]);
  const tools = rows[1] as any;
  expect(tools.tools).toEqual(["shell", "read_file"]);
  expect(tools.name).toBe(true);
  expect((rows[2] as any).name).toBe(false); // Chef again
  expect((rows[3] as any).name).toBe(true); // Scout
  expect((rows[5] as any).text).toBe("stream");
  expect((rows[6] as any).label).toBe("Working…");
});

test("the working row names the tool", () => {
  const rows = rowsOf({ ...group, kind: "dm" }, { loaded: true, live: [], entries: [], working: "a", tool: "shell" }, true, bots);
  expect((rows[0] as any).label).toBe("Running shell…");
});
