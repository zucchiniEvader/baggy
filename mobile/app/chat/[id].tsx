import { Stack, useLocalSearchParams } from "expo-router";
import { useHeaderHeight } from "expo-router/react-navigation";
import { SymbolView } from "expo-symbols";
import { useEffect, useMemo, useState } from "react";
import { ActivityIndicator, Alert, FlatList, KeyboardAvoidingView, Pressable, StyleSheet, Text, TextInput, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Body } from "../../src/Body";
import { t } from "../../src/i18n";
import { chatTitle, textOf, type Bot } from "../../src/model";
import { rowsOf, type Row } from "../../src/rows";
import { closeChat, openChat, send, stop, useStore, type Room } from "../../src/store";
import { usePalette } from "../../src/theme";

const emptyRoom: Room = { entries: [], loaded: false, live: [] };

export default function ChatScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const p = usePalette();
  const insets = useSafeAreaInsets();
  const headerHeight = useHeaderHeight();
  const chat = useStore((s) => s.chats.find((c) => c.id === id));
  const bots = useStore((s) => s.bots);
  const room = useStore((s) => s.rooms[id] ?? emptyRoom);
  const busy = useStore((s) => s.busy[id] ?? false);
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);

  useEffect(() => {
    void openChat(id);
    return () => closeChat(id);
  }, [id]);

  const rows = useMemo(() => (chat ? rowsOf(chat, room, busy, bots).reverse() : []), [chat, room, busy, bots]);
  if (!chat) return null;
  const title = chatTitle(chat, bots);

  async function submit() {
    const s = text.trim();
    if (!s || sending) return;
    setSending(true);
    try {
      await send(id, s);
      setText("");
    } catch (e) {
      Alert.alert(t("Not sent"), e instanceof Error ? e.message : String(e));
    } finally {
      setSending(false);
    }
  }

  const canSend = text.trim() !== "" && !sending;
  const showStop = busy && !canSend;

  return (
    <KeyboardAvoidingView behavior="padding" keyboardVerticalOffset={headerHeight} style={{ flex: 1, backgroundColor: p.background }}>
      <Stack.Screen options={{ title, headerBackTitle: t("Chats") }} />
      <FlatList
        inverted
        data={rows}
        keyExtractor={(r) => r.key}
        keyboardDismissMode="interactive"
        // Inverted, the list's end is its top: a short chat starts there.
        contentContainerStyle={{ paddingHorizontal: 16, paddingVertical: 12, flexGrow: 1, justifyContent: "flex-end" }}
        ListFooterComponent={!room.loaded ? <ActivityIndicator style={{ marginVertical: 24 }} /> : null}
        renderItem={({ item }) => <RowView row={item} bots={bots} />}
      />
      {room.error && (
        <View style={[styles.error, { backgroundColor: p.fill }]}>
          <SymbolView name="exclamationmark.circle" size={15} tintColor={p.red as string} />
          <Text style={{ color: p.red, fontSize: 14, flex: 1 }}>{room.error}</Text>
        </View>
      )}
      <View style={[styles.composer, { paddingBottom: Math.max(insets.bottom, 8) }]}>
        <View style={[styles.field, { backgroundColor: p.fill }]}>
          <TextInput
            value={text}
            onChangeText={setText}
            placeholder={chat.kind === "dm" ? t("Message %s", title) : t("Message")}
            placeholderTextColor={p.tertiaryLabel as string}
            multiline
            style={[styles.input, { color: p.label }]}
          />
          <Pressable
            accessibilityLabel={showStop ? t("Stop") : t("Message")}
            disabled={!showStop && !canSend}
            onPress={() => (showStop ? void stop(id).catch(() => {}) : void submit())}
            style={[styles.send, { backgroundColor: showStop ? p.label : canSend ? p.tint : p.fill }]}
          >
            {sending ? (
              <ActivityIndicator size="small" color="#fff" />
            ) : (
              <SymbolView name={showStop ? "stop.fill" : "arrow.up"} size={showStop ? 12 : 16} weight="bold" tintColor={showStop ? (p.background as string) : "#fff"} />
            )}
          </Pressable>
        </View>
      </View>
    </KeyboardAvoidingView>
  );
}

