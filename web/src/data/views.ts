// The views of the web client, computed from the replica (D-31): which
// open tasks each screen shows, in which sections and order, and the
// sidebar counts. Pure functions of (snapshot, zone, now), so tests can fix
// all three.
import type { CalendarDate } from "@internationalized/date";

import type { List, Tag } from "../api/types";
import { dateIn, parseDay } from "../lib/dates";
import { comparePositions } from "../lib/positions";
import { matcher } from "../lib/search";
import type { Snapshot, TaskRow } from "./replica";

/** The screens that show tasks. */
export type View =
  | { kind: "list"; listId: string }
  | { kind: "today" }
  | { kind: "next7" }
  | { kind: "tag"; tagId: string }
  | { kind: "search"; query: string };

/**
 * A group of tasks under a heading. `key` identifies the section; the
 * component turns it into a translated heading.
 */
export type Section =
  | { key: "all"; tasks: TaskRow[] }
  | { key: "overdue"; tasks: TaskRow[] }
  | { key: "day"; day: CalendarDate; tasks: TaskRow[] };

export interface Context {
  snapshot: Snapshot;
  zone: string;
  now: Date;
}

/** Days shown by "Next 7 days": today and the six after it (D-48). */
export const nextDays = 7;

/**
 * Open tasks in live lists. D-49: dropped tasks are not shown anywhere;
 * done tasks only through the Completed section.
 */
function openTasks(snapshot: Snapshot): TaskRow[] {
  const result: TaskRow[] = [];
  for (const task of snapshot.tasks.values()) {
    if (task.status === "open" && snapshot.lists.has(task.list_id)) {
      result.push(task);
    }
  }
  return result;
}

/** Lists in their manual order. */
export function sortedLists(snapshot: Snapshot): List[] {
  return [...snapshot.lists.values()].sort(comparePositions);
}

/** Tags by name, ignoring case, as the server lists them. */
export function sortedTags(snapshot: Snapshot): Tag[] {
  return [...snapshot.tags.values()].sort((a, b) => {
    const byName = a.name.toLowerCase().localeCompare(b.name.toLowerCase());
    return byName !== 0 ? byName : a.id < b.id ? -1 : 1;
  });
}

/** D-43, sort=position: the list's position, then the task's, then id. */
function byListThenPosition(snapshot: Snapshot): (a: TaskRow, b: TaskRow) => number {
  return (a, b) => {
    if (a.list_id !== b.list_id) {
      const listA = snapshot.lists.get(a.list_id);
      const listB = snapshot.lists.get(b.list_id);
      if (listA && listB) {
        return comparePositions(listA, listB);
      }
    }
    return comparePositions(a, b);
  };
}

/**
 * D-43, sort=due: by due date with undated tasks last, all-day before
 * timed, then by time, position and id.
 */
export function byDue(a: TaskRow, b: TaskRow): number {
  const dateA = a.due_date ?? "9999-12-31";
  const dateB = b.due_date ?? "9999-12-31";
  if (dateA !== dateB) {
    return dateA < dateB ? -1 : 1;
  }
  const timedA = a.due_time ? 1 : 0;
  const timedB = b.due_time ? 1 : 0;
  if (timedA !== timedB) {
    return timedA - timedB;
  }
  const timeA = a.due_time ?? "";
  const timeB = b.due_time ?? "";
  if (timeA !== timeB) {
    return timeA < timeB ? -1 : 1;
  }
  return comparePositions(a, b);
}

/** The sections of a view, in display order. */
export function sections(view: View, ctx: Context): Section[] {
  const { snapshot } = ctx;
  const today = dateIn(ctx.now, ctx.zone);
  switch (view.kind) {
    case "list": {
      const tasks = openTasks(snapshot)
        .filter((t) => t.list_id === view.listId)
        .sort(comparePositions);
      return [{ key: "all", tasks }];
    }
    case "today":
      return byDay(openTasks(snapshot), today, 1);
    case "next7":
      return byDay(openTasks(snapshot), today, nextDays);
    case "tag": {
      const tasks = openTasks(snapshot)
        .filter((t) => t.tag_ids.includes(view.tagId))
        .sort(byListThenPosition(snapshot));
      return [{ key: "all", tasks }];
    }
    case "search": {
      const matches = matcher(view.query);
      const tasks = openTasks(snapshot)
        .filter((t) => matches(t.title, t.description))
        .sort(byListThenPosition(snapshot));
      return [{ key: "all", tasks }];
    }
  }
}

/**
 * Overdue (due before today; D-30: the date as stored), then one section
 * per day from today. Days with no tasks are kept, so "Next 7 days" always
 * shows the week; Today has one day.
 */
function byDay(tasks: TaskRow[], today: CalendarDate, days: number): Section[] {
  const dated = tasks.filter((t) => t.due_date).sort(byDue);
  const overdue: TaskRow[] = [];
  const perDay: Section[] = [];
  for (let i = 0; i < days; i++) {
    perDay.push({ key: "day", day: today.add({ days: i }), tasks: [] });
  }
  for (const task of dated) {
    const offset = daysBetween(today, parseDay(task.due_date ?? ""));
    if (offset < 0) {
      overdue.push(task);
    } else if (offset < days) {
      perDay[offset]?.tasks.push(task);
    }
  }
  return [{ key: "overdue", tasks: overdue }, ...perDay];
}

/** Whole days from a to b (negative when b is earlier). */
function daysBetween(a: CalendarDate, b: CalendarDate): number {
  // Calendar dates have no time or zone, so counting through UTC midnight
  // is exact.
  const ms = b.toDate("UTC").getTime() - a.toDate("UTC").getTime();
  return Math.round(ms / 86_400_000);
}

/** Open-task counts for the sidebar. */
export interface Counts {
  today: number;
  next7: number;
  lists: ReadonlyMap<string, number>;
}

export function counts(ctx: Context): Counts {
  const today = dateIn(ctx.now, ctx.zone).toString();
  const lastDay = dateIn(ctx.now, ctx.zone)
    .add({ days: nextDays - 1 })
    .toString();
  const lists = new Map<string, number>();
  let dueToday = 0;
  let dueNext7 = 0;
  for (const task of openTasks(ctx.snapshot)) {
    lists.set(task.list_id, (lists.get(task.list_id) ?? 0) + 1);
    // "YYYY-MM-DD" strings compare like the dates they write.
    if (task.due_date && task.due_date <= today) dueToday++;
    if (task.due_date && task.due_date <= lastDay) dueNext7++;
  }
  return { today: dueToday, next7: dueNext7, lists };
}

/**
 * The query parameters of GET /completions that select the Completed
 * section of a view (D-54); null for views without one.
 */
export function completedScope(
  view: View,
  ctx: Context,
): { list_id?: string; tag_id?: string; due_to?: string } | null {
  const today = dateIn(ctx.now, ctx.zone);
  switch (view.kind) {
    case "list":
      return { list_id: view.listId };
    case "tag":
      return { tag_id: view.tagId };
    case "today":
      return { due_to: today.toString() };
    case "next7":
      return { due_to: today.add({ days: nextDays - 1 }).toString() };
    case "search":
      return null;
  }
}
