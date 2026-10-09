// What the service worker does with a push message and with a tap on a
// notification (SPEC section 6, D-64, D-65). Plain functions with their
// dependencies passed in, so tests run them without a browser; sw.ts wires
// them to the service worker events.
import type { TFunction } from "i18next";

import type { Task } from "../api/types";
import type { Clock } from "../lib/clock";
import { dateIn, dueInstant, parseDay } from "../lib/dates";
import { formatDay, formatTime } from "../lib/format";
import { uuidv7 } from "../lib/ids";

/** The message the server sends (internal/notify/payload.go). */
export interface Payload {
  type: "reminder" | "test";
  task_id?: string;
  reminder_id?: string;
  title?: string;
  list_name?: string;
  is_inbox?: boolean;
  due_date?: string;
  due_time?: string;
  due_tz?: string;
  timezone?: string;
  fire_at?: string;
}

/** What showNotification takes. */
export interface Shown {
  title: string;
  options: {
    body: string;
    tag?: string;
    data: Payload;
    actions?: { action: string; title: string }[];
    icon: string;
    badge: string;
  };
}

/** How long Snooze from a notification waits (D-64). */
export const snoozeMinutes = 10;

const icon = "/icons/icon-192.png";
const badge = "/icons/badge-96.png";

/** The notification for a payload, in the user's language and zone. */
export function notificationFor(
  payload: Payload,
  t: TFunction,
  locale: string,
  clock: Clock,
): Shown {
  if (payload.type === "test") {
    return {
      title: t("notification.testTitle"),
      options: { body: t("notification.testBody"), data: payload, icon, badge },
    };
  }
  const list = payload.is_inbox ? t("nav.inbox") : (payload.list_name ?? "");
  const due = describeDue(payload, t, locale, clock);
  return {
    title: payload.title ?? "",
    options: {
      body: due === "" ? list : t("notification.body", { list, due }),
      // One notification per task: a later reminder replaces the earlier.
      ...(payload.task_id ? { tag: payload.task_id } : {}),
      data: payload,
      actions: [
        { action: "complete", title: t("notification.complete") },
        { action: "snooze", title: t("notification.snooze", { count: snoozeMinutes }) },
      ],
      icon,
      badge,
    },
  };
}

function describeDue(p: Payload, t: TFunction, locale: string, clock: Clock): string {
  if (!p.due_date) return "";
  const zone = p.timezone ?? "UTC";
  // dueInstant reads the same fields as a task.
  const instant = dueInstant(
    { due_date: p.due_date, due_time: p.due_time ?? null, due_tz: p.due_tz ?? null } as Task,
    zone,
  );
  const today = dateIn(clock.now(), zone);
  if (instant === null) {
    const day = parseDay(p.due_date);
    return day.compare(today) === 0 ? t("sections.today") : formatDay(day, zone, locale);
  }
  const time = formatTime(instant, zone, locale);
  const day = dateIn(instant, zone);
  return day.compare(today) === 0
    ? t("notification.todayAt", { time })
    : t("notification.dayAt", { day: formatDay(day, zone, locale), time });
}

/** What the service worker can do, passed in for tests. */
export interface Environment {
  fetch: (url: string, init: RequestInit) => Promise<Response>;
  /** Focuses an open window of the app at url, or opens one. */
  openApp: (url: string) => Promise<void>;
  clock: Clock;
}

/**
 * Handles a tap on a notification or one of its actions. Complete and
 * Snooze call the API with the session cookie; when the session has
 * expired (401), or anything else goes wrong, the app opens at the task so
 * the user can act there. A plain tap opens the task.
 */
export async function handleClick(
  action: string,
  payload: Payload,
  env: Environment,
): Promise<void> {
  const taskURL = payload.task_id ? `/?task=${payload.task_id}` : "/";
  if (
    payload.type !== "reminder" ||
    !payload.task_id ||
    (action !== "complete" && action !== "snooze")
  ) {
    await env.openApp(taskURL);
    return;
  }
  const path = `/api/v1/tasks/${payload.task_id}/${action}`;
  // Ids generated here, once: a retry by the browser repeats the same one
  // (D-04, D-24). occurrence_due_date is the date the reminder was for
  // (D-10): if the task moved on since, completing does nothing.
  const body =
    action === "complete"
      ? { completion_id: uuidv7(env.clock), occurrence_due_date: payload.due_date ?? null }
      : {
          reminder_id: uuidv7(env.clock),
          until: new Date(env.clock.now().getTime() + snoozeMinutes * 60_000).toISOString(),
        };
  let ok: boolean;
  try {
    const response = await env.fetch(path, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    ok = response.ok;
  } catch {
    ok = false;
  }
  if (!ok) {
    await env.openApp(taskURL);
  }
}
