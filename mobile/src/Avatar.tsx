import { Image } from "expo-image";
import { Text, View } from "react-native";
import { colorOf, type Bot } from "./model";
import { avatarURL } from "./store";
import { usePalette } from "./theme";

/** A bot's avatar: its own picture in a circle, else its initial in white on its color. */
export function Avatar({ bot, size = 40 }: { bot?: Bot; size?: number }) {
  const round = { width: size, height: size, borderRadius: size / 2 };
  const url = bot?.avatar ? avatarURL(bot.avatar) : undefined;
  if (url) return <Image source={{ uri: url }} style={round} contentFit="cover" />;
  return (
    <View style={[round, { backgroundColor: bot ? colorOf(bot) : "#8e8e93", alignItems: "center", justifyContent: "center" }]}>
      <Text style={{ color: "#fff", fontSize: size * 0.42, fontWeight: "600" }}>{(bot?.name ?? "?").slice(0, 1).toUpperCase()}</Text>
    </View>
  );
}

/** A group's avatar: its first two members, overlapping. */
export function GroupAvatar({ bots, size = 40 }: { bots: (Bot | undefined)[]; size?: number }) {
  const p = usePalette();
  if (bots.length < 2) return <Avatar bot={bots[0]} size={size} />;
  const small = size * 0.68;
  return (
    <View style={{ width: size, height: size }}>
      <View style={{ position: "absolute", left: 0, top: 0 }}>
        <Avatar bot={bots[0]} size={small} />
      </View>
      <View style={{ position: "absolute", right: 0, bottom: 0, borderRadius: small, borderWidth: 2, borderColor: p.background }}>
        <Avatar bot={bots[1]} size={small} />
      </View>
    </View>
  );
}
