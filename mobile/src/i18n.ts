// English in the code, Chinese from this catalog when the phone prefers it.

import { getLocales } from "expo-localization";

const zh: Record<string, string> = {
  Chats: "聊天",
  Settings: "设置",
  Done: "完成",
  "Pair with Your Computer": "与电脑配对",
  "Open Baggy on your computer, go to Settings › Phone, turn on Connect a phone, and scan the code.":
    "在电脑上打开 Baggy,进入 设置 › 手机,打开“连接手机”,然后扫描二维码。",
  "Scan Code": "扫描二维码",
  "Paste Code": "粘贴配对码",
  "Pairing failed": "配对失败",
  "Camera access needed": "需要相机权限",
  "Allow the camera to scan the pairing code, or paste the code instead.": "允许使用相机来扫描配对码,或改为粘贴配对码。",
  Cancel: "取消",
  "Connecting…": "正在连接…",
  "Can't connect": "无法连接",
  "Pair again": "需要重新配对",
  "Try Again": "重试",
  "Working…": "工作中…",
  "%s is working…": "%s 工作中…",
  "Running %s…": "正在运行 %s…",
  "Retrying…": "正在重试…",
  Message: "消息",
  "Message %s": "发消息给 %s",
  Stop: "停止",
  You: "你",
  "No chats yet": "还没有聊天",
  "Create a bot in Baggy on your computer.": "在电脑上的 Baggy 里创建一个 bot。",
  "Used %s": "用了 %s",
  "Summarized the earlier conversation": "已总结之前的对话",
  "Stopped": "已停止",
  "The reply failed": "回复失败",
  Computer: "电脑",
  Address: "地址",
  Status: "状态",
  Connected: "已连接",
  Unpair: "取消配对",
  "Unpair this phone?": "取消这台手机的配对?",
  "You can pair again by scanning the code.": "之后可以扫码重新配对。",
  "Not sent": "未发送",
  "%d members": "%d 个成员",
  Yesterday: "昨天",
};

const chinese = getLocales()[0]?.languageCode === "zh";

/** The phrase in the phone's language, with %s and %d filled in order. */
export function t(s: string, ...args: (string | number)[]): string {
  let out = (chinese && zh[s]) || s;
  for (const a of args) out = out.replace(/%[sd]/, String(a));
  return out;
}
