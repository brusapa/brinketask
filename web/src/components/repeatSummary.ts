// The one-line summary of a task's recurrence, e.g. "Every 2 weeks on Mon,
// Wed, until Dec 31", built from message templates so it is translatable.
import { parseDate } from "@internationalized/date";
import type { TFunction } from "i18next";

import { formatDay } from "../lib/format";
import { lastDay, parseRule, weekdayName } from "../lib/rrule";

export function describeRepeat(
  rrule: string | null | undefined,
  repeatFrom: "due" | "completion",
  t: TFunction,
  locale: string,
): string {
  if (!rrule) return t("repeat.none");
  const rule = parseRule(rrule);
  if (rule === null) return rrule; // never stored by the server; shown as is

  let text =
    repeatFrom === "completion"
      ? t(`repeat.afterCompletion.${rule.freq}`, { count: rule.interval })
      : t(`repeat.every.${rule.freq}`, { count: rule.interval });
  if (rule.byDay.length > 0) {
    const days = new Intl.ListFormat(locale, { style: "narrow", type: "conjunction" }).format(
      rule.byDay.map((d) => weekdayName(d, locale)),
    );
    text = t("repeat.onDays", { rule: text, days });
  }
  if (rule.byMonthDay === lastDay) {
    text = t("repeat.onLastDay", { rule: text });
  } else if (rule.byMonthDay !== null) {
    text = t("repeat.onMonthDay", { rule: text, day: rule.byMonthDay });
  }
  if (rule.count !== null) {
    text = t("repeat.times", { rule: text, count: rule.count });
  } else if (rule.until !== null) {
    text = t("repeat.until", { rule: text, date: formatDay(parseDate(rule.until), "UTC", locale) });
  }
  return text;
}
