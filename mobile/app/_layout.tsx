import { DarkTheme, DefaultTheme, Stack, ThemeProvider } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { useEffect, useMemo } from "react";
import { t } from "../src/i18n";
import { start, useStore } from "../src/store";
import { usePalette } from "../src/theme";

export default function RootLayout() {
  const ready = useStore((s) => s.ready);
  const paired = useStore((s) => s.pairing !== null);
  const p = usePalette();

  useEffect(() => {
    void start();
  }, []);

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
