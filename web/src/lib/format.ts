// Display of dates and times with the platform's locale-aware APIs (SPEC
// section 9). The locale is the UI language, so dates read the same way as
// the rest of the interface.
import type { CalendarDate } from "@internationalized/date";

import type { Task } from "../api/types";
import { dateIn, dueInstant, parseDay } from "./dates";

/**
 * The label of a task's due column (DESIGN.md section 5): the time when it
 * is due today, the weekday and date otherwise. A fixed-time task is shown
 * in the user's zone.
 */
export function formatDue(
  task: Task,
  today: CalendarDate,
  userZone: string,
  locale: string,
): string {
  if (!task.due_date) {
    return "";
  }
  const instant = dueInstant(task, userZone);
  const day = instant !== null ? dateIn(instant, userZone) : parseDay(task.due_date);
  if (instant !== null && day.compare(today) === 0) {
    return formatTime(instant, userZone, locale);
  }
  return formatDay(day, userZone, locale);
}

/** "Mon, Oct 12", in the given year only when it is not the current one. */
export function formatDay(
  day: CalendarDate,
  zone: string,
  locale: string,
  currentYear?: number,
): string {
  const options: Intl.DateTimeFormatOptions = {
    weekday: "short",
    month: "short",
    day: "numeric",
    timeZone: zone,
  };
  if (currentYear !== undefined && day.year !== currentYear) {
    options.year = "numeric";
  }
  return new Intl.DateTimeFormat(locale, options).format(day.toDate(zone));
}

/** "14:30" or "2:30 PM", as the locale writes times. */
export function formatTime(instant: Date, zone: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, {
    hour: "numeric",
    minute: "2-digit",
    timeZone: zone,
  }).format(instant);
}

/** A long date, for headers: "Friday, October 9". */
export function formatLongDay(day: CalendarDate, zone: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, {
    weekday: "long",
    month: "long",
    day: "numeric",
    timeZone: zone,
  }).format(day.toDate(zone));
}
