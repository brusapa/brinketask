import { screen, waitFor, within } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import { FakeServer, json } from "../test/fakeFetch";
import { list, page, tag, task } from "../test/fixtures";
import { inbox, inboxId, renderApp, testUser } from "../test/render";

const workId = "00000000-0000-7000-8000-0000000000b1";
const work = list({ id: workId, name: "Work", position: "a1", color: "#2563EB" });

describe("Today", () => {
  test("overdue and today sections, with list names and due labels", async () => {
    await renderApp({
      path: "/today",
      state: {
        lists: [work],
        tasks: [
          task({ id: "t1", list_id: inboxId, title: "Pay rent", due_date: "2026-10-01" }),
          task({
            id: "t2",
            list_id: workId,
            title: "Standup",
            due_date: "2026-10-09",
            due_time: "09:30",
          }),
          task({ id: "t3", list_id: workId, title: "Report", due_date: "2026-10-09", priority: 3 }),
          task({ id: "t4", list_id: inboxId, title: "Later", due_date: "2026-10-20" }),
        ],
      },
    });
    expect(await screen.findByRole("heading", { level: 1, name: "Today" })).toBeDefined();
    const overdue = screen.getByRole("grid", { name: "Overdue" });
    expect(
      within(overdue)
        .getAllByRole("row")
        .map((r) => r.textContent),
    ).toEqual([expect.stringContaining("Pay rent")]);
    const today = screen.getByRole("grid", { name: "Today" });
    const rows = within(today).getAllByRole("row");
    // All-day before timed (D-43).
    expect(rows.map((r) => within(r).getByText(/Report|Standup/).textContent)).toEqual([
      "Report",
      "Standup",
    ]);
    expect(rows[1]?.textContent).toContain("Work");
    // 09:30 is already past at 12:00: overdue, and said so in words.
    expect(rows[1]?.textContent).toContain("Overdue:");
    expect(screen.queryByText("Later")).toBeNull();
  });
});

test("the checkbox states the priority (DESIGN.md section 1)", async () => {
  await renderApp({
    state: { tasks: [task({ id: "t", list_id: inboxId, title: "Urgent", priority: 3 })] },
  });
  expect(
    await screen.findByRole("checkbox", { name: "Complete task, high priority" }),
  ).toBeDefined();
});

test("completing shows an undo toast that undoes that completion", async () => {
  const { server, user } = await renderApp({
    state: { tasks: [task({ id: "t", list_id: inboxId, title: "Call mum" })] },
  });
  server.on("POST", "/tasks/t/complete", (r) =>
    json({
      applied: true,
      task: task({ id: "t", list_id: inboxId, title: "Call mum", status: "done", version: 2 }),
      completion: {
        id: (r.body as { completion_id: string }).completion_id,
        task_id: "t",
        kind: "completed",
        completed_at: "2026-10-09T10:00:00Z",
      },
    }),
  );
  server.on(
    "POST",
    "/tasks/t/uncomplete",
    json({
      applied: true,
      task: task({ id: "t", list_id: inboxId, title: "Call mum", version: 3 }),
    }),
  );

  await user.click(await screen.findByRole("checkbox", { name: "Complete task" }));
  await waitFor(() => {
    expect(screen.queryByRole("row", { name: /Call mum/ })).toBeNull();
  });
  await user.click(await screen.findByRole("button", { name: "Undo" }));
  await waitFor(() => {
    expect(server.calls("POST", "/tasks/t/uncomplete")).toHaveLength(1);
  });
  const sent = server.calls("POST", "/tasks/t/complete")[0]?.body as { completion_id: string };
  expect(server.calls("POST", "/tasks/t/uncomplete")[0]?.body).toEqual({
    completion_id: sent.completion_id,
  });
  expect(await screen.findByRole("row", { name: /Call mum/ })).toBeDefined();
});

