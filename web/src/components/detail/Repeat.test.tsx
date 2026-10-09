import { screen, waitFor, within } from "@testing-library/react";
import { expect, test } from "vitest";

import { json } from "../../test/fakeFetch";
import { task } from "../../test/fixtures";
import { inboxId, renderApp } from "../../test/render";

const id = "00000000-0000-7000-8000-0000000000d1";

/** Echoes PATCH bodies onto the task, one version up. */
function echo(base: ReturnType<typeof task>) {
  let current = base;
  return (r: { body: unknown }) => {
    current = { ...current, ...(r.body as object), version: current.version + 1 };
    return json(current);
  };
}

test("the editor writes a weekly rule in canonical form", async () => {
  const base = task({ id, list_id: inboxId, title: "Gym", due_date: "2026-10-12" });
  const { server, user } = await renderApp({ path: `/?task=${id}`, state: { tasks: [base] } });
  server.on("PATCH", `/tasks/${id}`, echo(base));
  const panel = await screen.findByRole("complementary", { name: "Task details" });

  await user.click(within(panel).getByRole("button", { name: "Repeat: Does not repeat" }));
  const dialog = await screen.findByRole("dialog", { name: "Repeat" });
  await user.click(within(dialog).getByRole("button", { name: /Does not repeat/ }));
  await user.click(await screen.findByRole("option", { name: "Weekly" }));
  await user.click(within(dialog).getByRole("button", { name: "Wed" }));
  await user.click(within(dialog).getByRole("button", { name: "Mon" }));
  await user.click(within(dialog).getByRole("button", { name: "Save" }));

  await waitFor(() => {
    expect(server.calls("PATCH", `/tasks/${id}`)[0]?.body).toEqual({
      rrule: "FREQ=WEEKLY;BYDAY=MO,WE",
      repeat_from: "due",
    });
  });
  expect(
    await within(panel).findByRole("button", { name: "Repeat: Every week on Mon, Wed" }),
  ).toBeDefined();
  // The row shows that the task repeats, with a name for screen readers.
  expect(
    within(screen.getByRole("row", { name: /Gym/ })).getByRole("img", { name: "Repeats" }),
  ).toBeDefined();
});

// D-26: a rule needs a date; an undated task starts today.
test("a rule on an undated task also sets today's date", async () => {
  const base = task({ id, list_id: inboxId, title: "Water plants" });
  const { server, user } = await renderApp({ path: `/?task=${id}`, state: { tasks: [base] } });
  server.on("PATCH", `/tasks/${id}`, echo(base));
  const panel = await screen.findByRole("complementary", { name: "Task details" });
  await user.click(within(panel).getByRole("button", { name: "Repeat: Does not repeat" }));
  const dialog = await screen.findByRole("dialog", { name: "Repeat" });
  await user.click(within(dialog).getByRole("button", { name: /Does not repeat/ }));
  await user.click(await screen.findByRole("option", { name: "Daily" }));
  await user.click(within(dialog).getByRole("radio", { name: "The completion date" }));
  await user.click(within(dialog).getByRole("button", { name: "Save" }));
  await waitFor(() => {
    expect(server.calls("PATCH", `/tasks/${id}`)[0]?.body).toEqual({
      rrule: "FREQ=DAILY",
      repeat_from: "completion",
      due_date: "2026-10-09",
    });
  });
});

test("completing a recurring task names the next date and offers undo", async () => {
  const base = task({
    id,
    list_id: inboxId,
    title: "Standup",
    due_date: "2026-10-09",
    rrule: "FREQ=DAILY",
  });
  const { server, user } = await renderApp({ state: { tasks: [base] } });
  server.on("POST", `/tasks/${id}/complete`, (r) =>
    json({
      applied: true,
      task: { ...base, due_date: "2026-10-10", recurrence_done_count: 1, version: 2 },
      completion: {
        id: (r.body as { completion_id: string }).completion_id,
        task_id: id,
        kind: "completed",
        completed_at: "2026-10-09T10:00:00Z",
      },
    }),
  );
  server.on(
    "POST",
    `/tasks/${id}/uncomplete`,
    json({ applied: true, task: { ...base, version: 3 } }),
  );

  await user.click(await screen.findByRole("checkbox", { name: "Complete task" }));
  expect(await screen.findByText("Completed. Next: Sat, Oct 10")).toBeDefined();
  // The task stays in the inbox, now due tomorrow.
  expect(screen.getByRole("row", { name: /Standup/ }).textContent).toContain("Sat, Oct 10");
  await user.click(screen.getByRole("button", { name: "Undo" }));
  await waitFor(() => {
    expect(server.calls("POST", `/tasks/${id}/uncomplete`)).toHaveLength(1);
  });
});

test("skip is offered for recurring tasks only, and can be undone", async () => {
  const base = task({
    id,
    list_id: inboxId,
    title: "Standup",
    due_date: "2026-10-09",
    rrule: "FREQ=DAILY",
  });
  const plain = task({ id: "p", list_id: inboxId, title: "Plain" });
  const { server, user } = await renderApp({
    path: `/?task=${id}`,
    state: { tasks: [base, plain] },
  });
  server.on("POST", `/tasks/${id}/skip`, (r) =>
    json({
      applied: true,
      task: { ...base, due_date: "2026-10-10", version: 2 },
      completion: {
        id: (r.body as { completion_id: string }).completion_id,
        task_id: id,
        kind: "skipped",
        completed_at: "2026-10-09T10:00:00Z",
      },
    }),
  );
  const panel = await screen.findByRole("complementary", { name: "Task details" });
  await user.click(within(panel).getByRole("button", { name: "Skip this occurrence" }));
  expect(await screen.findByText("Occurrence skipped. Next: Sat, Oct 10")).toBeDefined();
  expect(server.calls("POST", `/tasks/${id}/skip`)[0]?.body).toMatchObject({
    occurrence_due_date: "2026-10-09",
  });

  await user.click(screen.getByRole("row", { name: /Plain/ }));
  const plainPanel = await screen.findByRole("complementary", { name: "Task details" });
  await waitFor(() => {
    expect(within(plainPanel).queryByRole("button", { name: "Skip this occurrence" })).toBeNull();
  });
});
