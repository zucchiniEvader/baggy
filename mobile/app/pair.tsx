import { CameraView } from "expo-camera";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import { useLocalSearchParams } from "expo-router";
import { useEffect, useRef, useState } from "react";
import { ActivityIndicator, Alert, Image, Pressable, StyleSheet, Text, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { t } from "../src/i18n";
import { pair } from "../src/store";
import { usePalette } from "../src/theme";

export default function PairScreen() {
  const p = usePalette();
  const insets = useSafeAreaInsets();
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);

  async function tryPair(code: string) {
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    try {
      await pair(code);
      void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
    } catch (e) {
      void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
      Alert.alert(t("Pairing failed"), e instanceof Error ? e.message : String(e));
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }

  // The code opened as a link (baggy://pair?…, from the Camera app) lands here with its fields.
  const params = useLocalSearchParams<{ host?: string; port?: string; token?: string; name?: string }>();
  useEffect(() => {
    if (!params.host || !params.port || !params.token) return;
    const q = (k: string, v: string) => `${k}=${encodeURIComponent(v)}`;
    void tryPair(`baggy://pair?${[q("host", params.host), q("port", params.port), q("token", params.token), q("name", params.name ?? "")].join("&")}`);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params.host, params.port, params.token]);

  // VisionKit's scanner reports the code here.
  useEffect(() => {
    const sub = CameraView.onModernBarcodeScanned(({ data }) => {
      if (!data.startsWith("baggy://pair?") || inFlight.current) return;
      void CameraView.dismissScanner();
      void tryPair(data);
    });
    return () => sub.remove();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function scan() {
    if (!CameraView.isModernBarcodeScannerAvailable) {
      Alert.alert(t("Camera access needed"), t("Allow the camera to scan the pairing code, or paste the code instead."));
      return;
    }
    await CameraView.launchScanner({ barcodeTypes: ["qr"], isGuidanceEnabled: true, isHighlightingEnabled: true });
  }

  async function paste() {
    const text = (await Clipboard.getStringAsync()).trim();
    if (text) void tryPair(text);
  }

  return (
    <View style={[styles.root, { backgroundColor: p.background, paddingTop: insets.top, paddingBottom: insets.bottom + 16 }]}>
      <View style={styles.middle}>
        <Image source={require("../assets/icon.png")} style={styles.icon} />
        <Text style={[styles.title, { color: p.label }]}>{t("Pair with Your Computer")}</Text>
        <Text style={[styles.body, { color: p.secondaryLabel }]}>
          {t("Open Baggy on your computer, go to Settings › Phone, turn on Connect a phone, and scan the code.")}
        </Text>
      </View>
      <View style={styles.buttons}>
        {busy ? (
          <ActivityIndicator style={{ height: 50 }} />
        ) : (
          <>
            <Pressable onPress={scan} style={({ pressed }) => [styles.button, { backgroundColor: p.tint, opacity: pressed ? 0.8 : 1 }]}>
              <Text style={[styles.buttonText, { color: "#fff" }]}>{t("Scan Code")}</Text>
            </Pressable>
            <Pressable onPress={paste} style={({ pressed }) => [styles.button, { backgroundColor: p.fill, opacity: pressed ? 0.7 : 1 }]}>
              <Text style={[styles.buttonText, { color: p.tint }]}>{t("Paste Code")}</Text>
            </Pressable>
          </>
        )}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1, paddingHorizontal: 24 },
  middle: { flex: 1, alignItems: "center", justifyContent: "center", gap: 12 },
  icon: { width: 96, height: 96, borderRadius: 22, marginBottom: 8 },
  title: { fontSize: 28, fontWeight: "700", textAlign: "center" },
  body: { fontSize: 16, lineHeight: 22, textAlign: "center", maxWidth: 340 },
  buttons: { gap: 10 },
  button: { height: 50, borderRadius: 14, alignItems: "center", justifyContent: "center" },
  buttonText: { fontSize: 17, fontWeight: "600" },
});