describe("quick add (D-53)", () => {
  test("in Today the task is due today, in the inbox", async () => {
    const { server, user } = await renderApp({ path: "/today" });
    server.on("POST", "/tasks", (r) =>
      json(
        task({ ...(r.body as object), id: (r.body as { id: string }).id, list_id: inboxId }),
        201,
      ),
    );
    await user.type(await screen.findByRole("textbox", { name: "Add a task" }), "Buy bread{Enter}");
    await waitFor(() => {
      expect(server.calls("POST", "/tasks")).toHaveLength(1);
    });
    expect(server.calls("POST", "/tasks")[0]?.body).toMatchObject({
      title: "Buy bread",
      list_id: inboxId,
      due_date: "2026-10-09",
    });
    expect(screen.getByRole<HTMLInputElement>("textbox", { name: "Add a task" }).value).toBe("");
  });

  test("in a tag view the task carries the tag", async () => {
    const { server, user } = await renderApp({
      path: "/tags/g",
      state: { tags: [tag({ id: "g", name: "home" })] },
    });
    server.on("POST", "/tasks", (r) =>
      json(
        task({ ...(r.body as object), id: (r.body as { id: string }).id, list_id: inboxId }),
        201,
      ),
    );
    await user.type(await screen.findByRole("textbox", { name: "Add a task" }), "Fix tap{Enter}");
    await waitFor(() => {
      expect(server.calls("POST", "/tasks")[0]?.body).toMatchObject({
        list_id: inboxId,
        tag_ids: ["g"],
        due_date: null,
      });
    });
  });
});

// D-54 and SPEC section 8: the day in the profile zone, the view's scope.
test("the Completed section asks for today in the profile zone, scoped like the view", async () => {
  const { server } = await renderApp({ path: "/today" });
  await waitFor(() => {
    expect(server.calls("GET", "/completions").length).toBeGreaterThan(0);
  });
  const query = server.calls("GET", "/completions")[0]?.query;
  expect(query?.get("completed_from")).toBe("2026-10-08T22:00:00.000Z");
  expect(query?.get("completed_to")).toBe("2026-10-09T22:00:00.000Z");
  expect(query?.get("due_to")).toBe("2026-10-09");
});

test("the Completed section shows entries; only the latest of a task can be undone", async () => {
  const server = new FakeServer()
    .on("GET", "/me", json(testUser))
    .on("GET", "/sync/changes", json(page({ lists: [inbox] })))
    .on(
      "GET",
      "/completions",
      json({
        items: [
          completion("c2", "Walk", "2026-10-09T08:00:00Z", true),
          completion("c1", "Water plants", "2026-10-09T07:00:00Z", false),
        ],
      }),
    );
  const { user } = await renderApp({ server });
  const toggle = await screen.findByRole("button", { name: /Completed/ });
  expect(toggle.textContent).toContain("2");
  await user.click(toggle);
  expect(screen.getByRole("checkbox", { name: "Mark “Walk” as not completed" })).toHaveProperty(
    "disabled",
    false,
  );
  expect(
    screen.getByRole("checkbox", { name: "Mark “Water plants” as not completed" }),
  ).toHaveProperty("disabled", true);
  // Completion time in the profile zone.
  expect(screen.getByText("10:00 AM")).toBeDefined();
});

function completion(id: string, title: string, at: string, canUndo: boolean) {
  return {
    id,
    task_id: `task-${id}`,
    kind: "completed",
    completed_at: at,
    can_undo: canUndo,
    task: { id: `task-${id}`, list_id: inboxId, title, priority: 0 },
  };
}

test("the sidebar shows counts and creates lists", async () => {
  const { server, user } = await renderApp({
    state: {
      lists: [work],
      tasks: [
        task({ id: "1", list_id: workId, due_date: "2026-10-09" }),
        task({ id: "2", list_id: workId }),
        task({ id: "3", list_id: inboxId }),
      ],
    },
  });
  const nav = await screen.findByRole("navigation");
  expect(within(nav).getByRole("link", { name: /Work/ }).textContent).toContain("2");
  expect(within(nav).getByRole("link", { name: /Today/ }).textContent).toContain("1");

  server.on("POST", "/lists", (r) =>
    json(list({ ...(r.body as object), id: (r.body as { id: string }).id }), 201),
  );
  await user.click(within(nav).getByRole("button", { name: "New list" }));
  const dialog = await screen.findByRole("dialog");
  await user.type(within(dialog).getByRole("textbox", { name: "Name" }), "Home");
  await user.click(within(dialog).getByRole("radio", { name: "Green" }));
  await user.click(within(dialog).getByRole("button", { name: "Save" }));
  await waitFor(() => {
    expect(server.calls("POST", "/lists")[0]?.body).toMatchObject({
      name: "Home",
      color: "#16A34A",
    });
  });
  expect(await within(nav).findByRole("link", { name: /Home/ })).toBeDefined();
});
