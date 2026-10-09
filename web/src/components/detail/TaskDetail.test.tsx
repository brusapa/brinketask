import { screen, waitFor, within } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import { json, noContent } from "../../test/fakeFetch";
import { item, list, tag, task } from "../../test/fixtures";
import { inboxId, renderApp } from "../../test/render";

const taskId = "00000000-0000-7000-8000-0000000000c1";

/** Echoes a PATCH /tasks/{id} body onto the task, one version up. */
function patchEcho(base: ReturnType<typeof task>) {
  let current = base;
  return (r: { body: unknown }) => {
    current = { ...current, ...(r.body as object), version: current.version + 1 };
    return json(current);
  };
}

async function openDetail(
  extra: Partial<ReturnType<typeof task>> = {},
  state: Parameters<typeof renderApp>[0] = {},
) {
  const base = task({ id: taskId, list_id: inboxId, title: "Plan trip", ...extra });
  const rendered = await renderApp({
    path: `/?task=${taskId}`,
    ...state,
    state: { ...state.state, tasks: [base, ...(state.state?.tasks ?? [])] },
  });
  rendered.server.on("PATCH", `/tasks/${taskId}`, patchEcho(base));
  const panel = await screen.findByRole("complementary", { name: "Task details" });
  return { ...rendered, panel };
}

test("selecting a row opens the detail; Escape closes it", async () => {
  const { user } = await renderApp({
    state: { tasks: [task({ id: taskId, list_id: inboxId, title: "Plan trip" })] },
  });
  await user.click(await screen.findByText("Plan trip"));
  const panel = await screen.findByRole("complementary", { name: "Task details" });
  expect(within(panel).getByRole<HTMLTextAreaElement>("textbox", { name: "Title" }).value).toBe(
    "Plan trip",
  );
  await user.keyboard("{Escape}");
  await waitFor(() => {
    expect(screen.queryByRole("complementary", { name: "Task details" })).toBeNull();
  });
});

test("the title is saved when the field loses focus", async () => {
  const { panel, server, user } = await openDetail();
  const title = within(panel).getByRole("textbox", { name: "Title" });
  await user.clear(title);
  await user.type(title, "Plan the trip");
  await user.tab();
  await waitFor(() => {
    expect(server.calls("PATCH", `/tasks/${taskId}`).map((c) => c.body)).toEqual([
      { title: "Plan the trip" },
    ]);
  });
  expect(await screen.findByRole("row", { name: /Plan the trip/ })).toBeDefined();
});

describe("due date", () => {
  test("a shortcut sets the date", async () => {
    const { panel, server, user } = await openDetail();
    await user.click(within(panel).getByRole("button", { name: "Due: No date" }));
    await user.click(await screen.findByRole("button", { name: "Tomorrow" }));
    await waitFor(() => {
      expect(server.calls("PATCH", `/tasks/${taskId}`)[0]?.body).toEqual({
        due_date: "2026-10-10",
      });
    });
  });

  // D-26: clearing the date clears the fields that depend on it.
  test("clearing removes date, time and zone in one patch", async () => {
    const { panel, server, user } = await openDetail({
      due_date: "2026-10-12",
      due_time: "09:00",
      due_tz: "Europe/Madrid",
    });
    await user.click(within(panel).getByRole("button", { name: /^Due: / }));
    await user.click(await screen.findByRole("button", { name: "Clear" }));
    await waitFor(() => {
      expect(server.calls("PATCH", `/tasks/${taskId}`)[0]?.body).toEqual({
        due_date: null,
        due_time: null,
        due_tz: null,
      });
    });
  });
});

test("priority is chosen from a menu", async () => {
  const { panel, server, user } = await openDetail();
  await user.click(within(panel).getByRole("button", { name: "Priority: No priority" }));
  await user.click(await screen.findByRole("menuitemradio", { name: "High" }));
  await waitFor(() => {
    expect(server.calls("PATCH", `/tasks/${taskId}`)[0]?.body).toEqual({ priority: 3 });
  });
});

