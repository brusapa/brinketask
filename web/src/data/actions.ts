// Every write the web client makes. Each action sends one API call (retried
// with the same body on network failures; writes are idempotent, D-04 and
// D-10), stores what the server returned in the replica, and reports
// failures through onError instead of throwing, so components only decide
// what to show.
//
// Some writes are optimistic: the replica changes at once and goes back if
// the server refuses. That covers what users repeat quickly (completing,
// reordering, ticking checklist items, editing fields); creations wait for
// the server, which answers in milliseconds.
import { mergePatch, unwrap, withRetry, type Api } from "../api/client";
import type { ChecklistItemPatch, ListPatch, TagPatch, Task, TaskPatch } from "../api/types";
import type { Clock } from "../lib/clock";
import { dateIn } from "../lib/dates";
import { uuidv7 } from "../lib/ids";
import { positionAtEnd } from "../lib/positions";
import type { Replica, TaskRow } from "./replica";
import type { Syncer } from "./sync";

export interface ActionsOptions {
  api: Api;
  replica: Replica;
  syncer: Syncer;
  clock: Clock;
  /** The profile zone (D-47), read when needed, since it can change. */
  zone: () => string;
  /** Called after a completion is recorded or undone. */
  onCompletionsChanged: () => void;
  /** Called with every failed write. */
  onError: (error: unknown) => void;
}

/** Fields of a new task; the rest take the contract's defaults. */
export interface NewTask {
  title: string;
  listId: string;
  dueDate?: string | undefined;
  tagIds?: string[] | undefined;
}

/** A recorded completion or skip: its id, for undo, and the task after it. */
export interface Recorded {
  completionId: string;
  task: Task;
}

/** The outcome of a write: whether it succeeded and what it returned. */
type Outcome<T> = { ok: true; value: T } | { ok: false };

export class Actions {
  constructor(private readonly o: ActionsOptions) {}

  private get api(): Api {
    return this.o.api;
  }

  private get replica(): Replica {
    return this.o.replica;
  }

  /**
   * Runs a write with retries. On failure it runs `undo` (which puts back
   * an optimistic change) and reports the error.
   */
  private async attempt<T>(write: () => Promise<T>, undo?: () => void): Promise<Outcome<T>> {
    try {
      return { ok: true, value: await withRetry(write) };
    } catch (err) {
      undo?.();
      this.o.onError(err);
      return { ok: false };
    }
  }

  /** Pulls changes a write caused beyond its own response. */
  private refresh(): void {
    this.o.syncer.pull().catch(this.o.onError);
  }

  // Lists ------------------------------------------------------------------

  async createList(name: string, color: string | null): Promise<string | undefined> {
    const lists = [...this.replica.getSnapshot().lists.values()];
    const body = {
      id: uuidv7(this.o.clock),
      name,
      color,
      position: positionAtEnd(lists.map((l) => l.position)),
    };
    const result = await this.attempt(() => unwrap(() => this.api.POST("/lists", { body })));
    if (!result.ok) return undefined;
    this.replica.putList(result.value);
    return result.value.id;
  }

  async updateList(id: string, patch: ListPatch): Promise<void> {
    const before = this.replica.getSnapshot().lists.get(id);
    if (!before) return;
    this.replica.setLocally("lists", { ...before, ...patch });
    const result = await this.attempt(
      () =>
        unwrap(() =>
          this.api.PATCH("/lists/{id}", {
            params: { path: { id } },
            body: patch,
            headers: mergePatch,
          }),
        ),
      () => {
        this.replica.setLocally("lists", before);
      },
    );
    if (result.ok) this.replica.putList(result.value);
  }

  /** Deletes a list and, on the server, its tasks (SPEC section 8). */
  async deleteList(id: string): Promise<boolean> {
    const snapshot = this.replica.getSnapshot();
    const before = snapshot.lists.get(id);
    if (!before) return false;
    const tasks = [...snapshot.tasks.values()].filter((t) => t.list_id === id);
    this.replica.remove("lists", [id]);
    this.replica.remove(
      "tasks",
      tasks.map((t) => t.id),
    );
    const result = await this.attempt(
      () => unwrap(() => this.api.DELETE("/lists/{id}", { params: { path: { id } } })),
      () => {
        this.replica.setLocally("lists", before);
        for (const task of tasks) this.replica.setLocally("tasks", task);
      },
    );
    // The tombstones of the tasks the server deleted with the list.
    if (result.ok) this.refresh();
    return result.ok;
  }

