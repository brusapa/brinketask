// The recurrence editor (SPEC section 9: "limited to the subset"): how often,
// on which weekdays or day of the month, when the series ends, and whether
// the next date counts from the due date or from the completion (R-3).
// Saving sends one merge patch with the rule in canonical form.
import { parseDate, type CalendarDate } from "@internationalized/date";
import { Repeat } from "lucide-react";
import { useState } from "react";
import {
  Button,
  DateField,
  DateInput,
  DateSegment,
  Dialog,
  DialogTrigger,
  Group,
  Input,
  Label,
  ListBox,
  ListBoxItem,
  NumberField,
  Popover,
  RadioButton,
  RadioField,
  RadioGroup,
  Select,
  SelectValue,
  ToggleButton,
  ToggleButtonGroup,
} from "react-aria-components";
import { useTranslation } from "react-i18next";

import type { TaskPatch } from "../../api/types";
import { useServices } from "../../app/services";
import type { TaskRow } from "../../data/replica";
import {
  formatRule,
  frequencies,
  lastDay,
  parseRule,
  weekdayName,
  weekdays,
  type Frequency,
  type Rule,
} from "../../lib/rrule";
import { describeRepeat } from "../repeatSummary";

export function RepeatEditor({
  task,
  onPatch,
}: {
  task: TaskRow;
  onPatch: (patch: TaskPatch) => void;
}) {
  const { t, i18n } = useTranslation();
  const summary = describeRepeat(task.rrule, task.repeat_from, t, i18n.language);
  return (
    <DialogTrigger>
      <Button className="header-button" aria-label={t("repeatEditor.edit", { summary })}>
        <Repeat size={16} strokeWidth={1.5} aria-hidden="true" />
        <span>{summary}</span>
      </Button>
      <Popover className="popover repeat-popover" placement="bottom start">
        <Dialog className="dialog" aria-label={t("repeatEditor.title")}>
          {({ close }) => <RepeatForm task={task} onPatch={onPatch} onDone={close} />}
        </Dialog>
      </Popover>
    </DialogTrigger>
  );
}

type Ends = "never" | "count" | "until";
type MonthMode = "dueDay" | "lastDay";

