// The pairing code the desktop's Settings › Phone shows: baggy://pair?host=…&port=…&token=…&name=…

export interface Pairing {
  host: string;
  port: number;
  token: string;
  name: string;
}

export function parsePairing(text: string): Pairing {
  const s = text.trim();
  const q = s.indexOf("?");
  if (!/^baggy:\/\/pair\?/.test(s) || q < 0) throw new Error("This is not a Baggy pairing code.");
  const params = new Map<string, string>();
  for (const kv of s.slice(q + 1).split("&")) {
    const i = kv.indexOf("=");
    if (i > 0) params.set(decodeURIComponent(kv.slice(0, i)), decodeURIComponent(kv.slice(i + 1).replace(/\+/g, " ")));
  }
  const host = params.get("host") ?? "";
  const port = Number(params.get("port"));
  const token = params.get("token") ?? "";
  if (!host || !Number.isInteger(port) || port <= 0 || port > 65535 || !/^[0-9a-f]{64}$/.test(token)) {
    throw new Error("The pairing code is incomplete.");
  }
  return { host, port, token, name: params.get("name") ?? host };
}

export function baseURL(p: Pairing): string {
  const host = p.host.includes(":") ? `[${p.host}]` : p.host;
  return `http://${host}:${p.port}`;
}
