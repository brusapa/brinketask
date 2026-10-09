import { expect, test } from "vitest";

import { i18n } from "../i18n";
import { describeRepeat } from "./repeatSummary";

const t = i18n.t.bind(i18n);

test.each([
  [null, "due", "Does not repeat"],
  ["FREQ=DAILY", "due", "Every day"],
  ["FREQ=DAILY;INTERVAL=3", "due", "Every 3 days"],
  ["FREQ=WEEKLY;BYDAY=MO,WE", "due", "Every week on Mon, Wed"],
  ["FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE,FR", "due", "Every 2 weeks on Mon, Wed, Fri"],
  ["FREQ=MONTHLY;BYMONTHDAY=-1", "due", "Every month on the last day"],
  ["FREQ=MONTHLY;BYMONTHDAY=15", "due", "Every month on day 15"],
  ["FREQ=YEARLY;COUNT=1", "due", "Every year, once"],
  ["FREQ=DAILY;COUNT=5", "due", "Every day, 5 times"],
  ["FREQ=WEEKLY;UNTIL=20261231", "due", "Every week, until Thu, Dec 31"],
  ["FREQ=DAILY;INTERVAL=3", "completion", "3 days after completion"],
  ["FREQ=MONTHLY", "completion", "1 month after completion"],
] as const)("%s (%s) reads %j", (rule, from, text) => {
  expect(describeRepeat(rule, from, t, "en")).toBe(text);
});