  async restoreList(id: string): Promise<boolean> {
    const result = await this.attempt(() =>
      unwrap(() => this.api.POST("/lists/{id}/restore", { params: { path: { id } } })),
    );
    if (!result.ok) return false;
    this.replica.putList(result.value);
    // Its tasks come back too, each with a new seq.
    this.refresh();
    return true;
  }

  // Tags -------------------------------------------------------------------

  async createTag(name: string): Promise<string | undefined> {
    const body = { id: uuidv7(this.o.clock), name, color: null };
    const result = await this.attempt(() => unwrap(() => this.api.POST("/tags", { body })));
    if (!result.ok) return undefined;
    this.replica.putTag(result.value);
    return result.value.id;
  }

  async updateTag(id: string, patch: TagPatch): Promise<void> {
    const result = await this.attempt(() =>
      unwrap(() =>
        this.api.PATCH("/tags/{id}", {
          params: { path: { id } },
          body: patch,
          headers: mergePatch,
        }),
      ),
    );
    if (result.ok) this.replica.putTag(result.value);
  }

  /** Deleting a tag also removes it from every task (D-19). */
  async deleteTag(id: string): Promise<void> {
    const before = this.replica.getSnapshot().tags.get(id);
    if (!before) return;
    this.replica.remove("tags", [id]);
    const result = await this.attempt(
      () => unwrap(() => this.api.DELETE("/tags/{id}", { params: { path: { id } } })),
      () => {
        this.replica.setLocally("tags", before);
      },
    );
    // The tasks that lost the tag changed on the server.
    if (result.ok) this.refresh();
  }

  // Tasks ------------------------------------------------------------------

  async createTask(input: NewTask): Promise<string | undefined> {
    const siblings = [...this.replica.getSnapshot().tasks.values()].filter(
      (t) => t.list_id === input.listId,
    );
    const body = {
      id: uuidv7(this.o.clock),
      list_id: input.listId,
      title: input.title,
      description: "",
      priority: 0,
      repeat_from: "due" as const,
      position: positionAtEnd(siblings.map((t) => t.position)),
      due_date: input.dueDate ?? null,
      tag_ids: input.tagIds ?? [],
    };
    const result = await this.attempt(() => unwrap(() => this.api.POST("/tasks", { body })));
    if (!result.ok) return undefined;
    this.replica.putTask(result.value);
    return result.value.id;
  }

  /**
   * Applies a merge patch to a task, optimistically. The patch must leave
   * the task valid (D-26): the detail panel clears dependent fields itself.
   */
  async updateTask(id: string, patch: TaskPatch): Promise<void> {
    const before = this.replica.getSnapshot().tasks.get(id);
    if (!before) return;
    // The spread copies the task and overwrites the patched fields.
    this.replica.setLocally("tasks", { ...before, ...patch });
    const result = await this.attempt(
      () =>
        unwrap(() =>
          this.api.PATCH("/tasks/{id}", {
            params: { path: { id } },
            body: patch,
            headers: mergePatch,
          }),
        ),
      () => {
        this.replica.setLocally("tasks", before);
      },
    );
    if (result.ok) this.replica.putTask(result.value);
  }

  async deleteTask(id: string): Promise<boolean> {
    const before = this.replica.getSnapshot().tasks.get(id);
    if (!before) return false;
    this.replica.remove("tasks", [id]);
    const result = await this.attempt(
      () => unwrap(() => this.api.DELETE("/tasks/{id}", { params: { path: { id } } })),
      () => {
        this.replica.setLocally("tasks", before);
      },
    );
    return result.ok;
  }

  async restoreTask(id: string): Promise<boolean> {
    const result = await this.attempt(() =>
      unwrap(() => this.api.POST("/tasks/{id}/restore", { params: { path: { id } } })),
    );
    if (!result.ok) return false;
    this.replica.putTask(result.value);
    return true;
  }

  /** Tasks with a completion or skip in flight; a second press waits. */
  private recording = new Set<string>();

  /**
   * Completes a task. Returns the completion id the undo toast needs and the
   * task as the server left it: done, or for a recurring task moved to its
   * next occurrence. The id is generated once and reused by retries, so a
   * retry after a lost response cannot complete twice (D-24);
   * occurrence_due_date is the date the user saw (D-10).
   */
  complete(id: string): Promise<Recorded | undefined> {
    return this.record(id, "complete");
  }

