// UIKit's semantic colors, so the app follows light and dark mode as the system's apps do.

import { PlatformColor, useColorScheme, type ColorValue } from "react-native";

export interface Palette {
  label: ColorValue;
  secondaryLabel: ColorValue;
  tertiaryLabel: ColorValue;
  separator: ColorValue;
  background: ColorValue;
  groupedBackground: ColorValue;
  cell: ColorValue;
  fill: ColorValue;
  tint: ColorValue;
  userBubble: ColorValue;
  code: ColorValue;
  red: ColorValue;
  green: ColorValue;
  dark: boolean;
}

const c = (name: string) => PlatformColor(name);
const palettes: { light?: Palette; dark?: Palette } = {};

export function usePalette(): Palette {
  const dark = useColorScheme() === "dark";
  return (palettes[dark ? "dark" : "light"] ??= {
    label: c("label"),
    secondaryLabel: c("secondaryLabel"),
    tertiaryLabel: c("tertiaryLabel"),
    separator: c("separator"),
    background: c("systemBackground"),
    groupedBackground: c("systemGroupedBackground"),
    cell: c("secondarySystemGroupedBackground"),
    fill: c("tertiarySystemFill"),
    tint: c("systemBlue"),
    // A neutral bubble for the user's messages, as on the desktop.
    userBubble: dark ? "#262626" : "#ececea",
    code: dark ? "rgba(255,255,255,0.08)" : "rgba(0,0,0,0.05)",
    red: c("systemRed"),
    green: c("systemGreen"),
    dark,
  });
}
