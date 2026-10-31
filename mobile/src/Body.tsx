import { Linking, ScrollView, StyleSheet, Text, View, type ColorValue } from "react-native";
import { blocks, type Block, type Span } from "./markdown";
import { usePalette } from "./theme";

/** A reply's markdown: headings, lists, quotes, tables and fenced code (a monospaced block that
 * scrolls sideways) as blocks; bold, italic, inline code and links in the text. */
export function Body({ text, color }: { text: string; color?: ColorValue }) {
  const p = usePalette();
  const fg = color ?? p.label;

  const spans = (ss: Span[]) =>
    ss.map((s, i) => {
      if (s.kind === "code")
        return (
          <Text key={i} style={[styles.inlineCode, { backgroundColor: p.code }]}>
            {s.text}
          </Text>
        );
      if (s.kind === "link") {
        const ok = /^(https?:|mailto:)/i.test(s.url);
        return (
          <Text key={i} style={{ color: p.tint }} onPress={ok ? () => void Linking.openURL(s.url) : undefined}>
            {s.text}
          </Text>
        );
      }
      return (
        <Text
          key={i}
          style={{
            fontWeight: s.bold ? "600" : undefined,
            fontStyle: s.italic ? "italic" : undefined,
            textDecorationLine: s.strike ? "line-through" : undefined,
          }}
        >
          {s.text}
        </Text>
      );
    });

  const block = (b: Block, i: number) => {
    switch (b.kind) {
      case "heading":
        return (
          <Text key={i} selectable style={[styles.text, { color: fg, fontWeight: "700", fontSize: b.level <= 2 ? 20 : 17 }]}>
            {spans(b.spans)}
          </Text>
        );
      case "paragraph":
        return (
          <Text key={i} selectable style={[styles.text, { color: fg }]}>
            {spans(b.spans)}
          </Text>
        );
      case "item":
        return (
          <View key={i} style={[styles.item, { paddingLeft: b.depth * 18 }]}>
            <Text style={[styles.text, styles.marker, { color: p.secondaryLabel }]}>{b.marker}</Text>
            <Text selectable style={[styles.text, { color: fg, flex: 1 }]}>
              {spans(b.spans)}
            </Text>
          </View>
        );
      case "quote":
        return (
          <View key={i} style={[styles.quote, { borderLeftColor: p.separator }]}>
            <Text selectable style={[styles.text, { color: p.secondaryLabel }]}>
              {spans(b.spans)}
            </Text>
          </View>
        );
      case "code":
        return (
          <ScrollView key={i} horizontal style={[styles.code, { backgroundColor: p.code }]} contentContainerStyle={{ padding: 10 }}>
            <Text selectable style={[styles.mono, { color: p.label }]}>
              {b.text}
            </Text>
          </ScrollView>
        );
      case "table":
        return (
          <ScrollView key={i} horizontal style={[styles.table, { borderColor: p.separator }]}>
            <View>
              {[b.header, ...b.rows].map((row, r) => (
                <View key={r} style={[styles.tableRow, r > 0 && { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: p.separator }]}>
                  {row.map((cell, c) => (
                    <Text key={c} selectable style={[styles.cell, { color: fg, fontWeight: r === 0 ? "600" : undefined }]}>
                      {spans(cell)}
                    </Text>
                  ))}
                </View>
              ))}
            </View>
          </ScrollView>
        );
      case "rule":
        return <View key={i} style={{ height: StyleSheet.hairlineWidth, backgroundColor: p.separator, marginVertical: 4 }} />;
    }
  };

  return <View style={{ gap: 8 }}>{blocks(text).map(block)}</View>;
}

const styles = StyleSheet.create({
  text: { fontSize: 17, lineHeight: 24 },
  item: { flexDirection: "row", gap: 6, marginTop: -4 },
  marker: { minWidth: 14 },
  quote: { borderLeftWidth: 3, paddingLeft: 10 },
  inlineCode: { fontFamily: "Menlo", fontSize: 15 },
  code: { borderRadius: 8, flexGrow: 0 },
  mono: { fontFamily: "Menlo", fontSize: 13, lineHeight: 18 },
  table: { borderWidth: StyleSheet.hairlineWidth, borderRadius: 8, flexGrow: 0 },
  tableRow: { flexDirection: "row" },
  cell: { fontSize: 15, lineHeight: 21, paddingHorizontal: 10, paddingVertical: 6, minWidth: 80, maxWidth: 260 },
});
