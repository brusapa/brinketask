// Registering this browser for reminders, and the user's devices (SPEC
// section 9, settings).
import type { TFunction } from "i18next";

import { unwrap, type Api } from "../api/client";
import type { Clock } from "../lib/clock";
import { uuidv7 } from "../lib/ids";
import type { PushEnvironment } from "./browser";

/** Where this browser remembers the id of its device row. */
const deviceKey = "brinketask.deviceId";

export function thisDeviceId(): string | null {
  try {
    return window.localStorage.getItem(deviceKey);
  } catch {
    return null;
  }
}

function rememberDevice(id: string | null): void {
  try {
    if (id === null) window.localStorage.removeItem(deviceKey);
    else window.localStorage.setItem(deviceKey, id);
  } catch {
    // Without storage the list just cannot mark this device.
  }
}

/** The label of this browser, e.g. "Chrome on Linux" (D-69). */
export function deviceLabel(push: PushEnvironment, t: TFunction): string {
  const { browser, os } = push.describe();
  if (browser !== "" && os !== "") return t("devices.label", { browser, os });
  return browser || os || t("devices.unknown");
}

/**
 * Sends this browser's subscription to the server. Registering is
 * idempotent on the endpoint, so this also refreshes keys and moves the
 * device to whoever is signed in now (D-32).
 */
async function register(
  api: Api,
  clock: Clock,
  label: string,
  sub: { endpoint: string; p256dh: string; auth: string },
) {
  const device = await unwrap(() =>
    api.POST("/push/subscriptions", {
      body: {
        id: thisDeviceId() ?? uuidv7(clock),
        channel: "webpush",
        endpoint: sub.endpoint,
        keys: { p256dh: sub.p256dh, auth: sub.auth },
        label,
      },
    }),
  );
  rememberDevice(device.id);
  return device;
}

/**
 * Turns on notifications here: asks for permission, subscribes with the
 * server's key and registers the device. Returns the permission the user
 * gave.
 */
export async function enableNotifications(
  api: Api,
  push: PushEnvironment,
  clock: Clock,
  label: string,
): Promise<NotificationPermission> {
  const permission = await push.requestPermission();
  if (permission !== "granted") return permission;
  const { public_key } = await unwrap(() => api.GET("/push/vapid-public-key"));
  const sub = (await push.current()) ?? (await push.subscribe(public_key));
  await register(api, clock, label, sub);
  return permission;
}

/** Turns notifications off here: unsubscribes and deletes the device. */
export async function disableNotifications(api: Api, push: PushEnvironment): Promise<void> {
  await push.unsubscribe();
  const id = thisDeviceId();
  rememberDevice(null);
  if (id !== null) {
    await unwrap(() => api.DELETE("/push/subscriptions/{id}", { params: { path: { id } } })).catch(
      () => undefined,
    );
  }
}

/**
 * On start: if this browser is subscribed and allowed, register it again,
 * so a device that changed keys or users keeps receiving the right
 * reminders (D-32). Failures are ignored; the next start tries again.
 */
export async function refreshRegistration(
  api: Api,
  push: PushEnvironment,
  clock: Clock,
  label: string,
): Promise<void> {
  if (!push.supported || push.permission() !== "granted") return;
  const sub = await push.current().catch(() => null);
  if (sub === null) return;
  await register(api, clock, label, sub).catch(() => undefined);
}
