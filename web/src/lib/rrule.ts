// The RRULE subset of SPEC section 5, for the recurrence editor: reading a
// stored rule into fields, writing fields back in the server's canonical
// form (D-59), and a short summary for the screen. Dates of occurrences
// are computed by the server only; nothing here does calendar arithmetic.

export const frequencies = ["DAILY", "WEEKLY", "MONTHLY", "YEARLY"] as const;
export type Frequency = (typeof frequencies)[number];

/** Days in RRULE order, Monday first, as the canonical form lists them. */
export const weekdays = ["MO", "TU", "WE", "TH", "FR", "SA", "SU"] as const;
export type Weekday = (typeof weekdays)[number];

/** BYMONTHDAY=-1, the last day of the month. */
export const lastDay = -1;

export interface Rule {
  freq: Frequency;
  /** 1 to 999. */
  interval: number;
  /** WEEKLY only; empty means "the due date's weekday". */
  byDay: Weekday[];
  /** MONTHLY only: 1-31, lastDay, or null for "the due date's day". */
  byMonthDay: number | null;
  /** Number of occurrences, or null. Exclusive with until. */
  count: number | null;
  /** Last date, "YYYY-MM-DD", inclusive, or null. */
  until: string | null;
}

/**
 * Reads a rule as the server stores it. Returns null for anything outside
 * the subset, which the server never stores.
 */
export function parseRule(text: string): Rule | null {
  const rule: Rule = {
    freq: "DAILY",
    interval: 1,
    byDay: [],
    byMonthDay: null,
    count: null,
    until: null,
  };
  let hasFreq = false;
  for (const part of text.toUpperCase().split(";")) {
    const [name, value, extra] = part.split("=");
    if (name === undefined || value === undefined || extra !== undefined || value === "")
      return null;
    switch (name) {
      case "FREQ": {
        const freq = frequencies.find((f) => f === value);
        if (freq === undefined) return null;
        rule.freq = freq;
        hasFreq = true;
        break;
      }
      case "INTERVAL":
        rule.interval = Number(value);
        if (!Number.isInteger(rule.interval) || rule.interval < 1 || rule.interval > 999)
          return null;
        break;
      case "BYDAY":
        for (const day of value.split(",")) {
          const known = weekdays.find((w) => w === day);
          if (known === undefined) return null;
          rule.byDay.push(known);
        }
        break;
      case "BYMONTHDAY":
        rule.byMonthDay = Number(value);
        if (!Number.isInteger(rule.byMonthDay)) return null;
        break;
      case "COUNT":
        rule.count = Number(value);
        if (!Number.isInteger(rule.count) || rule.count < 1) return null;
        break;
      case "UNTIL":
        if (!/^\d{8}$/.test(value)) return null;
        rule.until = `${value.slice(0, 4)}-${value.slice(4, 6)}-${value.slice(6, 8)}`;
        break;
      default:
        return null;
    }
  }
  return hasFreq ? rule : null;
}

/**
 * Writes a rule in the server's canonical form (D-59), so the server sees
 * no change when nothing changed. Parts that do not apply to the frequency
 * are left out.
 */
export function formatRule(rule: Rule): string {
  const parts = [`FREQ=${rule.freq}`];
  if (rule.interval > 1) parts.push(`INTERVAL=${rule.interval}`);
  if (rule.freq === "WEEKLY" && rule.byDay.length > 0) {
    parts.push(`BYDAY=${weekdays.filter((d) => rule.byDay.includes(d)).join(",")}`);
  }
  if (rule.freq === "MONTHLY" && rule.byMonthDay !== null)
    parts.push(`BYMONTHDAY=${rule.byMonthDay}`);
  if (rule.count !== null) {
    parts.push(`COUNT=${rule.count}`);
  } else if (rule.until !== null) {
    parts.push(`UNTIL=${rule.until.replaceAll("-", "")}`);
  }
  return parts.join(";");
}

/** The short name of a weekday in a locale, e.g. "Mon". */
export function weekdayName(day: Weekday, locale: string): string {
  // 2024-01-01 was a Monday; adding the day's index gives that weekday.
  const date = new Date(Date.UTC(2024, 0, 1 + weekdays.indexOf(day)));
  return new Intl.DateTimeFormat(locale, { weekday: "short", timeZone: "UTC" }).format(date);
}
