import { describe, expect, test } from "vitest";

import type { Task } from "../api/types";
import { list, page, reminder, tag, task } from "../test/fixtures";
import { Replica } from "./replica";
import {
  completedScope,
  counts,
  sections,
  tasksWithPendingReminders,
  type Context,
  type Section,
} from "./views";

const zone = "Europe/Madrid";
// Friday 2026-10-09, 12:00 in Madrid.
const now = new Date("2026-10-09T10:00:00Z");

function context(tasks: Task[], extra: Partial<Parameters<typeof page>[0]> = {}): Context {
  const replica = new Replica();
  replica.replaceAll(
    [
      page({
        lists: [
          list({ id: "inbox", position: "a0", is_inbox: true }),
          list({ id: "work", position: "a1" }),
        ],
        tasks,
        ...extra,
      }),
    ],
    "c",
  );
  return { snapshot: replica.getSnapshot(), zone, now };
}

function ids(section: Section | undefined): string[] {
  return section?.tasks.map((t) => t.id) ?? [];
}

describe("list view", () => {
  test("open tasks of the list in manual order", () => {
    const ctx = context([
      task({ id: "b", list_id: "work", position: "a2" }),
      task({ id: "a", list_id: "work", position: "a1" }),
      task({ id: "done", list_id: "work", status: "done" }),
      task({ id: "dropped", list_id: "work", status: "dropped" }), // D-49
      task({ id: "other", list_id: "inbox" }),
    ]);
    const result = sections({ kind: "list", listId: "work" }, ctx);
    expect(result).toHaveLength(1);
    expect(ids(result[0])).toEqual(["a", "b"]);
  });
});

describe("Today", () => {
  test("overdue, then today, in due order (D-43)", () => {
    const ctx = context([
      task({ id: "late-timed", list_id: "inbox", due_date: "2026-10-09", due_time: "18:00" }),
      task({ id: "all-day", list_id: "work", due_date: "2026-10-09" }),
      task({ id: "early", list_id: "inbox", due_date: "2026-10-09", due_time: "08:00" }),
      task({ id: "yesterday", list_id: "inbox", due_date: "2026-10-08" }),
      task({ id: "last-week", list_id: "work", due_date: "2026-10-01" }),
      task({ id: "tomorrow", list_id: "inbox", due_date: "2026-10-10" }),
      task({ id: "undated", list_id: "inbox" }),
    ]);
    const [overdue, today, ...rest] = sections({ kind: "today" }, ctx);
    expect(overdue?.key).toBe("overdue");
    expect(ids(overdue)).toEqual(["last-week", "yesterday"]);
    expect(ids(today)).toEqual(["all-day", "early", "late-timed"]);
    expect(rest).toHaveLength(0);
  });

  test("the day follows the profile zone (D-47)", () => {
    const tasks = [task({ id: "t", list_id: "inbox", due_date: "2026-10-10" })];
    const late = { ...context(tasks), now: new Date("2026-10-09T23:30:00Z") }; // 01:30 on the 10th in Madrid
    expect(ids(sections({ kind: "today" }, late)[1])).toEqual(["t"]);
    const newYork = { ...late, zone: "America/New_York" }; // still the 9th
    expect(ids(sections({ kind: "today" }, newYork)[1])).toEqual([]);
  });
});

// D-48: overdue, then today and the six days after it.
describe("Next 7 days", () => {
  test("one section per day, empty days included", () => {
    const ctx = context([
      task({ id: "overdue", list_id: "inbox", due_date: "2026-10-01" }),
      task({ id: "today", list_id: "inbox", due_date: "2026-10-09" }),
      task({ id: "sunday", list_id: "inbox", due_date: "2026-10-11" }),
      task({ id: "day7", list_id: "inbox", due_date: "2026-10-15" }),
      task({ id: "day8", list_id: "inbox", due_date: "2026-10-16" }),
    ]);
    const result = sections({ kind: "next7" }, ctx);
    expect(result).toHaveLength(8);
    expect(ids(result[0])).toEqual(["overdue"]);
    const days = result.slice(1).map((s) => (s.key === "day" ? s.day.toString() : ""));
    expect(days).toEqual([
      "2026-10-09",
      "2026-10-10",
      "2026-10-11",
      "2026-10-12",
      "2026-10-13",
      "2026-10-14",
      "2026-10-15",
    ]);
    expect(ids(result[1])).toEqual(["today"]);
    expect(ids(result[2])).toEqual([]);
    expect(ids(result[3])).toEqual(["sunday"]);
    expect(ids(result[7])).toEqual(["day7"]);
  });
});

