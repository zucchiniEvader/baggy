import { router, Stack } from "expo-router";
import { SymbolView } from "expo-symbols";
import { useMemo, useState } from "react";
import { ActivityIndicator, FlatList, Pressable, StyleSheet, Text, View } from "react-native";
import { Avatar, GroupAvatar } from "../src/Avatar";
import { stamp } from "../src/format";
import { t } from "../src/i18n";
import { chatTitle, User, type Chat } from "../src/model";
import { reconnect, useStore } from "../src/store";
import { usePalette } from "../src/theme";

export default function ChatsScreen() {
  const p = usePalette();
  const chats = useStore((s) => s.chats);
  const bots = useStore((s) => s.bots);
  const status = useStore((s) => s.status);
  const [query, setQuery] = useState("");

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    const list = q ? chats.filter((c) => chatTitle(c, bots).toLowerCase().includes(q)) : chats;
    // Pinned first, newest pin on top; then by the last message, as the desktop's sidebar.
    return [...list.filter((c) => c.pinned).sort((a, b) => b.pinned - a.pinned), ...list.filter((c) => !c.pinned)];
  }, [chats, bots, query]);

  return (
    <>
      <Stack.Screen
        options={{
          headerSearchBarOptions: { placeholder: t("Chats"), onChangeText: (e) => setQuery(e.nativeEvent.text), hideWhenScrolling: true },
          headerRight: () => (
            <Pressable onPress={() => router.push("/settings")} hitSlop={10} accessibilityLabel={t("Settings")}>
              <SymbolView name="gearshape" size={22} tintColor={p.tint as string} />
            </Pressable>
          ),
        }}
      />
      <FlatList
        contentInsetAdjustmentBehavior="automatic"
        style={{ backgroundColor: p.background }}
        data={shown}
        keyExtractor={(c) => c.id}
        ListHeaderComponent={<StatusBanner status={status} />}
        ListEmptyComponent={
          status === "online" ? (
            <View style={styles.empty}>
              <SymbolView name="bubble.left.and.bubble.right" size={36} tintColor={p.tertiaryLabel as string} />
              <Text style={[styles.emptyTitle, { color: p.label }]}>{t("No chats yet")}</Text>
              <Text style={{ color: p.secondaryLabel, fontSize: 15 }}>{t("Create a bot in Baggy on your computer.")}</Text>
            </View>
          ) : null
        }
        renderItem={({ item }) => <ChatRow chat={item} />}
      />
    </>
  );
}

function StatusBanner({ status }: { status: string }) {
  const p = usePalette();
  if (status === "online") return null;
  const label = status === "connecting" ? t("Connecting…") : status === "unpaired" ? t("Pair again") : t("Can't connect");
  return (
    <Pressable
      onPress={() => (status === "unpaired" ? router.push("/settings") : reconnect())}
      style={[styles.banner, { backgroundColor: p.fill }]}
    >
      {status === "connecting" ? <ActivityIndicator size="small" /> : <SymbolView name="exclamationmark.triangle" size={16} tintColor={p.secondaryLabel as string} />}
      <Text style={{ color: p.secondaryLabel, fontSize: 15, flex: 1 }}>{label}</Text>
      {status === "offline" && <Text style={{ color: p.tint, fontSize: 15 }}>{t("Try Again")}</Text>}
    </Pressable>
  );
}

function ChatRow({ chat }: { chat: Chat }) {
  const p = usePalette();
  const bots = useStore((s) => s.bots);
  const preview = useStore((s) => s.previews[chat.id]);
  const unread = useStore((s) => s.unread[chat.id] ?? 0);
  const busy = useStore((s) => s.busy[chat.id] ?? false);
  const title = chatTitle(chat, bots);
  let line = preview?.Text ?? "";
  if (preview && chat.kind === "group") {
    const who = preview.Author === User ? t("You") : bots[preview.Author]?.name;
    if (who) line = `${who}: ${line}`;
  }
  return (
    <Pressable onPress={() => router.push({ pathname: "/chat/[id]", params: { id: chat.id } })} style={({ pressed }) => [styles.row, pressed && { backgroundColor: p.fill }]}>
      {chat.kind === "dm" ? <Avatar bot={bots[chat.members[0]]} size={52} /> : <GroupAvatar bots={chat.members.map((m) => bots[m])} size={52} />}
      <View style={[styles.rowText, { borderBottomColor: p.separator }]}>
        <View style={styles.rowTop}>
          <Text style={[styles.name, { color: p.label }]} numberOfLines={1}>
            {title}
          </Text>
          <Text style={{ color: p.secondaryLabel, fontSize: 14 }}>{stamp(preview?.At ?? chat.updated)}</Text>
        </View>
        <View style={styles.rowTop}>
          <Text style={{ color: p.secondaryLabel, fontSize: 15, flex: 1 }} numberOfLines={2}>
            {busy ? t("Working…") : line}
          </Text>
          {unread > 0 && (
            <View style={[styles.badge, { backgroundColor: p.tint }]}>
              <Text style={styles.badgeText}>{unread}</Text>
            </View>
          )}
        </View>
      </View>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  row: { flexDirection: "row", alignItems: "center", paddingLeft: 16, gap: 12 },
  rowText: { flex: 1, paddingVertical: 10, paddingRight: 16, gap: 2, borderBottomWidth: StyleSheet.hairlineWidth, minHeight: 72, justifyContent: "center" },
  rowTop: { flexDirection: "row", alignItems: "center", gap: 8 },
  name: { fontSize: 17, fontWeight: "600", flex: 1 },
  badge: { minWidth: 20, height: 20, borderRadius: 10, paddingHorizontal: 6, alignItems: "center", justifyContent: "center" },
  badgeText: { color: "#fff", fontSize: 13, fontWeight: "600", fontVariant: ["tabular-nums"] },
  banner: { flexDirection: "row", alignItems: "center", gap: 10, marginHorizontal: 16, marginVertical: 8, paddingHorizontal: 14, paddingVertical: 10, borderRadius: 12 },
  empty: { alignItems: "center", gap: 8, paddingTop: 120 },
  emptyTitle: { fontSize: 20, fontWeight: "600", marginTop: 4 },
});