function RepeatForm({
  task,
  onPatch,
  onDone,
}: {
  task: TaskRow;
  onPatch: (patch: TaskPatch) => void;
  onDone: () => void;
}) {
  const { t, i18n } = useTranslation();
  const { actions } = useServices();
  const initial = (task.rrule ? parseRule(task.rrule) : null) ?? defaultRule();
  const [freq, setFreq] = useState<Frequency | "none">(task.rrule ? initial.freq : "none");
  const [interval, setInterval] = useState(initial.interval);
  const [byDay, setByDay] = useState(initial.byDay);
  const [monthMode, setMonthMode] = useState<MonthMode>(
    initial.byMonthDay === lastDay ? "lastDay" : "dueDay",
  );
  const [ends, setEnds] = useState<Ends>(
    initial.count !== null ? "count" : initial.until !== null ? "until" : "never",
  );
  const [count, setCount] = useState(initial.count ?? 10);
  const [until, setUntil] = useState<CalendarDate | null>(
    initial.until !== null ? parseDate(initial.until) : null,
  );
  const [repeatFrom, setRepeatFrom] = useState(task.repeat_from);
  const fromCompletion = repeatFrom === "completion";

  const save = () => {
    if (freq === "none") {
      if (task.rrule) onPatch({ rrule: null });
      onDone();
      return;
    }
    // R-3: counting from the completion takes no weekdays or month day.
    const rule: Rule = {
      freq,
      interval,
      byDay: freq === "WEEKLY" && !fromCompletion ? byDay : [],
      byMonthDay: freq === "MONTHLY" && !fromCompletion && monthMode === "lastDay" ? lastDay : null,
      count: ends === "count" ? count : null,
      until: ends === "until" && until !== null ? until.toString() : null,
    };
    const patch: TaskPatch = { rrule: formatRule(rule), repeat_from: repeatFrom };
    // A rule needs a due date (D-26): an undated task starts today.
    if (!task.due_date) patch.due_date = actions.today();
    onPatch(patch);
    onDone();
  };

  return (
    <div className="repeat-form">
      <Select
        className="field"
        value={freq}
        onChange={(key) => {
          if (key !== null) setFreq(key as Frequency | "none");
        }}
      >
        <Label>{t("repeatEditor.frequency")}</Label>
        <Button className="input select-button">
          <SelectValue />
        </Button>
        <Popover className="popover">
          <ListBox className="listbox">
            <ListBoxItem id="none" className="menu-item">
              {t("repeat.none")}
            </ListBoxItem>
            {frequencies.map((f) => (
              <ListBoxItem key={f} id={f} className="menu-item">
                {t(`repeatEditor.freq.${f}`)}
              </ListBoxItem>
            ))}
          </ListBox>
        </Popover>
      </Select>

      {freq !== "none" && (
        <>
          <NumberField
            className="field"
            value={interval}
            onChange={setInterval}
            minValue={1}
            maxValue={999}
            step={1}
          >
            <Label>{t("repeatEditor.interval")}</Label>
            <Group className="number-group">
              <Input className="input number-input" />
              <span>{t(`repeatEditor.unit.${freq}`, { count: interval })}</span>
            </Group>
          </NumberField>

          <RadioGroup
            className="field"
            value={repeatFrom}
            onChange={(value) => setRepeatFrom(value === "completion" ? "completion" : "due")}
          >
            <Label>{t("repeatEditor.repeatFrom")}</Label>
            <Choice value="due" label={t("repeatEditor.fromDue")} />
            <Choice value="completion" label={t("repeatEditor.fromCompletion")} />
          </RadioGroup>

          {freq === "WEEKLY" && !fromCompletion && (
            <div className="field">
              <span className="field-label" id="repeat-days">
                {t("repeatEditor.onDays")}
              </span>
              <ToggleButtonGroup
                className="weekday-group"
                aria-labelledby="repeat-days"
                selectionMode="multiple"
                selectedKeys={byDay}
                onSelectionChange={(keys) => setByDay(weekdays.filter((d) => keys.has(d)))}
              >
                {weekdays.map((day) => (
                  <ToggleButton key={day} id={day} className="weekday">
                    {weekdayName(day, i18n.language)}
                  </ToggleButton>
                ))}
              </ToggleButtonGroup>
            </div>
          )}

          {freq === "MONTHLY" && !fromCompletion && (
            <RadioGroup
              className="field"
              value={monthMode}
              onChange={(value) => setMonthMode(value === "lastDay" ? "lastDay" : "dueDay")}
            >
              <Label>{t("repeatEditor.monthDay")}</Label>
              <Choice value="dueDay" label={t("repeatEditor.dueDay")} />
              <Choice value="lastDay" label={t("repeatEditor.lastDay")} />
            </RadioGroup>
          )}

          <RadioGroup className="field" value={ends} onChange={(value) => setEnds(value as Ends)}>
            <Label>{t("repeatEditor.ends")}</Label>
            <Choice value="never" label={t("repeatEditor.never")} />
            <Choice value="count" label={t("repeatEditor.afterCount")} />
            <Choice value="until" label={t("repeatEditor.onDate")} />
          </RadioGroup>
          {ends === "count" && (
            <NumberField
              className="field"
              value={count}
              onChange={setCount}
              minValue={1}
              maxValue={1000000}
              step={1}
            >
              <Label>{t("repeatEditor.times")}</Label>
              <Group className="number-group">
                <Input className="input number-input" />
              </Group>
            </NumberField>
          )}
          {ends === "until" && (
            <DateField className="field" value={until} onChange={setUntil}>
              <Label>{t("repeatEditor.lastDate")}</Label>
              <DateInput className="input date-input">
                {(segment) => <DateSegment segment={segment} className="date-segment" />}
              </DateInput>
            </DateField>
          )}
        </>
      )}

      <div className="dialog-actions">
        <Button className="button" onPress={onDone}>
          {t("common.cancel")}
        </Button>
        <Button
          className="button button-primary"
          onPress={save}
          isDisabled={ends === "until" && until === null && freq !== "none"}
        >
          {t("common.save")}
        </Button>
      </div>
    </div>
  );
}

function Choice({ value, label }: { value: string; label: string }) {
  return (
    <RadioField value={value} className="choice">
      <RadioButton className="choice-button">
        <span className="choice-dot" aria-hidden="true" />
        {label}
      </RadioButton>
    </RadioField>
  );
}

function defaultRule(): Rule {
  return { freq: "DAILY", interval: 1, byDay: [], byMonthDay: null, count: null, until: null };
}