function RowView({ row, bots }: { row: Row; bots: Record<string, Bot> }) {
  const p = usePalette();
  switch (row.kind) {
    case "user": {
      const e = row.entry;
      const quote = e.meta.reply_to;
      return (
        <View style={styles.userWrap}>
          {quote && (
            <Text style={[styles.quote, { color: p.secondaryLabel }]} numberOfLines={2}>
              {quote.text}
            </Text>
          )}
          <View style={[styles.bubble, { backgroundColor: p.userBubble }]}>
            {!!textOf(e.msg) && (
              <Text selectable style={{ color: p.label, fontSize: 17, lineHeight: 23 }}>
                {textOf(e.msg)}
              </Text>
            )}
            {e.meta.files?.map((f) => (
              <View key={f.id} style={styles.file}>
                <SymbolView name="paperclip" size={13} tintColor={p.secondaryLabel as string} />
                <Text style={{ color: p.secondaryLabel, fontSize: 14 }} numberOfLines={1}>
                  {f.name}
                </Text>
              </View>
            ))}
          </View>
        </View>
      );
    }
    case "bot":
      return (
        <View style={styles.botWrap}>
          {row.name && <Text style={[styles.botName, { color: p.secondaryLabel }]}>{bots[row.bot]?.name ?? ""}</Text>}
          {!!row.text && <Body text={row.text} color={row.error ? p.red : undefined} />}
          {row.tools.length > 0 && (
            <View style={styles.tools}>
              <SymbolView name="wrench.and.screwdriver" size={13} tintColor={p.tertiaryLabel as string} />
              <Text style={{ color: p.tertiaryLabel, fontSize: 14, flex: 1 }} numberOfLines={1}>
                {t("Used %s", row.tools.join(", "))}
              </Text>
            </View>
          )}
        </View>
      );
    case "note":
      return <Text style={[styles.note, { color: p.tertiaryLabel }]}>{row.text}</Text>;
    case "working":
      return (
        <View style={styles.working}>
          <ActivityIndicator size="small" />
          <Text style={{ color: p.secondaryLabel, fontSize: 15 }}>{row.label}</Text>
        </View>
      );
  }
}

const styles = StyleSheet.create({
  userWrap: { alignItems: "flex-end", marginVertical: 6, marginLeft: 48, gap: 4 },
  quote: { fontSize: 13, paddingHorizontal: 4 },
  bubble: { borderRadius: 18, paddingHorizontal: 14, paddingVertical: 9, gap: 4 },
  file: { flexDirection: "row", alignItems: "center", gap: 6 },
  botWrap: { marginVertical: 6, gap: 6 },
  botName: { fontSize: 13, fontWeight: "600", marginTop: 6 },
  tools: { flexDirection: "row", alignItems: "center", gap: 6 },
  note: { textAlign: "center", fontSize: 13, marginVertical: 10 },
  working: { flexDirection: "row", alignItems: "center", gap: 8, marginVertical: 8 },
  error: { flexDirection: "row", alignItems: "center", gap: 8, marginHorizontal: 16, marginBottom: 6, padding: 10, borderRadius: 10 },
  composer: { paddingHorizontal: 12, paddingTop: 6 },
  field: { flexDirection: "row", alignItems: "flex-end", borderRadius: 22, paddingLeft: 16, paddingRight: 5, paddingVertical: 5, minHeight: 44 },
  input: { flex: 1, fontSize: 17, maxHeight: 140, paddingTop: 7, paddingBottom: 7 },
  send: { width: 34, height: 34, borderRadius: 17, alignItems: "center", justifyContent: "center", marginLeft: 8 },
});
