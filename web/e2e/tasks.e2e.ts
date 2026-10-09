// The end-to-end path of SPEC section 11: create, complete, undo and
// receive a reminder, in a real browser against the built image.
import { expect, test, type Locator, type Page } from "@playwright/test";

import { fakePushURL } from "./env";

// A suffix unique to this run, so tasks of an earlier run against the same
// database never match.
const run = Date.now().toString(36);

/**
 * Clicks a task's checkbox where the user does: on the box React Aria
 * draws. Its <input> is visually hidden, so a click at the input's place
 * lands on the row.
 */
async function clickCheckbox(row: Locator) {
  await row.locator(".checkbox-button").click();
}

/** The open-task row with this title in the current view. */
function openRow(page: Page, title: string) {
  return page
    .getByRole("row")
    .filter({ hasText: title })
    .filter({ has: page.getByRole("checkbox", { name: "Complete task" }) });
}

async function openInbox(page: Page) {
  await page.goto("/");
  await page
    .getByRole("navigation")
    .getByRole("link", { name: /^Inbox/ })
    .click();
  await expect(page.getByRole("textbox", { name: "Add a task" })).toBeVisible();
}

test("create, complete and undo a task", async ({ page }) => {
  const title = `Water the plants ${run}`;
  await openInbox(page);

  await page.getByRole("textbox", { name: "Add a task" }).fill(title);
  const created = page.waitForResponse(
    (r) => r.request().method() === "POST" && r.url().endsWith("/api/v1/tasks"),
  );
  await page.keyboard.press("Enter");
  expect((await created).status()).toBe(201);
  await expect(openRow(page, title)).toBeVisible();

  // It is on the server: it survives a reload.
  await page.reload();
  await expect(openRow(page, title)).toBeVisible();

  const completed = page.waitForResponse((r) => r.url().endsWith("/complete"));
  await clickCheckbox(openRow(page, title));
  expect((await completed).status()).toBe(200);
  await expect(openRow(page, title)).toBeHidden();
  await expect(page.getByRole("status")).toContainText("Task completed");

  const undone = page.waitForResponse((r) => r.url().endsWith("/uncomplete"));
  await page.getByRole("status").getByRole("button", { name: "Undo" }).click();
  expect((await undone).status()).toBe(200);
  await expect(openRow(page, title)).toBeVisible();

  // The undo reached the server too.
  await page.reload();
  await expect(openRow(page, title)).toBeVisible();
});

interface PushMessage {
  device: string;
  payload: string;
}

test("receive a reminder", async ({ page, context, request }) => {
  const title = `Call the plumber ${run}`;
  await openInbox(page);
  // The service worker shows notifications; wait until it controls the page.
  await page.evaluate(async () => {
    await navigator.serviceWorker.ready;
  });

  // A device. Headless Chromium has no push service, so the fake one makes
  // the subscription, and the page registers it as the web client does
  // after PushManager.subscribe (D-73).
  const device = `e2e-${run}`;
  const subscription = (await (await request.post(`${fakePushURL}/devices/${device}`)).json()) as {
    endpoint: string;
    keys: { p256dh: string; auth: string };
  };
  // A task with a reminder three seconds from now, written through the API
  // from the page so the session cookie and same-origin headers are the
  // web client's.
  const status = await page.evaluate(
    async ({ subscription, title }) => {
      const post = (path: string, body: unknown) =>
        fetch(`/api/v1${path}`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        }).then((r) => r.status);
      const me = (await (await fetch("/api/v1/me")).json()) as { inbox_list_id: string };
      return [
        await post("/push/subscriptions", {
          id: crypto.randomUUID(),
          channel: "webpush",
          ...subscription,
        }),
        await post("/tasks", {
          id: crypto.randomUUID(),
          list_id: me.inbox_list_id,
          title,
          position: "zz",
          reminders: [
            {
              id: crypto.randomUUID(),
              kind: "absolute",
              at: new Date(Date.now() + 3000).toISOString(),
            },
          ],
        }),
      ];
    },
    { subscription, title },
  );
  expect(status).toEqual([201, 201]);

  // The server fires it, encrypts it and signs it; the fake push service
  // checks the signature and decrypts it as a browser would.
  let message: PushMessage | undefined;
  await expect
    .poll(
      async () => {
        const messages = (await (
          await request.get(`${fakePushURL}/messages`)
        ).json()) as PushMessage[];
        message = messages.find((m) => m.device === device);
        return message !== undefined;
      },
      { timeout: 30_000 },
    )
    .toBe(true);
  expect(await (await request.get(`${fakePushURL}/failures`)).json()).toEqual([]);
  expect(JSON.parse(message?.payload ?? "{}")).toMatchObject({ type: "reminder", title });

  // The browser's push service would now wake the service worker with the
  // message; DevTools does the same.
  const cdp = await context.newCDPSession(page);
  const registrationId = new Promise<string>((resolve) => {
    cdp.on("ServiceWorker.workerRegistrationUpdated", ({ registrations }) => {
      const live = registrations.find((r) => !r.isDeleted);
      if (live) resolve(live.registrationId);
    });
  });
  await cdp.send("ServiceWorker.enable");
  await cdp.send("ServiceWorker.deliverPushMessage", {
    origin: new URL(page.url()).origin,
    registrationId: await registrationId,
    data: message?.payload ?? "",
  });

  // The notification the user sees, with its actions (D-64).
  await expect
    .poll(() =>
      page.evaluate(async () => {
        const registration = await navigator.serviceWorker.ready;
        const shown = await registration.getNotifications();
        return shown.map((n) => ({
          title: n.title,
          body: n.body,
          // `actions` is not in TypeScript's DOM types yet.
          actions: (n as Notification & { actions: { title: string }[] }).actions.map(
            (a) => a.title,
          ),
        }));
      }),
    )
    .toContainEqual({
      title,
      body: expect.stringContaining("Inbox"),
      actions: ["Complete", "Snooze 10 min"],
    });

  // The task the reminder belongs to is in the inbox.
  await page.reload();
  await expect(page.getByRole("row").filter({ hasText: title })).toBeVisible();
});
