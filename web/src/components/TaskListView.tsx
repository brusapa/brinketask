// A screen of tasks: header, quick add, the view's sections and the
// Completed section (DESIGN.md sections 2 and 5). Which tasks and sections
// come from data/views.ts; this component only lays them out.
import type { CalendarDate } from "@internationalized/date";
import { Plus } from "lucide-react";
import { useMemo, useState } from "react";
import {
  DropIndicator,
  Form,
  GridList,
  Input,
  TextField,
  useDragAndDrop,
  type Selection,
} from "react-aria-components";
import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router";

import { useNow, useProfile, useServices, useSnapshot } from "../app/services";
import type { Snapshot, TaskRow as Row } from "../data/replica";
import {
  completedScope,
  sections,
  tasksWithPendingReminders,
  type Section,
  type View,
} from "../data/views";
import { dateIn } from "../lib/dates";
import { formatDay } from "../lib/format";
import { positionForMove } from "../lib/positions";
import { CompletedSection } from "./CompletedSection";
import { taskDragType } from "./drag";
import { TaskRow } from "./TaskRow";
import { ViewHeader } from "./ViewHeader";

export function TaskListView({
  view,
  title,
  info,
}: {
  view: View;
  title: string;
  info?: string | undefined;
}) {
  const { t, i18n } = useTranslation();
  const snapshot = useSnapshot();
  const profile = useProfile();
  const now = useNow();
  const zone = profile.timezone;
  const today = dateIn(now, zone);
  const ctx = { snapshot, zone, now };
  const result = sections(view, ctx);
  const scope = completedScope(view, ctx);
  const progress = useMemo(() => checklistProgress(snapshot), [snapshot]);
  const withReminders = useMemo(() => tasksWithPendingReminders(snapshot), [snapshot]);
  const showList = view.kind !== "list";
  const isEmpty = result.every((s) => s.tasks.length === 0);

  return (
    <section className="view" aria-label={title}>
      <ViewHeader title={title} info={info} />
      {view.kind !== "search" && <QuickAdd view={view} />}
      {isEmpty && (
        <p className="empty">{view.kind === "search" ? t("views.noMatches") : t("views.empty")}</p>
      )}
      {result.map((section) => {
        // Empty overdue sections are left out; empty days are kept so the
        // week reads as a week (D-48).
        if (section.key === "overdue" && section.tasks.length === 0) return null;
        const heading = sectionHeading(section, today, zone, i18n.language, t);
        return (
          <TaskSection
            key={section.key === "day" ? section.day.toString() : section.key}
            heading={heading}
            danger={section.key === "overdue"}
            tasks={section.tasks}
            reorderable={view.kind === "list"}
            today={today}
            now={now}
            showList={showList}
            progress={progress}
            withReminders={withReminders}
          />
        );
      })}
      {scope !== null && <CompletedSection scope={scope} now={now} />}
    </section>
  );
}

type Translate = ReturnType<typeof useTranslation>["t"];

function sectionHeading(
  section: Section,
  today: CalendarDate,
  zone: string,
  locale: string,
  t: Translate,
): string | undefined {
  switch (section.key) {
    case "all":
      return undefined;
    case "overdue":
      return t("sections.overdue");
    case "day":
      if (section.day.compare(today) === 0) return t("sections.today");
      if (section.day.compare(today.add({ days: 1 })) === 0) return t("sections.tomorrow");
      return formatDay(section.day, zone, locale);
  }
}

/** Done and total checklist items per task, computed once per snapshot. */
function checklistProgress(snapshot: Snapshot): Map<string, { done: number; total: number }> {
  const result = new Map<string, { done: number; total: number }>();
  for (const item of snapshot.items.values()) {
    const entry = result.get(item.task_id) ?? { done: 0, total: 0 };
    entry.total++;
    if (item.is_done) entry.done++;
    result.set(item.task_id, entry);
  }
  return result;
}

