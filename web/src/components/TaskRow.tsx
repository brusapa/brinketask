// One open task in a list (DESIGN.md section 5, "Task row content").
import { GridListItem } from "react-aria-components";

import { RepeatIcon } from "./RepeatIcon";
import { useTranslation } from "react-i18next";

import type { CalendarDate } from "@internationalized/date";

import { useProfile, useSnapshot } from "../app/services";
import type { TaskRow as Row } from "../data/replica";
import { isOverdue } from "../lib/dates";
import { formatDue } from "../lib/format";
import { DragHandle } from "./DragHandle";
import { priorityKey } from "./priority";
import { useRecord } from "./useRecord";
import { TaskCheckbox } from "./TaskCheckbox";

export function TaskRow({
  task,
  today,
  now,
  showList,
  progress,
}: {
  task: Row;
  today: CalendarDate;
  now: Date;
  /** In views that mix lists (Today, Next 7 days, tag, search). */
  showList: boolean;
  /** Checklist items done and in total, when the task has a checklist. */
  progress: { done: number; total: number } | undefined;
}) {
  const { t, i18n } = useTranslation();
  const snapshot = useSnapshot();
  const profile = useProfile();

  const overdue = isOverdue(task, now, profile.timezone);
  const due = formatDue(task, today, profile.timezone, i18n.language);
  const list = snapshot.lists.get(task.list_id);
  const listName = list?.is_inbox ? t("nav.inbox") : list?.name;
  const tags = task.tag_ids.flatMap((id) => {
    const tag = snapshot.tags.get(id);
    return tag ? [tag] : [];
  });
  const priority = priorityKey(task.priority);

  const record = useRecord();

  return (
    <GridListItem id={task.id} textValue={task.title} className="task-row">
      <DragHandle />
      <TaskCheckbox
        className={`priority-${priority}`}
        isSelected={false}
        onChange={() => {
          void record(task.id, "complete");
        }}
        label={
          priority === "none"
            ? t("task.complete")
            : t("task.completeWithPriority", { priority: t(`priority.${priority}`) })
        }
      />
      <span className="task-title">{task.title}</span>
      <span className="task-meta">
        {progress && (
          <span
            className="task-progress"
            aria-label={t("task.checklistProgress", { done: progress.done, total: progress.total })}
          >
            {progress.done}/{progress.total}
          </span>
        )}
        {task.rrule && <RepeatIcon />}
        {tags.map((tag) => (
          <span key={tag.id} className="chip">
            {tag.name}
          </span>
        ))}
        {showList && listName !== undefined && <span className="task-list-name">{listName}</span>}
      </span>
      <span className={overdue ? "task-due task-due-overdue" : "task-due"}>
        {overdue && <span className="visually-hidden">{t("task.overdue")} </span>}
        {due}
      </span>
    </GridListItem>
  );
}
