// The reminders of a task (SPEC sections 6 and 9): the list with remove
// buttons, presets that fit the task's due type, a date-and-time reminder,
// and Snooze for 10 min, 1 h or tomorrow at the default time (D-64).
import {
  fromAbsolute,
  parseTime,
  toCalendarDateTime,
  toZoned,
  type CalendarDate,
  type Time,
} from "@internationalized/date";
import { Bell, X } from "lucide-react";
import { useState } from "react";
import {
  Button,
  DateField,
  DateInput,
  DateSegment,
  Dialog,
  Heading,
  Label,
  Menu,
  MenuItem,
  MenuTrigger,
  Modal,
  ModalOverlay,
  Popover,
  TimeField,
} from "react-aria-components";
import { useTranslation } from "react-i18next";

import type { Reminder } from "../../api/types";
import { useProfile, useServices, useSnapshot } from "../../app/services";
import type { TaskRow } from "../../data/replica";
import { dateIn } from "../../lib/dates";
import { formatDay, formatTime } from "../../lib/format";

/** Most live reminders per task; snooze ones do not count (SPEC section 4). */
const maxReminders = 5;

type Translate = ReturnType<typeof useTranslation>["t"];

/** The offsets offered for each due type, in minutes before. */
function presets(task: TaskRow): number[] {
  if (!task.due_date) return [];
  return task.due_time ? [0, 5, 15, 30, 60, 1440] : [0, 1440, 2880];
}

/** "At the due time", "15 minutes before", "On the day at 9:00 AM"… */
export function describeOffset(
  offset: number,
  timed: boolean,
  allDayTime: string,
  t: Translate,
  locale: string,
): string {
  if (offset === 0) {
    if (timed) return t("reminders.atDue");
    const [h = 0, m = 0] = allDayTime.split(":").map(Number);
    const time = new Intl.DateTimeFormat(locale, {
      hour: "numeric",
      minute: "2-digit",
      timeZone: "UTC",
    }).format(new Date(Date.UTC(2000, 0, 1, h, m)));
    return t("reminders.onTheDay", { time });
  }
  if (offset % 1440 === 0) return t("reminders.daysBefore", { count: offset / 1440 });
  if (offset % 60 === 0) return t("reminders.hoursBefore", { count: offset / 60 });
  return t("reminders.minutesBefore", { count: offset });
}

export function RemindersField({ task }: { task: TaskRow }) {
  const { t, i18n } = useTranslation();
  const snapshot = useSnapshot();
  const profile = useProfile();
  const { actions } = useServices();
  const [picking, setPicking] = useState(false);
  const zone = profile.timezone;

  const reminders = [...snapshot.reminders.values()].filter((r) => r.task_id === task.id);
  const counted = reminders.filter((r) => r.kind !== "snooze").length;
  const timed = Boolean(task.due_time);

  const describe = (r: Reminder): string => {
    if (r.kind === "relative") {
      return describeOffset(
        r.offset_minutes ?? 0,
        timed,
        profile.all_day_reminder_time,
        t,
        i18n.language,
      );
    }
    const at = new Date(r.at ?? "");
    const when = t("reminders.at", {
      day: formatDay(dateIn(at, zone), zone, i18n.language),
      time: formatTime(at, zone, i18n.language),
    });
    return r.kind === "snooze" ? t("reminders.snoozedUntil", { when }) : when;
  };

  return (
    <div className="reminders-field">
      {reminders.length > 0 && (
        <ul className="reminder-list">
          {reminders.map((r) => (
            <li key={r.id} className={r.next_fire_at ? "reminder" : "reminder reminder-idle"}>
              <Bell size={14} strokeWidth={1.5} aria-hidden="true" />
              <span>{describe(r)}</span>
              <Button
                className="chip-remove"
                aria-label={t("reminders.remove", { reminder: describe(r) })}
                onPress={() => void actions.deleteReminder(r.id)}
              >
                <X size={12} strokeWidth={1.5} aria-hidden="true" />
              </Button>
            </li>
          ))}
        </ul>
      )}
      <div className="reminder-actions">
        <MenuTrigger>
          <Button className="link-button" isDisabled={counted >= maxReminders}>
            {t("reminders.add")}
          </Button>
          <Popover className="popover">
            <Menu
              className="menu"
              onAction={(key) => {
                if (key === "custom") setPicking(true);
                else void actions.addReminder(task.id, { offsetMinutes: Number(key) });
              }}
            >
              {presets(task).map((offset) => (
                <MenuItem key={offset} id={String(offset)} className="menu-item">
                  {describeOffset(offset, timed, profile.all_day_reminder_time, t, i18n.language)}
                </MenuItem>
              ))}
              <MenuItem id="custom" className="menu-item">
                {t("reminders.custom")}
              </MenuItem>
            </Menu>
          </Popover>
        </MenuTrigger>
        {task.status === "open" && <SnoozeMenu task={task} />}
      </div>
      {picking && (
        <AtDialog
          zone={zone}
          onClose={() => setPicking(false)}
          onSave={(at) => {
            void actions.addReminder(task.id, { at });
          }}
        />
      )}
    </div>
  );
}

