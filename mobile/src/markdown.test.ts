import { expect, test } from "bun:test";
import { blocks, inline, plain } from "./markdown";

test("inline: bold, italic, code, links, strikes", () => {
  expect(inline("a **b** *c* `d` [e](https://x.y) ~~f~~")).toEqual([
    { kind: "text", text: "a " },
    { kind: "text", text: "b", bold: true },
    { kind: "text", text: " " },
    { kind: "text", text: "c", italic: true },
    { kind: "text", text: " " },
    { kind: "code", text: "d" },
    { kind: "text", text: " " },
    { kind: "link", text: "e", url: "https://x.y" },
    { kind: "text", text: " " },
    { kind: "text", text: "f", strike: true },
  ]);
  expect(inline("snake_case_name and 2 * 3 * 4")).toEqual([{ kind: "text", text: "snake_case_name and 2 * 3 * 4" }]);
  expect(inline("**回答你的问题:会。** 我已经")[0]).toEqual({ kind: "text", text: "回答你的问题:会。", bold: true });
});

test("blocks of a bot's reply", () => {
  const md = "# Plan\n\nIntro line\nand more.\n\n- one `zt bug ls`\n  - nested\n- [x] done\n1. first\n\n> quoted\n\n```sh\nls -la\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n---";
  const bs = blocks(md);
  expect(bs.map((b) => b.kind)).toEqual(["heading", "paragraph", "item", "item", "item", "item", "quote", "code", "table", "rule"]);
  expect(bs[3]).toMatchObject({ kind: "item", depth: 1, marker: "•" });
  expect(bs[4]).toMatchObject({ marker: "☑" });
  expect(bs[5]).toMatchObject({ marker: "1." });
  expect(bs[7]).toEqual({ kind: "code", lang: "sh", text: "ls -la" });
  const table = bs[8] as any;
  expect(table.header.length).toBe(2);
  expect(table.rows[0][1]).toEqual([{ kind: "text", text: "2" }]);
});

test("plain is a preview's one line", () => {
  expect(plain("哨兵这边跑通了:\n\n- 踩到一个坑:`zt bug ls` **默认只回 20 条**")).toBe("哨兵这边跑通了: 踩到一个坑:zt bug ls 默认只回 20 条");
});