describe("tag and search views", () => {
  test("a tag mixes lists in list order, then task order", () => {
    const ctx = context(
      [
        task({ id: "w", list_id: "work", position: "a0", tag_ids: ["g"] }),
        task({ id: "i2", list_id: "inbox", position: "a2", tag_ids: ["g"] }),
        task({ id: "i1", list_id: "inbox", position: "a1", tag_ids: ["g", "h"] }),
        task({ id: "untagged", list_id: "inbox" }),
      ],
      { tags: [tag({ id: "g" })] },
    );
    expect(ids(sections({ kind: "tag", tagId: "g" }, ctx)[0])).toEqual(["i1", "i2", "w"]);
  });

  test("search covers open tasks only (D-52)", () => {
    const ctx = context([
      task({ id: "title", list_id: "inbox", title: "Café" }),
      task({ id: "desc", list_id: "work", title: "x", description: "more CAFE" }),
      task({ id: "done", list_id: "inbox", title: "cafe", status: "done" }),
    ]);
    expect(ids(sections({ kind: "search", query: "cafe" }, ctx)[0])).toEqual(["title", "desc"]);
  });
});

test("tasks of a deleted list are not shown", () => {
  const replica = new Replica();
  replica.replaceAll(
    [page({ lists: [list({ id: "inbox" })], tasks: [task({ id: "orphan", list_id: "gone" })] })],
    "c",
  );
  const ctx = { snapshot: replica.getSnapshot(), zone, now };
  expect(counts(ctx).lists.get("gone")).toBeUndefined();
});

describe("counts", () => {
  test("open tasks per list, due today or before, and in the next 7 days", () => {
    const ctx = context([
      task({ id: "1", list_id: "inbox", due_date: "2026-10-08" }),
      task({ id: "2", list_id: "inbox", due_date: "2026-10-09" }),
      task({ id: "3", list_id: "work", due_date: "2026-10-15" }),
      task({ id: "4", list_id: "work", due_date: "2026-10-16" }),
      task({ id: "5", list_id: "work" }),
      task({ id: "6", list_id: "work", status: "done", due_date: "2026-10-09" }),
    ]);
    const c = counts(ctx);
    expect(c.today).toBe(2);
    expect(c.next7).toBe(3);
    expect(c.lists.get("inbox")).toBe(2);
    expect(c.lists.get("work")).toBe(3);
  });
});

// D-54.
describe("completedScope", () => {
  const ctx = context([]);
  test.each([
    [{ kind: "list", listId: "work" } as const, { list_id: "work" }],
    [{ kind: "tag", tagId: "g" } as const, { tag_id: "g" }],
    [{ kind: "today" } as const, { due_to: "2026-10-09" }],
    [{ kind: "next7" } as const, { due_to: "2026-10-15" }],
    [{ kind: "search", query: "x" } as const, null],
  ])("%j", (view, scope) => {
    expect(completedScope(view, ctx)).toEqual(scope);
  });
});

test("tasks with a pending reminder", () => {
  const replica = new Replica();
  replica.replaceAll(
    [
      page({
        reminders: [
          reminder({ id: "1", task_id: "a", next_fire_at: "2026-10-10T07:00:00Z" }),
          reminder({ id: "2", task_id: "b" }),
        ],
      }),
    ],
    "c",
  );
  expect([...tasksWithPendingReminders(replica.getSnapshot())]).toEqual(["a"]);
});
