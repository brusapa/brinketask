import { describe, expect, test } from "vitest";

import { item, list, page, task, tombstone } from "../test/fixtures";
import { Replica } from "./replica";

describe("Replica", () => {
  test("a full state replaces everything and sets the cursor", () => {
    const r = new Replica();
    r.putList(list({ id: "old" }));
    r.replaceAll(
      [
        page({ lists: [list({ id: "a" })], has_more: true, next_cursor: "1" }),
        page({ tasks: [task({ id: "t", list_id: "a" })], next_cursor: "2" }),
      ],
      "2",
    );
    const s = r.getSnapshot();
    expect([...s.lists.keys()]).toEqual(["a"]);
    expect(s.tasks.has("t")).toBe(true);
    expect(r.cursor).toBe("2");
  });

  test("tombstones remove; tasks keep their checklist apart", () => {
    const r = new Replica();
    const t = task({
      id: "t",
      list_id: "a",
      checklist_items: [item({ id: "i", task_id: "t" })],
      reminders: [],
    });
    r.putTask(t);
    expect(r.getSnapshot().items.has("i")).toBe(true);
    expect("checklist_items" in (r.getSnapshot().tasks.get("t") ?? {})).toBe(false);

    r.applyChanges(page({ tasks: [tombstone(t)], next_cursor: "9" }));
    expect(r.getSnapshot().tasks.has("t")).toBe(false);
    expect(r.cursor).toBe("9");
  });

  test("an older version never replaces a newer one", () => {
    const r = new Replica();
    r.putTask(task({ id: "t", list_id: "a", title: "new", version: 3 }));
    r.applyChanges(page({ tasks: [task({ id: "t", list_id: "a", title: "old", version: 2 })] }));
    expect(r.getSnapshot().tasks.get("t")?.title).toBe("new");
  });

  // A sync page fetched before a local delete must not resurrect it.
  test("a resource deleted here stays deleted against stale copies", () => {
    const r = new Replica();
    const t = task({ id: "t", list_id: "a", version: 2 });
    r.putTask(t);
    r.remove("tasks", ["t"]);
    r.applyChanges(page({ tasks: [t] }));
    expect(r.getSnapshot().tasks.has("t")).toBe(false);
    // A newer version (a restore) does come back.
    r.putTask({ ...t, version: 4 });
    expect(r.getSnapshot().tasks.has("t")).toBe(true);
  });

  test("snapshots are immutable and listeners hear each change", () => {
    const r = new Replica();
    let calls = 0;
    const unsubscribe = r.subscribe(() => calls++);
    const before = r.getSnapshot();
    r.putList(list({ id: "a" }));
    expect(before.lists.size).toBe(0);
    expect(r.getSnapshot()).not.toBe(before);
    expect(calls).toBe(1);
    unsubscribe();
    r.putList(list({ id: "b" }));
    expect(calls).toBe(1);
  });
});
