import { t } from "./i18n";

/** A time as a list shows it: the time today, "Yesterday", else the date. */
export function stamp(ms: number, now = Date.now()): string {
  if (!ms) return "";
  const d = new Date(ms);
  const today = new Date(now);
  today.setHours(0, 0, 0, 0);
  if (d >= today) return d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
  if (d.getTime() >= today.getTime() - 86400000) return t("Yesterday");
  return d.toLocaleDateString([], { month: "short", day: "numeric" });
}
