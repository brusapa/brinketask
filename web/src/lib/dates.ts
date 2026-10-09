// Calendar logic of the client: "today", overdue state and day bounds, all
// in the profile zone (D-47). @internationalized/date does the zone
// arithmetic, including daylight saving changes, which plain Date cannot
// do for a zone other than the browser's.
import {
  type CalendarDate,
  parseDate,
  parseDateTime,
  toCalendarDate,
  fromAbsolute,
  toZoned,
} from "@internationalized/date";

import type { Task } from "../api/types";

export type { CalendarDate };

/** The calendar date of an instant in a zone. */
export function dateIn(instant: Date, zone: string): CalendarDate {
  return toCalendarDate(fromAbsolute(instant.getTime(), zone));
}

/** "YYYY-MM-DD" as a calendar date. */
export function parseDay(day: string): CalendarDate {
  return parseDate(day);
}

/**
 * The instant a timed task is due, or null for undated and all-day tasks.
 * A floating time is read in the user's zone, a fixed one in its own
 * (SPEC section 4, due types 3 and 4).
 */
export function dueInstant(task: Task, userZone: string): Date | null {
  if (!task.due_date || !task.due_time) {
    return null;
  }
  const local = parseDateTime(`${task.due_date}T${task.due_time}`);
  // "compatible" picks the later instant in a gap and the earlier one in an
  // overlap, as RFC 5545 does for local times in a daylight saving change.
  return toZoned(local, task.due_tz ?? userZone, "compatible").toDate();
}

/**
 * Whether an open task is overdue: an all-day task once its day is over, a
 * timed task once its instant has passed.
 */
export function isOverdue(task: Task, now: Date, userZone: string): boolean {
  if (!task.due_date) {
    return false;
  }
  const instant = dueInstant(task, userZone);
  if (instant !== null) {
    return instant.getTime() < now.getTime();
  }
  return parseDay(task.due_date).compare(dateIn(now, userZone)) < 0;
}

/**
 * The instants where a calendar day starts and ends in a zone, as the
 * half-open range [from, to) that GET /completions takes. A day is 23 or 25
 * hours long when the clocks change.
 */
export function dayBounds(day: CalendarDate, zone: string): { from: string; to: string } {
  return {
    from: toZoned(day, zone).toDate().toISOString(),
    to: toZoned(day.add({ days: 1 }), zone)
      .toDate()
      .toISOString(),
  };
}

/** Milliseconds from now until the next midnight in the zone. */
export function msUntilNextDay(now: Date, zone: string): number {
  const tomorrow = dateIn(now, zone).add({ days: 1 });
  return toZoned(tomorrow, zone).toDate().getTime() - now.getTime();
}
