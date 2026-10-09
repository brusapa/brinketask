// Builders of API resources for tests, with valid defaults.
import type { ChangesPage, ChecklistItem, List, Tag, Task } from "../api/types";

const timestamp = "2026-10-01T10:00:00Z";

export function list(fields: Partial<List> & { id: string }): List {
  return {
    name: "List",
    color: null,
    position: "a1",
    is_inbox: false,
    role: "owner",
    version: 1,
    created_at: timestamp,
    updated_at: timestamp,
    deleted_at: null,
    ...fields,
  };
}

export function task(fields: Partial<Task> & { id: string; list_id: string }): Task {
  return {
    title: "Task",
    description: "",
    status: "open",
    priority: 0,
    position: "a0",
    due_date: null,
    due_time: null,
    due_tz: null,
    rrule: null,
    repeat_from: "due",
    recurrence_done_count: 0,
    completed_at: null,
    tag_ids: [],
    version: 1,
    created_at: timestamp,
    updated_at: timestamp,
    deleted_at: null,
    ...fields,
  };
}

export function item(
  fields: Partial<ChecklistItem> & { id: string; task_id: string },
): ChecklistItem {
  return {
    title: "Item",
    is_done: false,
    position: "a0",
    version: 1,
    created_at: timestamp,
    updated_at: timestamp,
    deleted_at: null,
    ...fields,
  };
}

export function tag(fields: Partial<Tag> & { id: string }): Tag {
  return {
    name: "tag",
    color: null,
    version: 1,
    created_at: timestamp,
    updated_at: timestamp,
    deleted_at: null,
    ...fields,
  };
}

export function page(fields: Partial<ChangesPage> = {}): ChangesPage {
  return {
    lists: [],
    tasks: [],
    checklist_items: [],
    reminders: [],
    tags: [],
    next_cursor: "c1",
    has_more: false,
    ...fields,
  };
}

/** A stored copy of a resource, marked deleted. */
export function tombstone<T extends { version: number }>(resource: T): T {
  return { ...resource, version: resource.version + 1, deleted_at: timestamp };
}
