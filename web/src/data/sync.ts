// Keeps the replica in step with the server through /sync/changes (SPEC
// section 8): the full state on start, then the changes since the cursor
// whenever the client regains focus or a write touched more than its
// response shows (deleting a list deletes its tasks too).
import { ApiError, unwrap, type Api } from "../api/client";
import type { ChangesPage } from "../api/types";
import type { Replica } from "./replica";

/** Rows per page; the contract's maximum, to keep round trips few. */
const pageSize = 500;

export class Syncer {
  private running: Promise<void> | null = null;
  /** Counts pull() calls, so a running pull can tell whether more came in. */
  private requests = 0;

  constructor(
    private readonly api: Api,
    private readonly replica: Replica,
  ) {}

  /**
   * Brings the replica up to date. Calls made while a pull runs are folded
   * into one more pull after it, so a burst of focus events or writes costs
   * at most two round trips.
   */
  pull(): Promise<void> {
    this.requests++;
    if (this.running !== null) {
      return this.running;
    }
    this.running = (async () => {
      try {
        let served: number;
        do {
          served = this.requests;
          await this.pullOnce();
        } while (served !== this.requests);
      } finally {
        this.running = null;
      }
    })();
    return this.running;
  }

  private async pullOnce(): Promise<void> {
    const cursor = this.replica.cursor;
    if (cursor === null) {
      await this.pullFull();
      return;
    }
    try {
      let page = await this.fetchPage(cursor);
      this.replica.applyChanges(page);
      while (page.has_more) {
        page = await this.fetchPage(page.next_cursor);
        this.replica.applyChanges(page);
      }
    } catch (err) {
      // 410: the server purged tombstones newer than our cursor (D-21), so
      // the replica may hold deleted resources. Start over.
      if (err instanceof ApiError && err.code === "cursor_expired") {
        await this.pullFull();
        return;
      }
      throw err;
    }
  }

  /**
   * Reads the whole state, page by page, and swaps it in at the end, so the
   * screen never shows a half-loaded replica.
   */
  private async pullFull(): Promise<void> {
    let page = await this.fetchPage(null);
    const pages = [page];
    while (page.has_more) {
      page = await this.fetchPage(page.next_cursor);
      pages.push(page);
    }
    this.replica.replaceAll(pages, page.next_cursor);
  }

  private fetchPage(cursor: string | null): Promise<ChangesPage> {
    const query = cursor === null ? { limit: pageSize } : { cursor, limit: pageSize };
    return unwrap(() => this.api.GET("/sync/changes", { params: { query } }));
  }
}
