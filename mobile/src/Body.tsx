import { ScrollView, StyleSheet, Text, View, type ColorValue } from "react-native";
import { usePalette } from "./theme";

/** A reply's text: prose as selectable text, fenced code in a monospaced block that scrolls sideways. */
export function Body({ text, color }: { text: string; color?: ColorValue }) {
  const p = usePalette();
  const pieces = text.split(/^```[^\n]*\n?/m);
  return (
    <View style={{ gap: 8 }}>
      {pieces.map((piece, i) => {
        const s = piece.replace(/\n+$/, "");
        if (!s.trim()) return null;
        if (i % 2 === 1)
          return (
            <ScrollView key={i} horizontal style={[styles.code, { backgroundColor: p.code }]} contentContainerStyle={{ padding: 10 }}>
              <Text selectable style={[styles.mono, { color: p.label }]}>
                {s}
              </Text>
            </ScrollView>
          );
        return (
          <Text key={i} selectable style={[styles.text, { color: color ?? p.label }]}>
            {s.trim()}
          </Text>
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  text: { fontSize: 17, lineHeight: 24 },
  code: { borderRadius: 8 },
  mono: { fontFamily: "Menlo", fontSize: 13, lineHeight: 18 },
});
