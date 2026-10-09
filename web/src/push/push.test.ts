import { expect, test } from "vitest";

import { b64ToBytes } from "./base64";
import { describeUserAgent } from "./browser";

test.each([
  [
    "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0 Safari/537.36",
    "Chrome",
    "Linux",
  ],
  [
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0 Safari/537.36 Edg/141.0",
    "Edge",
    "Windows",
  ],
  ["Mozilla/5.0 (Macintosh; Intel Mac OS X 15_0) Gecko/20100101 Firefox/144.0", "Firefox", "macOS"],
  [
    "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1",
    "Safari",
    "iOS",
  ],
  [
    "Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0 Mobile Safari/537.36",
    "Chrome",
    "Android",
  ],
  ["curl/8.0", "", ""],
])("%s", (ua, browser, os) => {
  expect(describeUserAgent(ua)).toEqual({ browser, os });
});

test("base64url keys decode to bytes", () => {
  expect([...b64ToBytes("AQID_-8")]).toEqual([1, 2, 3, 255, 239]);
});
