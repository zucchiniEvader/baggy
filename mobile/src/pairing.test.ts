import { expect, test } from "bun:test";
import { baseURL, parsePairing } from "./pairing";
import { colorOf } from "./model";

const token = "a".repeat(64);

test("parses the desktop's code", () => {
  const p = parsePairing(`baggy://pair?host=192.168.1.5&name=Spike+Mac&port=7841&token=${token}`);
  expect(p).toEqual({ host: "192.168.1.5", port: 7841, token, name: "Spike Mac" });
  expect(baseURL(p)).toBe("http://192.168.1.5:7841");
});

test("refuses what is not one", () => {
  expect(() => parsePairing("https://example.com")).toThrow();
  expect(() => parsePairing(`baggy://pair?host=1.2.3.4&port=0&token=${token}`)).toThrow();
  expect(() => parsePairing("baggy://pair?host=1.2.3.4&port=7841&token=short")).toThrow();
});

test("a bot's color follows the desktop", () => {
  expect(colorOf({ id: "x", color: "teal" })).toBe("#30b0c7");
  // Picked by FNV-1a of the id, as desktop/avatar.go picks it.
  expect(colorOf({ id: "b-1", color: "" })).toBe("#30b0c7"); // teal
  expect(colorOf({ id: "bot_abc123", color: "" })).toBe("#5856d6"); // indigo
});
