import { describe, expect, test } from "vitest";

import { ApiError } from "../api/client";
import { FixedClock } from "../lib/clock";
import { item, list, page, task } from "../test/fixtures";
import { FakeServer, json, networkError, noContent, problem } from "../test/fakeFetch";
import { Actions } from "./actions";
import { Replica } from "./replica";
import { Syncer } from "./sync";

function setup() {
  const server = new FakeServer();
  const replica = new Replica();
  const api = server.api();
  const syncer = new Syncer(api, replica);
  const errors: unknown[] = [];
  let completionsChanged = 0;
  const actions = new Actions({
    api,
    replica,
    syncer,
    clock: new FixedClock("2026-10-09T10:00:00Z"),
    zone: () => "Europe/Madrid",
    onCompletionsChanged: () => completionsChanged++,
    onError: (e) => errors.push(e),
  });
  replica.replaceAll(
    [page({ lists: [list({ id: "inbox", is_inbox: true, position: "a0" })] })],
    "c0",
  );
  server.on("GET", "/sync/changes", json(page({ next_cursor: "c1" })));
  return { server, replica, actions, errors, completions: () => completionsChanged };
}

describe("idempotent retries (D-04, D-10)", () => {
  test("a create retried after a network failure sends the same id", async () => {
    const { server, replica, actions } = setup();
    server.on("POST", "/tasks", networkError(), (r) =>
      json(
        task({ ...(r.body as object), id: (r.body as { id: string }).id, list_id: "inbox" }),
        201,
      ),
    );
    const id = await actions.createTask({ title: "Milk", listId: "inbox" });
    const calls = server.calls("POST", "/tasks");
    expect(calls.length).toBe(2);
    expect((calls[0]?.body as { id: string }).id).toBe((calls[1]?.body as { id: string }).id);
    expect(id).toBe((calls[0]?.body as { id: string }).id);
    expect(replica.getSnapshot().tasks.get(id ?? "")?.title).toBe("Milk");
  });

  test("a completion retried sends the same completion_id and the due date seen", async () => {
    const { server, replica, actions, completions } = setup();
    replica.putTask(task({ id: "t", list_id: "inbox", due_date: "2026-10-09" }));
    server.on("POST", "/tasks/t/complete", networkError(), (r) =>
      json({
        applied: true,
        task: task({ id: "t", list_id: "inbox", status: "done", version: 2 }),
        completion: {
          id: (r.body as { completion_id: string }).completion_id,
          task_id: "t",
          kind: "completed",
          completed_at: "2026-10-09T10:00:00Z",
        },
      }),
    );
    const completionId = await actions.complete("t");
    const calls = server.calls("POST", "/tasks/t/complete");
    expect(calls.length).toBe(2);
    expect(calls[0]?.body).toEqual(calls[1]?.body);
    expect(calls[0]?.body).toMatchObject({
      completion_id: completionId,
      occurrence_due_date: "2026-10-09",
    });
    expect(replica.getSnapshot().tasks.get("t")?.status).toBe("done");
    expect(completions()).toBe(1);
  });

  test("completing twice quickly sends one request", async () => {
    const { server, replica, actions } = setup();
    replica.putTask(task({ id: "t", list_id: "inbox" }));
    server.on(
      "POST",
      "/tasks/t/complete",
      json({
        applied: true,
        task: task({ id: "t", list_id: "inbox", status: "done", version: 2 }),
      }),
    );
    await Promise.all([actions.complete("t"), actions.complete("t")]);
    expect(server.calls("POST", "/tasks/t/complete").length).toBe(1);
  });

  test("a stale completion (applied: false) gives nothing to undo", async () => {
    const { server, replica, actions } = setup();
    replica.putTask(task({ id: "t", list_id: "inbox" }));
    server.on(
      "POST",
      "/tasks/t/complete",
      json({ applied: false, task: task({ id: "t", list_id: "inbox", version: 2 }) }),
    );
    expect(await actions.complete("t")).toBeUndefined();
    expect(replica.getSnapshot().tasks.get("t")?.status).toBe("open");
  });
});

