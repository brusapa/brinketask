import { describe, expect, test } from "vitest";

import { list, page, task, tombstone } from "../test/fixtures";
import { FakeServer, json, problem } from "../test/fakeFetch";
import { Replica } from "./replica";
import { Syncer } from "./sync";

function setup() {
  const server = new FakeServer();
  const replica = new Replica();
  const syncer = new Syncer(server.api(), replica);
  return { server, replica, syncer };
}

describe("Syncer", () => {
  test("starts with the full state, then asks for changes since the cursor", async () => {
    const { server, replica, syncer } = setup();
    const t = task({ id: "t", list_id: "a" });
    server.on(
      "GET",
      "/sync/changes",
      json(page({ lists: [list({ id: "a" })], tasks: [t], next_cursor: "5" })),
      json(page({ tasks: [tombstone(t)], next_cursor: "6" })),
    );
    await syncer.pull();
    expect(replica.getSnapshot().tasks.has("t")).toBe(true);
    expect(server.requests[0]?.query.has("cursor")).toBe(false);

    await syncer.pull();
    expect(server.requests[1]?.query.get("cursor")).toBe("5");
    expect(replica.getSnapshot().tasks.has("t")).toBe(false);
    expect(replica.cursor).toBe("6");
  });

  test("follows has_more to the end", async () => {
    const { server, replica, syncer } = setup();
    server.on(
      "GET",
      "/sync/changes",
      json(page({ lists: [list({ id: "a" })], has_more: true, next_cursor: "1" })),
      json(page({ lists: [list({ id: "b" })], has_more: true, next_cursor: "2" })),
      json(page({ lists: [list({ id: "c" })], next_cursor: "3" })),
    );
    await syncer.pull();
    expect([...replica.getSnapshot().lists.keys()].sort()).toEqual(["a", "b", "c"]);
    expect(server.requests.map((r) => r.query.get("cursor"))).toEqual([null, "1", "2"]);
  });

  // D-21: a 410 throws the replica away and starts over.
  test("an expired cursor restarts from the full state", async () => {
    const { server, replica, syncer } = setup();
    replica.replaceAll([page({ lists: [list({ id: "gone" })] })], "old");
    server.on(
      "GET",
      "/sync/changes",
      problem(410, "cursor_expired"),
      json(page({ lists: [list({ id: "a" })], next_cursor: "9" })),
    );
    await syncer.pull();
    expect([...replica.getSnapshot().lists.keys()]).toEqual(["a"]);
    expect(replica.cursor).toBe("9");
    expect(server.requests[1]?.query.has("cursor")).toBe(false);
  });

  test("pulls requested while one runs are folded into one more", async () => {
    const { server, syncer } = setup();
    server.on("GET", "/sync/changes", json(page({ next_cursor: "1" })));
    await Promise.all([syncer.pull(), syncer.pull(), syncer.pull()]);
    expect(server.requests.length).toBe(2);
  });

  test("other errors reach the caller", async () => {
    const { server, syncer } = setup();
    server.on("GET", "/sync/changes", problem(500, "internal"));
    await expect(syncer.pull()).rejects.toThrow("HTTP 500");
  });
});