  /** Skips the current occurrence of a recurring task (R-6). */
  skip(id: string): Promise<Recorded | undefined> {
    return this.record(id, "skip");
  }

  private async record(id: string, action: "complete" | "skip"): Promise<Recorded | undefined> {
    const before = this.replica.getSnapshot().tasks.get(id);
    if (!before || before.status !== "open" || this.recording.has(id)) return undefined;
    const recurring = before.rrule !== null && before.rrule !== undefined;
    if (action === "skip" && !recurring) return undefined;
    this.recording.add(id);
    try {
      const body = {
        completion_id: uuidv7(this.o.clock),
        occurrence_due_date: before.due_date ?? null,
      };
      // A task without a rule simply becomes done, so the screen can show
      // that at once. A recurring one moves to a date only the server
      // computes (D-11): its row waits for the answer.
      if (!recurring) {
        this.replica.setLocally("tasks", {
          ...before,
          status: "done",
          completed_at: this.o.clock.now().toISOString(),
        });
      }
      const params = { params: { path: { id } }, body };
      const result = await this.attempt(
        () =>
          unwrap(() =>
            action === "complete"
              ? this.api.POST("/tasks/{id}/complete", params)
              : this.api.POST("/tasks/{id}/skip", params),
          ),
        () => {
          this.replica.setLocally("tasks", before);
        },
      );
      if (!result.ok) return undefined;
      this.replica.putTask(result.value.task);
      this.o.onCompletionsChanged();
      return result.value.applied
        ? { completionId: body.completion_id, task: result.value.task }
        : undefined;
    } finally {
      this.recording.delete(id);
    }
  }

  /** Undoes a completion (D-25); only the latest one can be undone. */
  async uncomplete(taskId: string, completionId: string): Promise<void> {
    const result = await this.attempt(() =>
      unwrap(() =>
        this.api.POST("/tasks/{id}/uncomplete", {
          params: { path: { id: taskId } },
          body: { completion_id: completionId },
        }),
      ),
    );
    if (!result.ok) return;
    this.replica.putTask(result.value.task);
    this.o.onCompletionsChanged();
  }

  /** Moves a task to the end of another list. */
  async moveTask(id: string, listId: string): Promise<void> {
    const siblings = [...this.replica.getSnapshot().tasks.values()].filter(
      (t) => t.list_id === listId,
    );
    await this.updateTask(id, {
      list_id: listId,
      position: positionAtEnd(siblings.map((t) => t.position)),
    });
  }

  // Checklist --------------------------------------------------------------

  async addItem(taskId: string, title: string): Promise<void> {
    const siblings = [...this.replica.getSnapshot().items.values()].filter(
      (i) => i.task_id === taskId,
    );
    const body = {
      id: uuidv7(this.o.clock),
      title,
      is_done: false,
      position: positionAtEnd(siblings.map((i) => i.position)),
    };
    const result = await this.attempt(() =>
      unwrap(() =>
        this.api.POST("/tasks/{id}/checklist-items", { params: { path: { id: taskId } }, body }),
      ),
    );
    if (result.ok) this.replica.putItem(result.value);
  }

  async updateItem(id: string, patch: ChecklistItemPatch): Promise<void> {
    const before = this.replica.getSnapshot().items.get(id);
    if (!before) return;
    this.replica.setLocally("items", { ...before, ...patch });
    const result = await this.attempt(
      () =>
        unwrap(() =>
          this.api.PATCH("/checklist-items/{id}", {
            params: { path: { id } },
            body: patch,
            headers: mergePatch,
          }),
        ),
      () => {
        this.replica.setLocally("items", before);
      },
    );
    if (result.ok) this.replica.putItem(result.value);
  }

  async deleteItem(id: string): Promise<void> {
    const before = this.replica.getSnapshot().items.get(id);
    if (!before) return;
    this.replica.remove("items", [id]);
    await this.attempt(
      () => unwrap(() => this.api.DELETE("/checklist-items/{id}", { params: { path: { id } } })),
      () => {
        this.replica.setLocally("items", before);
      },
    );
  }

  /** The task as the replica has it now. */
  currentTask(id: string): TaskRow | undefined {
    return this.replica.getSnapshot().tasks.get(id);
  }

  /** Today's date in the profile zone, for quick add (D-53). */
  today(): string {
    return dateIn(this.o.clock.now(), this.o.zone()).toString();
  }
}