describe("optimistic writes", () => {
  test("a refused patch puts the task back and reports the error", async () => {
    const { server, replica, actions, errors } = setup();
    replica.putTask(task({ id: "t", list_id: "inbox", title: "Before" }));
    server.on("PATCH", "/tasks/t", problem(422, "validation_failed"));
    const pending = actions.updateTask("t", { title: "After" });
    expect(replica.getSnapshot().tasks.get("t")?.title).toBe("After");
    await pending;
    expect(replica.getSnapshot().tasks.get("t")?.title).toBe("Before");
    expect(errors).toHaveLength(1);
    expect((errors[0] as ApiError).code).toBe("validation_failed");
  });

  test("patches are sent as merge patches", async () => {
    const { server, replica, actions } = setup();
    replica.putTask(task({ id: "t", list_id: "inbox" }));
    server.on(
      "PATCH",
      "/tasks/t",
      json(task({ id: "t", list_id: "inbox", title: "x", version: 2 })),
    );
    await actions.updateTask("t", { title: "x" });
    expect(server.calls("PATCH", "/tasks/t")[0]?.contentType).toBe("application/merge-patch+json");
  });

  test("a failed completion reopens the task", async () => {
    const { server, replica, actions, errors } = setup();
    replica.putTask(task({ id: "t", list_id: "inbox" }));
    server.on("POST", "/tasks/t/complete", problem(409, "conflict"));
    expect(await actions.complete("t")).toBeUndefined();
    expect(replica.getSnapshot().tasks.get("t")?.status).toBe("open");
    expect(errors).toHaveLength(1);
  });
});

describe("deletes", () => {
  test("deleting a list removes its tasks here and pulls the server's changes", async () => {
    const { server, replica, actions } = setup();
    replica.putList(list({ id: "work" }));
    replica.putTask(task({ id: "t", list_id: "work" }));
    server.on("DELETE", "/lists/work", noContent());
    expect(await actions.deleteList("work")).toBe(true);
    expect(replica.getSnapshot().lists.has("work")).toBe(false);
    expect(replica.getSnapshot().tasks.has("t")).toBe(false);
    await Promise.resolve();
    expect(server.calls("GET", "/sync/changes").length).toBe(1);
  });

  test("a failed delete puts the task back", async () => {
    const { server, replica, actions } = setup();
    replica.putTask(task({ id: "t", list_id: "inbox" }));
    server.on("DELETE", "/tasks/t", problem(500, "internal"));
    expect(await actions.deleteTask("t")).toBe(false);
    expect(replica.getSnapshot().tasks.has("t")).toBe(true);
  });

  test("restoring a task brings it and its checklist back", async () => {
    const { server, replica, actions } = setup();
    server.on(
      "POST",
      "/tasks/t/restore",
      json(
        task({
          id: "t",
          list_id: "inbox",
          version: 3,
          checklist_items: [item({ id: "i", task_id: "t" })],
          reminders: [],
        }),
      ),
    );
    expect(await actions.restoreTask("t")).toBe(true);
    expect(replica.getSnapshot().tasks.has("t")).toBe(true);
    expect(replica.getSnapshot().items.has("i")).toBe(true);
  });
});

describe("quick add", () => {
  test("new tasks go to the end of their list, due today in the profile zone (D-53)", async () => {
    const { server, replica, actions } = setup();
    replica.putTask(task({ id: "t", list_id: "inbox", position: "a5" }));
    server.on("POST", "/tasks", (r) =>
      json(
        task({ ...(r.body as object), id: (r.body as { id: string }).id, list_id: "inbox" }),
        201,
      ),
    );
    await actions.createTask({ title: "x", listId: "inbox", dueDate: actions.today() });
    const body = server.calls("POST", "/tasks")[0]?.body as { position: string; due_date: string };
    expect(body.position > "a5").toBe(true);
    expect(body.due_date).toBe("2026-10-09");
  });
});
