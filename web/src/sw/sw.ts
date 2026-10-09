// The service worker (SPEC section 2): it receives Web Push messages and
// shows reminders, and handles taps on them. Built on its own into
// dist/sw.js (vite.sw.config.ts) and served from the root, so its scope is
// the whole app. The logic is in notification.ts.
/// <reference lib="webworker" />
import { createI18n } from "../i18n/core";
import { systemClock } from "../lib/clock";
import { handleClick, notificationFor, type Payload } from "./notification";

// In a service worker `self` is the ServiceWorkerGlobalScope; TypeScript's
// DOM types call it Window, hence the cast.
const sw = self as unknown as ServiceWorkerGlobalScope;
const i18n = createI18n();

sw.addEventListener("install", () => {
  // Take over at once instead of waiting for every tab to close: the
  // worker has no cache whose versions could clash.
  void sw.skipWaiting();
});

sw.addEventListener("activate", (event) => {
  event.waitUntil(sw.clients.claim());
});

sw.addEventListener("push", (event) => {
  let payload: Payload;
  try {
    payload = event.data?.json() as Payload;
  } catch {
    return;
  }
  const { title, options } = notificationFor(payload, i18n.t, i18n.language, systemClock);
  // waitUntil keeps the worker alive until the notification is shown;
  // browsers penalize a push that shows nothing.
  event.waitUntil(sw.registration.showNotification(title, options));
});

sw.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const payload = event.notification.data as Payload;
  event.waitUntil(
    handleClick(event.action, payload, {
      fetch: (url, init) => fetch(url, init),
      clock: systemClock,
      openApp: async (url) => {
        const windows = await sw.clients.matchAll({ type: "window", includeUncontrolled: true });
        const open = windows[0];
        if (open !== undefined) {
          await open.navigate(url);
          await open.focus();
        } else {
          await sw.clients.openWindow(url);
        }
      },
    }),
  );
});
