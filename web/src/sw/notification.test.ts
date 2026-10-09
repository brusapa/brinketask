import { describe, expect, test } from "vitest";

import { i18n } from "../i18n";
import { FixedClock } from "../lib/clock";
import { handleClick, notificationFor, type Payload } from "./notification";

const t = i18n.t.bind(i18n);
// Friday 2026-10-09, 12:00 in Madrid.
const clock = new FixedClock("2026-10-09T10:00:00Z");
const taskId = "00000000-0000-7000-8000-0000000000c1";

const base: Payload = {
  type: "reminder",
  task_id: taskId,
  reminder_id: "00000000-0000-7000-8000-0000000000d1",
  title: "Call the plumber",
  list_name: "Home",
  timezone: "Europe/Madrid",
};

// D-65: the text is written here, in the user's language and zone.
describe("notificationFor", () => {
  test("a timed task due today", () => {
    const n = notificationFor(
      { ...base, due_date: "2026-10-09", due_time: "17:30" },
      t,
      "en",
      clock,
    );
    expect(n.title).toBe("Call the plumber");
    expect(n.options.body).toBe("Home · Today at 5:30 PM");
    expect(n.options.tag).toBe(taskId);
    // D-64: two actions.
    expect(n.options.actions?.map((a) => a.title)).toEqual(["Complete", "Snooze 10 min"]);
  });

  test("another day, a fixed zone shown in the user's", () => {
    const n = notificationFor(
      { ...base, due_date: "2026-10-12", due_time: "09:00", due_tz: "America/New_York" },
      t,
      "en",
      clock,
    );
    expect(n.options.body).toBe("Home · Mon, Oct 12 at 3:00 PM");
  });

  test("an all-day task in the inbox, whose internal name is not shown", () => {
    const n = notificationFor(
      { ...base, list_name: "Inbox", is_inbox: true, due_date: "2026-10-09" },
      t,
      "en",
      clock,
    );
    expect(n.options.body).toBe("Inbox · Today");
  });

  test("no due date: the list only", () => {
    expect(notificationFor(base, t, "en", clock).options.body).toBe("Home");
  });

  test("the test message", () => {
    const n = notificationFor({ type: "test" }, t, "en", clock);
    expect(n.title).toBe("Notifications are on");
    expect(n.options.actions).toBeUndefined();
  });
});

function environment(status: number | "network") {
  const calls: { url: string; body: unknown }[] = [];
  const opened: string[] = [];
  return {
    calls,
    opened,
    env: {
      clock,
      fetch: (url: string, init: RequestInit) => {
        calls.push({ url, body: JSON.parse(init.body as string) });
        if (status === "network") return Promise.reject(new TypeError("offline"));
        return Promise.resolve(new Response(null, { status }));
      },
      openApp: (url: string) => {
        opened.push(url);
        return Promise.resolve();
      },
    },
  };
}

describe("handleClick", () => {
  test("Complete sends the occurrence the reminder was for (D-10)", async () => {
    const { calls, opened, env } = environment(200);
    await handleClick("complete", { ...base, due_date: "2026-10-09" }, env);
    expect(calls).toHaveLength(1);
    expect(calls[0]?.url).toBe(`/api/v1/tasks/${taskId}/complete`);
    expect(calls[0]?.body).toMatchObject({ occurrence_due_date: "2026-10-09" });
    expect((calls[0]?.body as { completion_id: string }).completion_id).toMatch(/^[0-9a-f-]{36}$/);
    expect(opened).toEqual([]);
  });

  test("Snooze asks again in 10 minutes", async () => {
    const { calls, env } = environment(201);
    await handleClick("snooze", base, env);
    expect(calls[0]?.url).toBe(`/api/v1/tasks/${taskId}/snooze`);
    expect(calls[0]?.body).toMatchObject({ until: "2026-10-09T10:10:00.000Z" });
  });

  test.each([401, 409, "network"] as const)("on %s the app opens at the task", async (status) => {
    const { opened, env } = environment(status);
    await handleClick("complete", base, env);
    expect(opened).toEqual([`/?task=${taskId}`]);
  });

  test("a tap on the notification opens the task", async () => {
    const { calls, opened, env } = environment(200);
    await handleClick("", base, env);
    expect(calls).toHaveLength(0);
    expect(opened).toEqual([`/?task=${taskId}`]);
  });
});
