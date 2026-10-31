// A reply's markdown as blocks and inline spans, the subset a bot writes: headings, paragraphs,
// lists (nested by indent, task boxes), quotes, fenced code, tables, rules; bold, italic,
// strikethrough, inline code and links. Anything else reads as the text it is.

export type Span =
  | { kind: "text"; text: string; bold?: boolean; italic?: boolean; strike?: boolean }
  | { kind: "code"; text: string }
  | { kind: "link"; text: string; url: string };

export type Block =
  | { kind: "heading"; level: number; spans: Span[] }
  | { kind: "paragraph"; spans: Span[] }
  | { kind: "item"; depth: number; marker: string; spans: Span[] }
  | { kind: "quote"; spans: Span[] }
  | { kind: "code"; lang: string; text: string }
  | { kind: "table"; header: Span[][]; rows: Span[][][] }
  | { kind: "rule" };

const fence = /^\s*(```|~~~)\s*([\w+-]*)\s*$/;
const heading = /^(#{1,6})\s+(.*?)\s*#*\s*$/;
const bullet = /^(\s*)[-*+]\s+(\[[ xX]\]\s+)?(.*)$/;
const ordered = /^(\s*)(\d+)[.)]\s+(.*)$/;
const quote = /^\s*>\s?(.*)$/;
const rule = /^\s*([-*_])(\s*\1){2,}\s*$/;
const tableSep = /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/;

function cells(line: string): string[] {
  return line.trim().replace(/^\|/, "").replace(/\|$/, "").split("|").map((c) => c.trim());
}

/** The blocks of a markdown text. */
export function blocks(text: string): Block[] {
  const out: Block[] = [];
  const lines = text.replace(/\r\n?/g, "\n").split("\n");
  let para: string[] = [];
  const flush = () => {
    if (para.length) out.push({ kind: "paragraph", spans: inline(para.join("\n")) });
    para = [];
  };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    let m: RegExpMatchArray | null;
    if ((m = line.match(fence))) {
      flush();
      const body: string[] = [];
      for (i++; i < lines.length && !lines[i].match(new RegExp(`^\\s*${m[1]}\\s*$`)); i++) body.push(lines[i]);
      out.push({ kind: "code", lang: m[2], text: body.join("\n") });
    } else if (!line.trim()) {
      flush();
    } else if ((m = line.match(heading))) {
      flush();
      out.push({ kind: "heading", level: m[1].length, spans: inline(m[2]) });
    } else if (rule.test(line)) {
      flush();
      out.push({ kind: "rule" });
    } else if ((m = line.match(bullet))) {
      flush();
      const box = m[2] ? (/[xX]/.test(m[2]) ? "☑" : "☐") : "•";
      out.push({ kind: "item", depth: Math.floor(m[1].replace(/\t/g, "  ").length / 2), marker: box, spans: inline(m[3]) });
    } else if ((m = line.match(ordered))) {
      flush();
      out.push({ kind: "item", depth: Math.floor(m[1].replace(/\t/g, "  ").length / 2), marker: `${m[2]}.`, spans: inline(m[3]) });
    } else if ((m = line.match(quote))) {
      flush();
      const body = [m[1]];
      while (i + 1 < lines.length && (m = lines[i + 1].match(quote))) {
        body.push(m[1]);
        i++;
      }
      out.push({ kind: "quote", spans: inline(body.join("\n")) });
    } else if (line.includes("|") && i + 1 < lines.length && tableSep.test(lines[i + 1])) {
      flush();
      const header = cells(line).map((c) => inline(c));
      const rows: Span[][][] = [];
      for (i += 2; i < lines.length && lines[i].includes("|") && lines[i].trim(); i++) rows.push(cells(lines[i]).map((c) => inline(c)));
      i--;
      out.push({ kind: "table", header, rows });
    } else {
      para.push(line);
    }
  }
  flush();
  return out;
}

type Style = { bold?: boolean; italic?: boolean; strike?: boolean };

/** The spans of one block's text. */
export function inline(text: string, style: Style = {}): Span[] {
  const out: Span[] = [];
  const push = (s: string) => {
    if (!s) return;
    const last = out.at(-1);
    if (last?.kind === "text" && !!last.bold === !!style.bold && !!last.italic === !!style.italic && !!last.strike === !!style.strike) {
      last.text += s;
    } else {
      out.push({ kind: "text", text: s, ...style });
    }
  };
  let i = 0;
  while (i < text.length) {
    const rest = text.slice(i);
    let m: RegExpMatchArray | null;
    if ((m = rest.match(/^(`+)([\s\S]*?[^`])\1(?!`)/))) {
      out.push({ kind: "code", text: m[2].trim() });
    } else if ((m = rest.match(/^\[([^\]]+)\]\(([^)\s]+)\)/))) {
      out.push({ kind: "link", text: m[1], url: m[2] });
    } else if ((m = rest.match(/^(\*\*|__)(?=\S)([\s\S]*?\S)\1/))) {
      out.push(...inline(m[2], { ...style, bold: true }));
    } else if ((m = rest.match(/^~~(?=\S)([\s\S]*?\S)~~/))) {
      out.push(...inline(m[1], { ...style, strike: true }));
    } else if ((m = rest.match(/^\*(?=[^\s*])([^*]*?[^\s*])\*/)) || (/(^|[^\w])$/.test(text.slice(0, i)) && (m = rest.match(/^_(?=\S)([^_]*?\S)_(?!\w)/)))) {
      out.push(...inline(m[1], { ...style, italic: true }));
    } else if ((m = rest.match(/^\\([\\`*_~[\]()#>|-])/))) {
      push(m[1]);
    } else {
      push(text[i]);
      i++;
      continue;
    }
    i += m[0].length;
  }
  return out;
}

/** Markdown as one line of plain text, for a preview. */
export function plain(text: string): string {
  return blocks(text)
    .map((b) => {
      const words = (spans: Span[]) => spans.map((s) => s.text).join("");
      switch (b.kind) {
        case "code":
          return b.text;
        case "rule":
          return "";
        case "table":
          return [b.header, ...b.rows].map((r) => r.map(words).join(" ")).join(" ");
        case "item":
          return `${b.marker === "•" ? "" : b.marker + " "}${words(b.spans)}`;
        default:
          return words(b.spans);
      }
    })
    .join(" ")
    .replace(/\s+/g, " ")
    .trim();
}
