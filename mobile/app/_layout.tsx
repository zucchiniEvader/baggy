import { DarkTheme, DefaultTheme, Stack, ThemeProvider } from "expo-router";
import { StatusBar } from "expo-status-bar";
import * as Linking from "expo-linking";
import { useEffect, useMemo } from "react";
import { Alert } from "react-native";
import { t } from "../src/i18n";
import { pair, start, useStore } from "../src/store";
import { usePalette } from "../src/theme";

export default function RootLayout() {
  const ready = useStore((s) => s.ready);
  const paired = useStore((s) => s.pairing !== null);
  const p = usePalette();

  useEffect(() => {
    void start();
  }, []);

  // A pairing code opened while paired (a new token, another computer) replaces the pairing:
  // the pair screen, which takes it otherwise, is closed to a paired phone.
  const url = Linking.useLinkingURL();
  useEffect(() => {
    if (!ready || !paired || !url?.startsWith("baggy://pair?")) return;
    const current = useStore.getState().pairing;
    if (current && url.includes(`token=${current.token}`)) return;
    pair(url).catch((e) => Alert.alert(t("Pairing failed"), e instanceof Error ? e.message : String(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url, ready]);

  const theme = useMemo(() => {
    const base = p.dark ? DarkTheme : DefaultTheme;
    return { ...base, colors: { ...base.colors, primary: p.tint as string, background: p.background as string, card: p.background as string, text: p.label as string, border: p.separator as string } };
  }, [p]);

  if (!ready) return null;
  return (
    <ThemeProvider value={theme}>
      <StatusBar style="auto" />
      <Stack screenOptions={{ headerBackButtonDisplayMode: "minimal" }}>
        <Stack.Protected guard={paired}>
          <Stack.Screen name="index" options={{ title: t("Chats"), headerLargeTitle: true, headerTransparent: true, headerBlurEffect: "systemMaterial", headerLargeStyle: { backgroundColor: "transparent" } }} />
          <Stack.Screen name="chat/[id]" options={{ title: "" }} />
          <Stack.Screen
            name="settings"
            options={{ title: t("Settings"), presentation: "formSheet", sheetAllowedDetents: [0.6, 1], sheetGrabberVisible: true, contentStyle: { backgroundColor: p.groupedBackground } }}
          />
        </Stack.Protected>
        <Stack.Protected guard={!paired}>
          <Stack.Screen name="pair" options={{ headerShown: false }} />
        </Stack.Protected>
      </Stack>
    </ThemeProvider>
  );
}