/** A reminder at a date and time, in the profile zone (D-47). */
function AtDialog({
  zone,
  onClose,
  onSave,
}: {
  zone: string;
  onClose: () => void;
  onSave: (at: Date) => void;
}) {
  const { t } = useTranslation();
  const { clock } = useServices();
  // Proposed: the next whole hour after an hour from now.
  const start = fromAbsolute(clock.now().getTime() + 3600_000, zone);
  const [date, setDate] = useState<CalendarDate | null>(dateIn(start.toDate(), zone));
  const [time, setTime] = useState<Time | null>(
    parseTime(`${String(start.hour).padStart(2, "0")}:00`),
  );
  return (
    <ModalOverlay
      className="modal-overlay"
      isOpen
      onOpenChange={(open) => !open && onClose()}
      isDismissable
    >
      <Modal className="modal">
        <Dialog className="dialog">
          <Heading slot="title" className="dialog-title">
            {t("reminders.custom")}
          </Heading>
          <DateField className="field" value={date} onChange={setDate}>
            <Label>{t("due.date")}</Label>
            <DateInput className="input date-input">
              {(segment) => <DateSegment segment={segment} className="date-segment" />}
            </DateInput>
          </DateField>
          <TimeField className="field" value={time} onChange={setTime} hourCycle={24}>
            <Label>{t("due.time")}</Label>
            <DateInput className="input date-input">
              {(segment) => <DateSegment segment={segment} className="date-segment" />}
            </DateInput>
          </TimeField>
          <div className="dialog-actions">
            <Button className="button" onPress={onClose}>
              {t("common.cancel")}
            </Button>
            <Button
              className="button button-primary"
              isDisabled={date === null || time === null}
              onPress={() => {
                if (date === null || time === null) return;
                onSave(toZoned(toCalendarDateTime(date, time), zone).toDate());
                onClose();
              }}
            >
              {t("common.save")}
            </Button>
          </div>
        </Dialog>
      </Modal>
    </ModalOverlay>
  );
}

/** Snooze from the detail: 10 min, 1 h, or tomorrow at the default time (D-64). */
function SnoozeMenu({ task }: { task: TaskRow }) {
  const { t, i18n } = useTranslation();
  const { actions, clock, toasts } = useServices();
  const profile = useProfile();
  const zone = profile.timezone;

  const until = (key: string): Date => {
    const now = clock.now();
    if (key === "10m") return new Date(now.getTime() + 10 * 60_000);
    if (key === "1h") return new Date(now.getTime() + 60 * 60_000);
    const tomorrow = dateIn(now, zone).add({ days: 1 });
    return toZoned(
      toCalendarDateTime(tomorrow, parseTime(profile.all_day_reminder_time)),
      zone,
    ).toDate();
  };

  return (
    <MenuTrigger>
      <Button className="link-button">{t("reminders.snooze")}</Button>
      <Popover className="popover">
        <Menu
          className="menu"
          onAction={(key) => {
            const at = until(String(key));
            void actions.snooze(task.id, at).then((ok) => {
              if (!ok) return;
              const when = t("reminders.at", {
                day: formatDay(dateIn(at, zone), zone, i18n.language),
                time: formatTime(at, zone, i18n.language),
              });
              toasts.show({ message: t("reminders.snoozedUntil", { when }), tone: "info" });
            });
          }}
        >
          <MenuItem id="10m" className="menu-item">
            {t("reminders.in10")}
          </MenuItem>
          <MenuItem id="1h" className="menu-item">
            {t("reminders.in60")}
          </MenuItem>
          <MenuItem id="tomorrow" className="menu-item">
            {t("reminders.tomorrow", { time: profile.all_day_reminder_time })}
          </MenuItem>
        </Menu>
      </Popover>
    </MenuTrigger>
  );
}
