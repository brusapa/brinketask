import { parseDate } from "@internationalized/date";
import { describe, expect, test } from "vitest";

import { task } from "../test/fixtures";
import { dateIn, dayBounds, dueInstant, isOverdue, msUntilNextDay } from "./dates";
import { formatDue } from "./format";

const madrid = "Europe/Madrid";
const tokyo = "Asia/Tokyo";
const listId = "l";

describe("dateIn", () => {
  test("the same instant is a different day in different zones", () => {
    const instant = new Date("2026-10-09T23:30:00Z");
    expect(dateIn(instant, madrid).toString()).toBe("2026-10-10");
    expect(dateIn(instant, "America/New_York").toString()).toBe("2026-10-09");
  });
});

// SPEC section 4, the four due types.
describe("dueInstant and isOverdue", () => {
  const now = new Date("2026-10-09T10:00:00Z"); // 12:00 in Madrid

  test("no date: never overdue", () => {
    const t = task({ id: "1", list_id: listId });
    expect(dueInstant(t, madrid)).toBeNull();
    expect(isOverdue(t, now, madrid)).toBe(false);
  });

  test("all-day: overdue once its day is over in the user's zone", () => {
    expect(isOverdue(task({ id: "1", list_id: listId, due_date: "2026-10-09" }), now, madrid)).toBe(
      false,
    );
    expect(isOverdue(task({ id: "1", list_id: listId, due_date: "2026-10-08" }), now, madrid)).toBe(
      true,
    );
    // At 16:00 UTC it is still the 9th in Madrid but already the 10th in Tokyo.
    const evening = new Date("2026-10-09T16:00:00Z");
    const due9th = task({ id: "1", list_id: listId, due_date: "2026-10-09" });
    expect(isOverdue(due9th, evening, madrid)).toBe(false);
    expect(isOverdue(due9th, evening, tokyo)).toBe(true);
  });

  test("floating time: read in the user's zone", () => {
    const t = task({ id: "1", list_id: listId, due_date: "2026-10-09", due_time: "11:30" });
    expect(dueInstant(t, madrid)?.toISOString()).toBe("2026-10-09T09:30:00.000Z");
    expect(isOverdue(t, now, madrid)).toBe(true);
    // The same task for a user in New York is due later.
    expect(isOverdue(t, now, "America/New_York")).toBe(false);
  });

  test("fixed time: read in its own zone, whatever the user's", () => {
    const t = task({
      id: "1",
      list_id: listId,
      due_date: "2026-10-09",
      due_time: "11:30",
      due_tz: tokyo,
    });
    expect(dueInstant(t, madrid)?.toISOString()).toBe("2026-10-09T02:30:00.000Z");
    expect(dueInstant(t, "America/New_York")?.toISOString()).toBe("2026-10-09T02:30:00.000Z");
  });

  test("a floating time in the spring-forward gap moves forward", () => {
    // 2026-03-29 02:30 does not exist in Madrid; clocks jump to 03:00.
    const t = task({ id: "1", list_id: listId, due_date: "2026-03-29", due_time: "02:30" });
    expect(dueInstant(t, madrid)?.toISOString()).toBe("2026-03-29T01:30:00.000Z");
  });
});

// The Completed section asks for [start, end) of the user's day (D-47).
describe("dayBounds", () => {
  test("a normal day", () => {
    expect(dayBounds(parseDate("2026-10-09"), madrid)).toEqual({
      from: "2026-10-08T22:00:00.000Z",
      to: "2026-10-09T22:00:00.000Z",
    });
  });

  test("the last Sunday of March has 23 hours in Madrid", () => {
    const { from, to } = dayBounds(parseDate("2026-03-29"), madrid);
    expect(Date.parse(to) - Date.parse(from)).toBe(23 * 3600_000);
  });

  test("the last Sunday of October has 25 hours in Madrid", () => {
    const { from, to } = dayBounds(parseDate("2026-10-25"), madrid);
    expect(Date.parse(to) - Date.parse(from)).toBe(25 * 3600_000);
  });

  test("time until the next local midnight", () => {
    expect(msUntilNextDay(new Date("2026-10-09T21:00:00Z"), madrid)).toBe(3600_000);
  });
});

describe("formatDue", () => {
  const today = parseDate("2026-10-09");

  test("the time when due today, the day otherwise", () => {
    const timed = task({ id: "1", list_id: listId, due_date: "2026-10-09", due_time: "14:30" });
    expect(formatDue(timed, today, madrid, "en")).toBe("2:30 PM");
    const later = task({ id: "1", list_id: listId, due_date: "2026-10-12" });
    expect(formatDue(later, today, madrid, "en")).toBe("Mon, Oct 12");
    expect(formatDue(task({ id: "1", list_id: listId }), today, madrid, "en")).toBe("");
  });

  test("a fixed time is shown in the user's zone", () => {
    const t = task({
      id: "1",
      list_id: listId,
      due_date: "2026-10-09",
      due_time: "21:00",
      due_tz: "America/New_York",
    });
    // 21:00 in New York is 03:00 on the 10th in Madrid.
    expect(formatDue(t, today, madrid, "en")).toBe("Sat, Oct 10");
  });
});