function TaskSection({
  heading,
  danger,
  tasks,
  reorderable,
  today,
  now,
  showList,
  progress,
  withReminders,
}: {
  heading: string | undefined;
  danger: boolean;
  tasks: Row[];
  reorderable: boolean;
  today: CalendarDate;
  now: Date;
  showList: boolean;
  progress: Map<string, { done: number; total: number }>;
  withReminders: ReadonlySet<string>;
}) {
  const { t } = useTranslation();
  const { actions } = useServices();
  const [params, setParams] = useSearchParams();
  const selected = params.get("task");

  // Rows can always be dragged onto a list in the sidebar; within a list
  // they can also be reordered (D-12). React Aria makes both work with the
  // keyboard too.
  const { dragAndDropHooks } = useDragAndDrop({
    getItems: (keys) =>
      [...keys].map((key) => ({
        [taskDragType]: String(key),
        "text/plain": tasks.find((task) => task.id === key)?.title ?? "",
      })),
    onReorder: reorderable
      ? (event) => {
          const moved = String([...event.keys][0]);
          const position = positionForMove(
            tasks,
            moved,
            String(event.target.key),
            event.target.dropPosition,
          );
          if (position !== null) void actions.updateTask(moved, { position });
        }
      : undefined,
    renderDropIndicator: (target) => <DropIndicator target={target} className="drop-indicator" />,
  });

  // The selected row is the task open in the detail panel; selecting a row
  // opens it, and the URL (?task=) remembers it.
  const onSelectionChange = (selection: Selection) => {
    const next = new URLSearchParams(params);
    const key = selection === "all" ? undefined : [...selection][0];
    if (key === undefined) {
      next.delete("task");
    } else {
      next.set("task", String(key));
    }
    setParams(next);
  };

  return (
    <div className="task-section">
      {heading !== undefined && (
        <h2 className={danger ? "section-heading section-heading-danger" : "section-heading"}>
          <span>{heading}</span>
          <span className="section-count">{tasks.length}</span>
        </h2>
      )}
      {/* An empty grid would still show an empty row to screen readers. */}
      {tasks.length > 0 && (
        <GridList
          className="task-list"
          aria-label={heading ?? t("views.tasks")}
          items={tasks}
          selectionMode="single"
          selectionBehavior="replace"
          selectedKeys={selected !== null ? [selected] : []}
          onSelectionChange={onSelectionChange}
          dragAndDropHooks={dragAndDropHooks}
        >
          {(task) => (
            <TaskRow
              task={task}
              today={today}
              now={now}
              showList={showList}
              progress={progress.get(task.id)}
              hasReminder={withReminders.has(task.id)}
            />
          )}
        </GridList>
      )}
    </div>
  );
}

/**
 * Quick add (D-53): a title only, with defaults that keep the task in the
 * view where it was typed.
 */
function QuickAdd({ view }: { view: View }) {
  const { t } = useTranslation();
  const { actions } = useServices();
  const profile = useProfile();
  const [title, setTitle] = useState("");

  const submit = () => {
    const trimmed = title.trim();
    if (trimmed === "") return;
    setTitle("");
    void actions.createTask({
      title: trimmed,
      listId: view.kind === "list" ? view.listId : profile.inbox_list_id,
      dueDate: view.kind === "today" || view.kind === "next7" ? actions.today() : undefined,
      tagIds: view.kind === "tag" ? [view.tagId] : undefined,
    });
  };

  return (
    <Form
      className="quick-add"
      onSubmit={(event) => {
        event.preventDefault();
        submit();
      }}
    >
      <TextField
        className="quick-add-field"
        aria-label={t("quickAdd.label")}
        value={title}
        onChange={setTitle}
        maxLength={500}
      >
        <Plus size={16} strokeWidth={1.5} aria-hidden="true" className="quick-add-icon" />
        <Input className="quick-add-input" placeholder={t("quickAdd.placeholder")} />
      </TextField>
    </Form>
  );
}