describe("checklist", () => {
  test("adding and ticking items", async () => {
    const { panel, server, user } = await openDetail(
      {},
      { state: { checklist_items: [item({ id: "i1", task_id: taskId, title: "Passport" })] } },
    );
    server.on("POST", `/tasks/${taskId}/checklist-items`, (r) =>
      json(
        item({ ...(r.body as object), id: (r.body as { id: string }).id, task_id: taskId }),
        201,
      ),
    );
    server.on(
      "PATCH",
      "/checklist-items/i1",
      json(item({ id: "i1", task_id: taskId, title: "Passport", is_done: true, version: 2 })),
    );

    await user.click(within(panel).getByRole("checkbox", { name: "Mark “Passport” as done" }));
    await waitFor(() => {
      expect(server.calls("PATCH", "/checklist-items/i1")[0]?.body).toEqual({ is_done: true });
    });

    await user.type(within(panel).getByRole("textbox", { name: "Add item" }), "Tickets{Enter}");
    await waitFor(() => {
      expect(server.calls("POST", `/tasks/${taskId}/checklist-items`)[0]?.body).toMatchObject({
        title: "Tickets",
        is_done: false,
      });
    });
    expect(await within(panel).findByRole("button", { name: "Edit item “Tickets”" })).toBeDefined();
  });
});

test("a new tag is created and added to the task", async () => {
  const { panel, server, user } = await openDetail(
    {},
    { state: { tags: [tag({ id: "g1", name: "work" })] } },
  );
  server.on("POST", "/tags", (r) =>
    json(tag({ ...(r.body as object), id: (r.body as { id: string }).id }), 201),
  );
  await user.type(within(panel).getByRole("combobox", { name: "Add tag" }), "travel");
  await user.click(await screen.findByRole("option", { name: "Create tag “travel”" }));
  await waitFor(() => {
    expect(server.calls("PATCH", `/tasks/${taskId}`)).toHaveLength(1);
  });
  const newTag = server.calls("POST", "/tags")[0]?.body as { id: string; name: string };
  expect(newTag.name).toBe("travel");
  expect(server.calls("PATCH", `/tasks/${taskId}`)[0]?.body).toEqual({ tag_ids: [newTag.id] });
});

test("moving to another list sends the list and a position at its end", async () => {
  const work = list({ id: "work", name: "Work", position: "a1" });
  const { panel, server, user } = await openDetail(
    {},
    { state: { lists: [work], tasks: [task({ id: "w", list_id: "work", position: "a5" })] } },
  );
  await user.click(within(panel).getByRole("button", { name: /Inbox/ }));
  await user.click(await screen.findByRole("option", { name: "Work" }));
  await waitFor(() => {
    expect(server.calls("PATCH", `/tasks/${taskId}`)).toHaveLength(1);
  });
  const body = server.calls("PATCH", `/tasks/${taskId}`)[0]?.body as {
    list_id: string;
    position: string;
  };
  expect(body.list_id).toBe("work");
  expect(body.position > "a5").toBe(true);
});

test("deleting closes the panel and offers undo, which restores", async () => {
  const { panel, server, user } = await openDetail();
  server.on("DELETE", `/tasks/${taskId}`, noContent());
  server.on(
    "POST",
    `/tasks/${taskId}/restore`,
    json(task({ id: taskId, list_id: inboxId, title: "Plan trip", version: 3 })),
  );
  await user.click(within(panel).getByRole("button", { name: "Delete task" }));
  await waitFor(() => {
    expect(screen.queryByRole("complementary", { name: "Task details" })).toBeNull();
  });
  expect(screen.queryByRole("row", { name: /Plan trip/ })).toBeNull();
  await user.click(await screen.findByRole("button", { name: "Undo" }));
  expect(await screen.findByRole("row", { name: /Plan trip/ })).toBeDefined();
});

// D-55: Markdown, without raw HTML; links open safely in a new tab.
test("the description is shown as Markdown without raw HTML", async () => {
  const { panel } = await openDetail({
    description: "**Bold** and [a link](https://example.com)\n\n<img src=x onerror=alert(1)>",
  });
  expect(within(panel).getByText("Bold").tagName).toBe("STRONG");
  const link = within(panel).getByRole("link", { name: "a link" });
  expect(link.getAttribute("rel")).toBe("noopener noreferrer");
  expect(link.getAttribute("target")).toBe("_blank");
  expect(panel.querySelector("img")).toBeNull();
});
