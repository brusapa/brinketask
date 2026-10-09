import { describe, expect, test } from "vitest";

import { FixedClock } from "./clock";
import { uuidv7 } from "./ids";
import { comparePositions, positionAtEnd, positionAtStart, positionBetween } from "./positions";
import { fold, matcher } from "./search";

describe("uuidv7", () => {
  test("has the version 7 layout and the clock's time", () => {
    const clock = new FixedClock("2026-10-09T10:00:00.123Z");
    const id = uuidv7(clock);
    expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    const ms = parseInt(id.replace(/-/g, "").slice(0, 12), 16);
    expect(ms).toBe(Date.parse("2026-10-09T10:00:00.123Z"));
  });

  test("later ids sort later; equal times still differ", () => {
    const clock = new FixedClock("2026-10-09T10:00:00Z");
    const first = uuidv7(clock);
    expect(uuidv7(clock)).not.toBe(first);
    clock.advance(1);
    expect(uuidv7(clock) > first).toBe(true);
  });
});

describe("positions", () => {
  test("between, at the start and at the end", () => {
    const a = positionBetween(null, null);
    const b = positionBetween(a, null);
    const mid = positionBetween(a, b);
    expect(a < mid && mid < b).toBe(true);
    expect(positionAtEnd([a, b]) > b).toBe(true);
    expect(positionAtStart([a, b]) < a).toBe(true);
    expect(positionAtEnd([])).toBe(positionBetween(null, null));
  });

  test("the inbox position a0 works with the library", () => {
    expect(positionBetween("a0", null) > "a0").toBe(true);
  });

  // Positions written by another client need not follow the library's
  // format; the fallback still finds a string strictly between (D-51).
  test.each([
    ["b", "c"],
    ["a", "aB"],
    ["zz", null],
    [null, "!"],
    [null, "b"],
    ["x", "x0"],
    ["hello world", "hello worle"],
  ] as const)("between %j and %j", (before, after) => {
    const p = positionBetween(before, after);
    expect(p.length).toBeGreaterThan(0);
    if (before !== null) expect(p > before).toBe(true);
    if (after !== null) expect(p < after).toBe(true);
  });

  test("repeated inserts at the same spot stay ordered", () => {
    let low = "a0";
    const high = "a1";
    for (let i = 0; i < 50; i++) {
      const p = positionBetween(low, high);
      expect(p > low && p < high).toBe(true);
      low = p;
    }
    expect(low.length).toBeLessThanOrEqual(100);
  });

  test("ties are broken by id, as on the server", () => {
    const rows = [
      { position: "a1", id: "b" },
      { position: "a1", id: "a" },
      { position: "Z", id: "z" },
    ];
    expect(rows.sort(comparePositions).map((r) => r.id)).toEqual(["z", "a", "b"]);
  });
});

describe("search", () => {
  test("ignores case and accents (D-52)", () => {
    expect(fold("Café ÑOÑO")).toBe("cafe nono");
    const matches = matcher("CAFE");
    expect(matches("Un café", "")).toBe(true);
    expect(matches("Tea", "buy cafe beans")).toBe(true);
    expect(matches("Tea", "")).toBe(false);
  });

  test("an empty query matches nothing", () => {
    expect(matcher("  ")("anything", "")).toBe(false);
  });
});
