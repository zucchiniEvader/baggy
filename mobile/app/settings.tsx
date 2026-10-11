import { router } from "expo-router";
import { Alert, Pressable, ScrollView, StyleSheet, Text, View } from "react-native";
import { t } from "../src/i18n";
import { baseURL } from "../src/pairing";
import { unpair, useStore } from "../src/store";
import { usePalette } from "../src/theme";

export default function SettingsScreen() {
  const p = usePalette();
  const pairing = useStore((s) => s.pairing);
  const status = useStore((s) => s.status);
  if (!pairing) return null;
  const statusText = { online: t("Connected"), connecting: t("Connecting…"), offline: t("Can't connect"), unpaired: t("Pair again") }[status];

  function confirmUnpair() {
    Alert.alert(t("Unpair this phone?"), t("You can pair again by scanning the code."), [
      { text: t("Cancel"), style: "cancel" },
      {
        text: t("Unpair"),
        style: "destructive",
        onPress: () => {
          router.dismissAll();
          void unpair();
        },
      },
    ]);
  }

  const row = (label: string, value: string, last = false) => (
    <View style={[styles.row, !last && { borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: p.separator }]}>
      <Text style={{ color: p.label, fontSize: 17 }}>{label}</Text>
      <Text style={{ color: p.secondaryLabel, fontSize: 17, flexShrink: 1 }} numberOfLines={1}>
        {value}
      </Text>
    </View>
  );

  return (
    <ScrollView style={{ backgroundColor: p.groupedBackground }} contentContainerStyle={{ padding: 16, gap: 24 }}>
      <View style={[styles.card, { backgroundColor: p.cell }]}>
        {row(t("Computer"), pairing.name)}
        {row(t("Address"), baseURL(pairing).replace("http://", ""))}
        {row(t("Status"), statusText, true)}
      </View>
      <Pressable onPress={confirmUnpair} style={({ pressed }) => [styles.card, styles.row, { backgroundColor: pressed ? p.fill : p.cell, justifyContent: "center" }]}>
        <Text style={{ color: p.red, fontSize: 17 }}>{t("Unpair")}</Text>
      </Pressable>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  card: { borderRadius: 12, overflow: "hidden" },
  row: { flexDirection: "row", justifyContent: "space-between", alignItems: "center", gap: 12, minHeight: 48, paddingHorizontal: 16 },
});
