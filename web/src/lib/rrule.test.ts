import { describe, expect, test } from "vitest";

import { formatRule, parseRule, weekdayName } from "./rrule";

describe("rrule", () => {
  // The canonical forms the server writes (D-59) read and write back
  // unchanged.
  test.each([
    "FREQ=DAILY",
    "FREQ=DAILY;INTERVAL=3",
    "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE",
    "FREQ=MONTHLY;BYMONTHDAY=-1",
    "FREQ=MONTHLY;BYMONTHDAY=15;COUNT=6",
    "FREQ=YEARLY;UNTIL=20301231",
  ])("%s round-trips", (text) => {
    const rule = parseRule(text);
    expect(rule).not.toBeNull();
    expect(
      formatRule(
        rule ?? {
          freq: "DAILY",
          interval: 1,
          byDay: [],
          byMonthDay: null,
          count: null,
          until: null,
        },
      ),
    ).toBe(text);
  });

  test("fields", () => {
    expect(parseRule("FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE;UNTIL=20261231")).toEqual({
      freq: "WEEKLY",
      interval: 2,
      byDay: ["MO", "WE"],
      byMonthDay: null,
      count: null,
      until: "2026-12-31",
    });
  });

  test("writing orders days and drops parts that do not apply", () => {
    expect(
      formatRule({
        freq: "WEEKLY",
        interval: 1,
        byDay: ["FR", "MO"],
        byMonthDay: 3,
        count: null,
        until: null,
      }),
    ).toBe("FREQ=WEEKLY;BYDAY=MO,FR");
    expect(
      formatRule({
        freq: "DAILY",
        interval: 1,
        byDay: ["MO"],
        byMonthDay: null,
        count: 3,
        until: "2026-12-31",
      }),
    ).toBe("FREQ=DAILY;COUNT=3");
  });

  test.each([
    "",
    "FREQ=HOURLY",
    "INTERVAL=2",
    "FREQ=DAILY;BYHOUR=9",
    "FREQ=WEEKLY;BYDAY=1MO",
    "FREQ=DAILY;UNTIL=2026",
  ])("%j is outside the subset", (text) => {
    expect(parseRule(text)).toBeNull();
  });

  test("weekday names follow the locale", () => {
    expect(weekdayName("MO", "en")).toBe("Mon");
    expect(weekdayName("SU", "en")).toBe("Sun");
  });
});
