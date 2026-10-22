// Reproductions of findings in REVIEW.md that are confirmed. Each test
// describes the behaviour the specification asks for and fails until the
// finding is fixed.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { Actions } from "./data/actions";
import { Replica } from "./data/replica";
import { Syncer } from "./data/sync";
import { FixedClock } from "./lib/clock";
import { FakeServer, json } from "./test/fakeFetch";
import { list, page, reminder, task, tombstone } from "./test/fixtures";
import { inbox, inboxId, renderApp, testUser } from "./test/render";

const taskId = "00000000-0000-7000-8000-0000000000c1";

// M9 (D-05, D-31): the detail panel shows what a focus refresh brought,
// and leaving its fields untouched does not write the old values back over
// another device's edit.
test("M9: a refreshed task is not overwritten by stale drafts in the detail panel", async () => {
  const before = task({
    id: taskId,
    list_id: inboxId,
    title: "Plan trip",
    description: "old notes",
  });
  const after = { ...before, title: "Plan holiday", description: "new notes", version: 2 };
  const { server, user } = await renderApp({
    path: `/?task=${taskId}`,
    state: { tasks: [before] },
  });
  server.on("PATCH", `/tasks/${taskId}`, (r) =>
    json({ ...after, ...(r.body as object), version: 3 }),
  );
  const panel = await screen.findByRole("complementary", { name: "Task details" });

  // Another device edited the task; this window regains focus and syncs.
  server.on("GET", "/sync/changes", json(page({ tasks: [after], next_cursor: "c2" })));
  fireEvent(window, new Event("focus"));
  await within(panel).findByText("new notes");

  // Visiting the fields without typing anything.
  await user.click(within(panel).getByRole("textbox", { name: "Title" }));
  await user.tab();
  await user.click(within(panel).getByRole("button", { name: "Edit description" }));
  await user.tab();

  const bodies = server.calls("PATCH", `/tasks/${taskId}`).map((c) => c.body);
  expect(bodies).not.toContainEqual({ title: "Plan trip" });
  expect(bodies).not.toContainEqual({ description: "old notes" });
});

// M10 (R-8, D-31): completing a recurring task deletes its absolute
// reminders on the server; the replica must not keep showing them.
test("M10: reminders deleted by completing are gone from the replica", async () => {
  const server = new FakeServer();
  const replica = new Replica();
  const api = server.api();
  const actions = new Actions({
    api,
    replica,
    syncer: new Syncer(api, replica),
    clock: new FixedClock("2026-10-09T10:00:00Z"),
    zone: () => "Europe/Madrid",
    onCompletionsChanged: () => undefined,
    onError: () => undefined,
  });
  replica.replaceAll([page({ lists: [list({ id: "inbox", is_inbox: true })] })], "c0");
  const absolute = reminder({
    id: "r1",
    task_id: "t",
    kind: "absolute",
    offset_minutes: null,
    at: "2026-10-09T15:00:00Z",
    next_fire_at: "2026-10-09T15:00:00Z",
  });
  const recurring = task({
    id: "t",
    list_id: "inbox",
    due_date: "2026-10-09",
    rrule: "FREQ=DAILY",
  });
  replica.putTask({ ...recurring, checklist_items: [], reminders: [absolute] });

  server.on("POST", "/tasks/t/complete", (r) =>
    json({
      applied: true,
      task: {
        ...recurring,
        due_date: "2026-10-10",
        version: 2,
        checklist_items: [],
        reminders: [],
      },
      completion: {
        id: (r.body as { completion_id: string }).completion_id,
        task_id: "t",
        kind: "completed",
        completed_at: "2026-10-09T10:00:00Z",
      },
    }),
  );
  // What a sync would bring: the reminder's tombstone.
  server.on(
    "GET",
    "/sync/changes",
    json(page({ reminders: [tombstone(absolute)], next_cursor: "c1" })),
  );

  await actions.complete("t");
  await vi.waitFor(() => {
    expect(replica.getSnapshot().reminders.has("r1")).toBe(false);
  });
});

// M11 (D-26): clearing the date of a recurring task clears what depends
// on it in the same patch, so the server accepts it.
test("M11: clearing a recurring task's date does not leave its rule behind", async () => {
  const recurring = task({
    id: taskId,
    list_id: inboxId,
    title: "Water plants",
    due_date: "2026-10-12",
    rrule: "FREQ=DAILY",
  });
  const { server, user } = await renderApp({
    path: `/?task=${taskId}`,
    state: { tasks: [recurring] },
  });
  server.on("PATCH", `/tasks/${taskId}`, (r) =>
    json({ ...recurring, ...(r.body as object), version: 2 }),
  );
  const panel = await screen.findByRole("complementary", { name: "Task details" });
  await user.click(within(panel).getByRole("button", { name: /^Due: / }));
  const clear = screen.queryByRole("button", { name: "Clear" });
  if (clear !== null) {
    await user.click(clear);
    await waitFor(() => {
      expect(server.calls("PATCH", `/tasks/${taskId}`).length).toBeGreaterThan(0);
    });
  }
  for (const call of server.calls("PATCH", `/tasks/${taskId}`)) {
    const body = call.body as { due_date?: unknown; rrule?: unknown };
    if (body.due_date === null) {
      expect(body.rrule).toBeNull();
    }
  }
});

// L1 (SPEC sections 8 and 9): the Completed section shows every entry of
// the day, following next_cursor past the first page.
test("L1: the Completed section follows next_cursor", async () => {
  const entry = (n: number) => ({
    id: `c${n}`,
    task_id: `task-${n}`,
    kind: "completed",
    completed_at: "2026-10-09T08:00:00Z",
    can_undo: true,
    task: { id: `task-${n}`, list_id: inboxId, title: `Done ${n}`, priority: 0 },
  });
  const server = new FakeServer()
    .on("GET", "/me", json(testUser))
    .on("GET", "/sync/changes", json(page({ lists: [inbox] })))
    .on("GET", "/completions", (r) =>
      r.query.get("cursor") === null
        ? json({ items: Array.from({ length: 500 }, (_, i) => entry(i)), next_cursor: "p2" })
        : json({ items: [entry(500)], next_cursor: null }),
    );
  await renderApp({ server });
  const toggle = await screen.findByRole("button", { name: /Completed/ });
  await waitFor(() => {
    expect(toggle.textContent).toContain("501");
  });
});
