import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, test } from "vitest";

import { json, noContent } from "../test/fakeFetch";
import { FakePush } from "../test/fakePush";
import { renderApp, testUser } from "../test/render";

const device = (id: string, label: string) => ({
  id,
  channel: "webpush",
  label,
  created_at: "2026-10-01T10:00:00Z",
  last_success_at: null,
  disabled_at: null,
});

beforeEach(() => {
  window.localStorage.clear();
});

test("turning on notifications subscribes with the server's key and registers the device", async () => {
  const push = new FakePush();
  const { server, user } = await renderApp({
    path: "/settings",
    push,
    setup: (s) => {
      s.on(
        "GET",
        "/push/subscriptions",
        json({ items: [] }),
        json({ items: [device("d1", "Chrome on Linux")] }),
      );
      s.on("GET", "/push/vapid-public-key", json({ public_key: "BPUBLIC" }));
      s.on("POST", "/push/subscriptions", (r) =>
        json(device((r.body as { id: string }).id, "Chrome on Linux"), 201),
      );
    },
  });
  expect(await screen.findByText("Reminders are not shown on this device.")).toBeDefined();
  await user.click(screen.getByRole("button", { name: "Turn on notifications" }));
  await waitFor(() => {
    expect(server.calls("POST", "/push/subscriptions")).toHaveLength(1);
  });
  expect(push.subscribedWith).toBe("BPUBLIC");
  expect(server.calls("POST", "/push/subscriptions")[0]?.body).toMatchObject({
    channel: "webpush",
    endpoint: "https://push.example.com/abc",
    keys: { p256dh: "BKEY", auth: "AUTH" },
    label: "Chrome on Linux", // D-69
  });
});

test("a blocked permission is explained, without a button", async () => {
  const push = new FakePush();
  push.state = "denied";
  await renderApp({
    path: "/settings",
    push,
    setup: (s) => s.on("GET", "/push/subscriptions", json({ items: [] })),
  });
  expect(await screen.findByText(/blocked in this browser/)).toBeDefined();
  expect(screen.queryByRole("button", { name: "Turn on notifications" })).toBeNull();
});

test("the device list marks this device and offers a test and removal", async () => {
  window.localStorage.setItem("brinketask.deviceId", "d1");
  const { server, user } = await renderApp({
    path: "/settings",
    setup: (s) => {
      s.on(
        "GET",
        "/push/subscriptions",
        json({ items: [device("d1", "Chrome on Linux"), device("d2", "Firefox on Android")] }),
      );
      s.on("POST", "/push/subscriptions/d2/test", new Response(null, { status: 202 }));
      s.on("DELETE", "/push/subscriptions/d2", noContent());
    },
  });
  const mine = (await screen.findByText("Chrome on Linux")).closest("li");
  expect(mine && within(mine).getByText("This device")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Send a test to Firefox on Android" }));
  expect(await screen.findByText("Test notification sent")).toBeDefined();
  await user.click(screen.getByRole("button", { name: "Remove Firefox on Android" }));
  await waitFor(() => {
    expect(server.calls("DELETE", "/push/subscriptions/d2")).toHaveLength(1);
  });
});

test("the time of all-day reminders is saved, then reminders are pulled again", async () => {
  const { server, user } = await renderApp({
    path: "/settings",
    setup: (s) => {
      s.on("GET", "/push/subscriptions", json({ items: [] }));
      s.on("PATCH", "/me", (r) => json({ ...testUser, ...(r.body as object) }));
    },
  });
  const hour = await screen.findByRole("spinbutton", { name: /hour/i });
  await user.click(hour);
  await user.keyboard("07");
  await user.tab();
  await user.tab();
  await waitFor(() => {
    expect(server.calls("PATCH", "/me")[0]?.body).toEqual({ all_day_reminder_time: "07:00" });
  });
  await waitFor(() => {
    expect(server.calls("GET", "/sync/changes").length).toBeGreaterThan(1);
  });
});

// D-32: on start, a subscribed browser registers again for whoever signed in.
test("a subscribed browser registers again on start", async () => {
  const push = new FakePush();
  push.state = "granted";
  push.subscription = { endpoint: "https://push.example.com/old", p256dh: "K", auth: "A" };
  const { server } = await renderApp({
    push,
    setup: (s) =>
      s.on("POST", "/push/subscriptions", (r) => json(device((r.body as { id: string }).id, "x"))),
  });
  await waitFor(() => {
    expect(server.calls("POST", "/push/subscriptions")[0]?.body).toMatchObject({
      endpoint: "https://push.example.com/old",
    });
  });
});
