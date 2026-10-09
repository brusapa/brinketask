// The browser's push APIs behind a small interface, so the app can be
// tested without them (they need a real browser and a push service).
import { b64ToBytes } from "./base64";

/** A subscription as PushSubscription.toJSON() gives it. */
export interface BrowserSubscription {
  endpoint: string;
  p256dh: string;
  auth: string;
}

export interface PushEnvironment {
  /** Whether this browser can receive Web Push at all. */
  supported: boolean;
  permission(): NotificationPermission;
  requestPermission(): Promise<NotificationPermission>;
  /** The current subscription of this browser, if any. */
  current(): Promise<BrowserSubscription | null>;
  /** Subscribes with the server's VAPID public key (base64url). */
  subscribe(vapidPublicKey: string): Promise<BrowserSubscription>;
  unsubscribe(): Promise<void>;
  /** The browser and system, e.g. { browser: "Chrome", os: "Linux" } (D-69). */
  describe(): { browser: string; os: string };
}

export function browserPush(): PushEnvironment {
  const supported =
    "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
  const registration = () => navigator.serviceWorker.ready;
  return {
    supported,
    permission: () => (supported ? Notification.permission : "denied"),
    requestPermission: () => Notification.requestPermission(),
    current: async () => {
      const sub = await (await registration()).pushManager.getSubscription();
      return sub === null ? null : fromJSON(sub);
    },
    subscribe: async (key) => {
      const sub = await (
        await registration()
      ).pushManager.subscribe({
        // Chrome requires every push to show a notification.
        userVisibleOnly: true,
        applicationServerKey: b64ToBytes(key),
      });
      return fromJSON(sub);
    },
    unsubscribe: async () => {
      const sub = await (await registration()).pushManager.getSubscription();
      await sub?.unsubscribe();
    },
    describe: () => describeUserAgent(navigator.userAgent),
  };
}

function fromJSON(sub: PushSubscription): BrowserSubscription {
  const json = sub.toJSON();
  return { endpoint: sub.endpoint, p256dh: json.keys?.p256dh ?? "", auth: json.keys?.auth ?? "" };
}

/**
 * Names the browser and system from the user agent string. Product names
 * are not translated; an unknown one is "".
 */
export function describeUserAgent(ua: string): { browser: string; os: string } {
  let browser = "";
  if (/Edg\//.test(ua)) browser = "Edge";
  else if (/Firefox\//.test(ua)) browser = "Firefox";
  else if (/Chrome\//.test(ua)) browser = "Chrome";
  else if (/Safari\//.test(ua)) browser = "Safari";
  let os = "";
  if (/Android/.test(ua)) os = "Android";
  else if (/iPhone|iPad/.test(ua)) os = "iOS";
  else if (/CrOS/.test(ua)) os = "ChromeOS";
  else if (/Mac OS X/.test(ua)) os = "macOS";
  else if (/Windows/.test(ua)) os = "Windows";
  else if (/Linux/.test(ua)) os = "Linux";
  return { browser, os };
}
