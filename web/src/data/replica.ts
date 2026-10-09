// The client's full local replica of the user's data (D-31), built from
// /sync/changes and from the responses of the client's own writes. Views
// and sidebar counts are computed from it, not fetched.
//
// The replica is an immutable snapshot replaced on every change, so React
// can tell what changed by comparing references (useSyncExternalStore).
import type { ChangesPage, ChecklistItem, List, Tag, Task } from "../api/types";

/** A task as the replica keeps it: its children live in their own maps. */
export type TaskRow = Omit<Task, "checklist_items" | "reminders">;

export interface Snapshot {
  readonly lists: ReadonlyMap<string, List>;
  readonly tasks: ReadonlyMap<string, TaskRow>;
  readonly items: ReadonlyMap<string, ChecklistItem>;
  readonly tags: ReadonlyMap<string, Tag>;
}

export type Kind = keyof Snapshot;

/** The value type stored under each kind. */
export interface ValueOf {
  lists: List;
  tasks: TaskRow;
  items: ChecklistItem;
  tags: Tag;
}

/** The fields every resource shares. */
interface Versioned {
  id: string;
  version: number;
  deleted_at?: string | null;
}

/** The maps being changed by one update; see Replica.update. */
type Draft = { -readonly [K in Kind]: Map<string, ValueOf[K]> };

export class Replica {
  private snapshot: Snapshot = {
    lists: new Map(),
    tasks: new Map(),
    items: new Map(),
    tags: new Map(),
  };
  private listeners = new Set<() => void>();
  /**
   * Resources known to be deleted, with the version they had. A response to
   * a request sent before the delete (a sync page, say) can still carry the
   * live version; it must not bring the resource back.
   */
  private removed = new Map<string, number>();

  /** The /sync/changes cursor up to which the replica is complete. */
  cursor: string | null = null;

  // Arrow functions keep `this` when React calls them unbound.
  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  getSnapshot = (): Snapshot => this.snapshot;

  /** Replaces everything with a full state (a sync without cursor). */
  replaceAll(pages: readonly ChangesPage[], cursor: string): void {
    this.removed.clear();
    this.update((draft) => {
      draft.lists.clear();
      draft.tasks.clear();
      draft.items.clear();
      draft.tags.clear();
      for (const page of pages) {
        this.applyPage(draft, page);
      }
    });
    this.cursor = cursor;
  }

  /** Applies a page of changes since the cursor. */
  applyChanges(page: ChangesPage): void {
    this.update((draft) => {
      this.applyPage(draft, page);
    });
    this.cursor = page.next_cursor;
  }

  putList(list: List): void {
    this.update((draft) => {
      this.put(draft.lists, list);
    });
  }

  putTag(tag: Tag): void {
    this.update((draft) => {
      this.put(draft.tags, tag);
    });
  }

  putItem(item: ChecklistItem): void {
    this.update((draft) => {
      this.put(draft.items, item);
    });
  }

  /** Stores a task; the checklist a write returns goes to its own map. */
  putTask(task: Task): void {
    this.update((draft) => {
      this.putTaskInto(draft, task);
    });
  }

  /** Removes resources this client deleted. */
  remove(kind: Kind, ids: readonly string[]): void {
    this.update((draft) => {
      const map: Map<string, Versioned> = draft[kind];
      for (const id of ids) {
        const current = map.get(id);
        if (current !== undefined) {
          this.removed.set(id, current.version);
          map.delete(id);
        }
      }
    });
  }

  /**
   * Stores a locally edited copy before the server has answered (an
   * optimistic update), or puts back the copy from before it. The value
   * keeps the version it had, so the server's answer, one version higher,
   * replaces it.
   */
  setLocally<K extends Kind>(kind: K, value: ValueOf[K]): void {
    this.update((draft) => {
      const map: Map<string, ValueOf[K]> = draft[kind];
      map.set(value.id, value);
      this.removed.delete(value.id);
    });
  }

  private applyPage(draft: Draft, page: ChangesPage): void {
    for (const list of page.lists) this.put(draft.lists, list);
    for (const task of page.tasks) this.putTaskInto(draft, task);
    for (const item of page.checklist_items) this.put(draft.items, item);
    for (const tag of page.tags) this.put(draft.tags, tag);
  }

  private putTaskInto(draft: Draft, task: Task): void {
    // Destructuring with a rest element: `row` is the task without the two
    // child arrays.
    const { checklist_items: items, reminders, ...row } = task;
    this.put(draft.tasks, row);
    for (const item of items ?? []) {
      this.put(draft.items, item);
    }
  }

  /**
   * Stores one resource, unless the replica already has a newer version,
   * and drops it when it is a tombstone.
   */
  private put<V extends Versioned>(map: Map<string, V>, value: V): void {
    const removedAt = this.removed.get(value.id);
    if (removedAt !== undefined && value.version <= removedAt) {
      return;
    }
    const current = map.get(value.id);
    if (current !== undefined && current.version > value.version) {
      return;
    }
    if (value.deleted_at) {
      map.delete(value.id);
      this.removed.set(value.id, value.version);
    } else {
      map.set(value.id, value);
      this.removed.delete(value.id);
    }
  }

  /**
   * Copy-on-write: the change works on copies of the maps, which become the
   * new snapshot, so the previous snapshot stays untouched for whoever
   * still renders it. Copying 10,000 tasks takes about a millisecond.
   */
  private update(change: (draft: Draft) => void): void {
    const draft: Draft = {
      lists: new Map(this.snapshot.lists),
      tasks: new Map(this.snapshot.tasks),
      items: new Map(this.snapshot.items),
      tags: new Map(this.snapshot.tags),
    };
    change(draft);
    this.snapshot = draft;
    for (const listener of this.listeners) {
      listener();
    }
  }
}
