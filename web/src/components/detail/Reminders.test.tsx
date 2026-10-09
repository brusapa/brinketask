import { screen, waitFor, within } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import { i18n } from "../../i18n";
import { json, noContent } from "../../test/fakeFetch";
import { reminder, task } from "../../test/fixtures";
import { inboxId, renderApp } from "../../test/render";
import { describeOffset } from "./RemindersField";

const t = i18n.t.bind(i18n);
const id = "00000000-0000-7000-8000-0000000000e1";

describe("describeOffset", () => {
  test.each([
    [0, true, "At the due time"],
    [0, false, "On the day at 9:00 AM"],
    [1, true, "1 minute before"],
    [15, true, "15 minutes before"],
    [60, true, "1 hour before"],
    [120, true, "2 hours before"],
    [1440, false, "1 day before"],
    [2880, false, "2 days before"],
    [90, true, "90 minutes before"],
  ])("%d minutes, timed %s", (offset, timed, text) => {
    expect(describeOffset(offset, timed, "09:00", t, "en")).toBe(text);
  });
});

test("the detail lists reminders, adds a preset and removes one", async () => {
  const base = task({
    id,
    list_id: inboxId,
    title: "Dentist",
    due_date: "2026-10-12",
    due_time: "10:00",
  });
  const existing = reminder({
    id: "r1",
    task_id: id,
    offset_minutes: 60,
    next_fire_at: "2026-10-12T07:00:00Z",
  });
  const { server, user } = await renderApp({
    path: `/?task=${id}`,
    state: { tasks: [base], reminders: [existing] },
    setup: (s) => {
      s.on("POST", `/tasks/${id}/reminders`, (r) =>
        json(
          reminder({ ...(r.body as object), id: (r.body as { id: string }).id, task_id: id }),
          201,
        ),
      );
      s.on("DELETE", "/reminders/r1", noContent());
    },
  });
  const panel = await screen.findByRole("complementary", { name: "Task details" });
  expect(within(panel).getByText("1 hour before")).toBeDefined();
  // The row of a task with a pending reminder says so.
  expect(
    within(screen.getByRole("row", { name: /Dentist/ })).getByRole("img", {
      name: "Has a reminder",
    }),
  ).toBeDefined();

  await user.click(within(panel).getByRole("button", { name: "Add reminder" }));
  await user.click(await screen.findByRole("menuitem", { name: "15 minutes before" }));
  await waitFor(() => {
    expect(server.calls("POST", `/tasks/${id}/reminders`)[0]?.body).toMatchObject({
      kind: "relative",
      offset_minutes: 15,
    });
  });

  await user.click(within(panel).getByRole("button", { name: "Remove reminder: 1 hour before" }));
  await waitFor(() => {
    expect(server.calls("DELETE", "/reminders/r1")).toHaveLength(1);
  });
});

test("an all-day task offers day presets; an undated one only a date and time", async () => {
  const allDay = task({ id, list_id: inboxId, title: "Bins", due_date: "2026-10-12" });
  const { user } = await renderApp({ path: `/?task=${id}`, state: { tasks: [allDay] } });
  const panel = await screen.findByRole("complementary", { name: "Task details" });
  await user.click(within(panel).getByRole("button", { name: "Add reminder" }));
  const items = (await screen.findAllByRole("menuitem")).map((i) => i.textContent);
  expect(items).toEqual([
    "On the day at 9:00 AM",
    "1 day before",
    "2 days before",
    "At a date and time…",
  ]);
});

// D-64: snooze from the detail.
test("Snooze 1 hour asks the server to remind again then", async () => {
  const base = task({ id, list_id: inboxId, title: "Call" });
  const { server, user } = await renderApp({
    path: `/?task=${id}`,
    state: { tasks: [base] },
    setup: (s) =>
      s.on("POST", `/tasks/${id}/snooze`, (r) =>
        json(
          reminder({
            id: (r.body as { reminder_id: string }).reminder_id,
            task_id: id,
            kind: "snooze",
            offset_minutes: null,
            at: (r.body as { until: string }).until,
            next_fire_at: (r.body as { until: string }).until,
          }),
          201,
        ),
      ),
  });
  const panel = await screen.findByRole("complementary", { name: "Task details" });
  await user.click(within(panel).getByRole("button", { name: "Snooze" }));
  await user.click(await screen.findByRole("menuitem", { name: "1 hour" }));
  await waitFor(() => {
    expect(server.calls("POST", `/tasks/${id}/snooze`)[0]?.body).toMatchObject({
      until: "2026-10-09T11:00:00.000Z",
    });
  });
  // 13:00 in Madrid.
  expect(
    await screen.findByText("Snoozed until Fri, Oct 9 at 1:00 PM", { selector: ".toast span" }),
  ).toBeDefined();
});
