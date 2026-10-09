// A fake of the browser's push APIs for tests.
import type { BrowserSubscription, PushEnvironment } from "../push/browser";

export class FakePush implements PushEnvironment {
  supported = true;
  state: NotificationPermission = "default";
  /** What requestPermission answers. */
  answer: NotificationPermission = "granted";
  subscription: BrowserSubscription | null = null;
  subscribedWith: string | null = null;

  permission(): NotificationPermission {
    return this.state;
  }

  requestPermission(): Promise<NotificationPermission> {
    this.state = this.answer;
    return Promise.resolve(this.state);
  }

  current(): Promise<BrowserSubscription | null> {
    return Promise.resolve(this.subscription);
  }

  subscribe(key: string): Promise<BrowserSubscription> {
    this.subscribedWith = key;
    this.subscription = { endpoint: "https://push.example.com/abc", p256dh: "BKEY", auth: "AUTH" };
    return Promise.resolve(this.subscription);
  }

  unsubscribe(): Promise<void> {
    this.subscription = null;
    return Promise.resolve();
  }

  describe(): { browser: string; os: string } {
    return { browser: "Chrome", os: "Linux" };
  }
}
