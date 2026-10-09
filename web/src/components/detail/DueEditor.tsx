// Editing a task's due date, time and zone (SPEC section 4, due types).
// Every change is sent as one merge patch that keeps the fields consistent
// (D-26): clearing the date clears the time and zone with it; clearing the
// time clears the zone.
import { parseDate, parseTime, type CalendarDate, type Time } from "@internationalized/date";
import { CalendarDays, ChevronLeft, ChevronRight } from "lucide-react";
import { useState } from "react";
import {
  Button,
  Calendar,
  CalendarCell,
  CalendarGrid,
  Dialog,
  DialogTrigger,
  Heading,
  Label,
  Popover,
  SwitchButton,
  SwitchField,
  TimeField,
  DateInput,
  DateSegment,
} from "react-aria-components";
import { useTranslation } from "react-i18next";

import type { TaskPatch } from "../../api/types";
import { useNow, useProfile } from "../../app/services";
import type { TaskRow } from "../../data/replica";
import { dateIn, isOverdue } from "../../lib/dates";
import { formatDue } from "../../lib/format";
import { ZonePicker } from "../ZonePicker";

export function DueEditor({
  task,
  onPatch,
}: {
  task: TaskRow;
  onPatch: (patch: TaskPatch) => void;
}) {
  const { t, i18n } = useTranslation();
  const profile = useProfile();
  const now = useNow();
  const zone = profile.timezone;
  const today = dateIn(now, zone);
  const label = task.due_date ? formatDue(task, today, zone, i18n.language) : t("due.none");
  const overdue = isOverdue(task, now, zone);

  return (
    <DialogTrigger>
      <Button
        className={overdue ? "header-button task-due-overdue" : "header-button"}
        aria-label={t("due.edit", { due: label })}
      >
        <CalendarDays size={16} strokeWidth={1.5} aria-hidden="true" />
        <span>{label}</span>
      </Button>
      <Popover className="popover due-popover" placement="bottom start">
        <Dialog className="dialog" aria-label={t("due.title")}>
          {({ close }) => <DueForm task={task} today={today} onPatch={onPatch} onDone={close} />}
        </Dialog>
      </Popover>
    </DialogTrigger>
  );
}

function DueForm({
  task,
  today,
  onPatch,
  onDone,
}: {
  task: TaskRow;
  today: CalendarDate;
  onPatch: (patch: TaskPatch) => void;
  onDone: () => void;
}) {
  const { t } = useTranslation();
  const profile = useProfile();
  const date = task.due_date ? parseDate(task.due_date) : null;
  const time = task.due_time ? parseTime(task.due_time) : null;
  const [fixedZone, setFixedZone] = useState(task.due_tz ?? null);
  // The time is sent when the field loses focus, not on every keystroke.
  const [timeValue, setTimeValue] = useState<Time | null>(time);

  const commitTime = () => {
    const text =
      timeValue === null
        ? null
        : `${String(timeValue.hour).padStart(2, "0")}:${String(timeValue.minute).padStart(2, "0")}`;
    if (text === (task.due_time ?? null)) return;
    // A time needs a date (it has one here); no time means no zone (D-26).
    onPatch(text === null ? { due_time: null, due_tz: null } : { due_time: text });
  };

  const setDate = (day: CalendarDate) => {
    onPatch({ due_date: day.toString() });
  };

  return (
    <div className="due-form">
      <div className="due-shortcuts">
        <Button className="button" onPress={() => setDate(today)}>
          {t("due.today")}
        </Button>
        <Button className="button" onPress={() => setDate(today.add({ days: 1 }))}>
          {t("due.tomorrow")}
        </Button>
      </div>
      <Calendar className="calendar" value={date} onChange={setDate} aria-label={t("due.date")}>
        <header className="calendar-header">
          <Button slot="previous" className="icon-button" aria-label={t("due.previousMonth")}>
            <ChevronLeft size={16} strokeWidth={1.5} aria-hidden="true" />
          </Button>
          <Heading className="calendar-heading" />
          <Button slot="next" className="icon-button" aria-label={t("due.nextMonth")}>
            <ChevronRight size={16} strokeWidth={1.5} aria-hidden="true" />
          </Button>
        </header>
        <CalendarGrid className="calendar-grid">
          {(day) => <CalendarCell date={day} className="calendar-cell" />}
        </CalendarGrid>
      </Calendar>

      {date !== null && (
        <>
          <TimeField
            className="field"
            value={timeValue}
            hourCycle={24}
            onChange={setTimeValue}
            onBlur={commitTime}
          >
            <Label>{t("due.time")}</Label>
            <DateInput className="input date-input">
              {(segment) => <DateSegment segment={segment} className="date-segment" />}
            </DateInput>
          </TimeField>
          {time !== null && (
            <>
              {/* Floating: the time follows the user's zone. Fixed: an
                  absolute instant in the chosen zone. */}
              <SwitchField
                className="switch-field"
                isSelected={fixedZone !== null}
                onChange={(fixed) => {
                  const next = fixed ? profile.timezone : null;
                  setFixedZone(next);
                  onPatch({ due_tz: next });
                }}
              >
                <SwitchButton className="switch">
                  <span className="switch-track" aria-hidden="true" />
                  {t("due.fixedZone")}
                </SwitchButton>
              </SwitchField>
              {fixedZone !== null && (
                <ZonePicker
                  label={t("due.zone")}
                  value={fixedZone}
                  onChange={(zone) => {
                    setFixedZone(zone);
                    onPatch({ due_tz: zone });
                  }}
                />
              )}
            </>
          )}
        </>
      )}

      <div className="dialog-actions">
        {date !== null && (
          <Button
            className="button"
            onPress={() => {
              onPatch({ due_date: null, due_time: null, due_tz: null });
              onDone();
            }}
          >
            {t("due.clear")}
          </Button>
        )}
        <Button className="button button-primary" onPress={onDone}>
          {t("common.done")}
        </Button>
      </div>
    </div>
  );
}
