// The task detail panel (DESIGN.md section 2): header with the complete
// box, due date, priority and close; title; description; checklist; the
// List, Tags, Reminders (with Snooze) and Repeat fields; footer with the
// creation date, "Skip this occurrence" for recurring tasks, and delete.
import { ArrowLeft, Flag, SkipForward, Trash2, X } from "lucide-react";
import { useEffect, useState } from "react";
import {
  Button,
  Label,
  ListBox,
  ListBoxItem,
  Menu,
  MenuItem,
  MenuTrigger,
  Popover,
  Select,
  SelectValue,
  TextArea,
  TextField,
} from "react-aria-components";
import { useTranslation } from "react-i18next";
import Markdown from "react-markdown";

import type { TaskPatch } from "../../api/types";
import { useProfile, useServices, useSnapshot } from "../../app/services";
import type { TaskRow } from "../../data/replica";
import { sortedLists } from "../../data/views";
import { dateIn } from "../../lib/dates";
import { formatDay } from "../../lib/format";
import { priorityKey, priorityKeys } from "../priority";
import { TaskCheckbox } from "../TaskCheckbox";
import { useRecord } from "../useRecord";
import { Checklist } from "./Checklist";
import { DueEditor } from "./DueEditor";
import { RemindersField } from "./RemindersField";
import { RepeatEditor } from "./RepeatEditor";
import { TagsField } from "./TagsField";

export function TaskDetail({ taskId, onClose }: { taskId: string; onClose: () => void }) {
  const snapshot = useSnapshot();
  const task = snapshot.tasks.get(taskId);

  // A task that is gone (deleted, completed elsewhere and reopened as
  // another…) closes the panel.
  const missing = task === undefined || task.status !== "open";
  useEffect(() => {
    if (missing) onClose();
  }, [missing, onClose]);
  if (missing) return null;

  // `key`: a different task starts with fresh field state.
  return <DetailPanel key={task.id} task={task} onClose={onClose} />;
}

function DetailPanel({ task, onClose }: { task: TaskRow; onClose: () => void }) {
  const { t, i18n } = useTranslation();
  const { actions, toasts } = useServices();
  const profile = useProfile();
  const patch = (fields: TaskPatch) => {
    void actions.updateTask(task.id, fields);
  };

  const record = useRecord();

  const remove = async () => {
    onClose();
    const ok = await actions.deleteTask(task.id);
    if (!ok) return;
    toasts.show({
      message: t("task.deleted"),
      tone: "info",
      action: {
        label: t("common.undo"),
        run: () => {
          void actions.restoreTask(task.id);
        },
      },
    });
  };

  const priority = priorityKey(task.priority);
  const created = dateIn(new Date(task.created_at), profile.timezone);

  return (
    <aside
      className="detail"
      aria-label={t("detail.label")}
      onKeyDown={(event) => {
        // Escape closes the panel (SPEC section 9). Popovers and menus
        // inside handle their own Escape and stop it there.
        if (event.key === "Escape" && !event.defaultPrevented) onClose();
      }}
    >
      <div className="detail-header">
        <Button className="icon-button detail-back" aria-label={t("common.back")} onPress={onClose}>
          <ArrowLeft size={16} strokeWidth={1.5} aria-hidden="true" />
        </Button>
        <TaskCheckbox
          className={`priority-${priority}`}
          isSelected={false}
          onChange={() => {
            void record(task.id, "complete");
          }}
          label={t("task.complete")}
        />
        <DueEditor task={task} onPatch={patch} />
        <MenuTrigger>
          <Button
            className={`header-button priority-text-${priority}`}
            aria-label={t("priority.edit", { priority: t(`priorityLabel.${priority}`) })}
          >
            <Flag size={16} strokeWidth={1.5} aria-hidden="true" />
            <span>{t(`priorityLabel.${priority}`)}</span>
          </Button>
          <Popover className="popover">
            <Menu
              className="menu"
              selectionMode="single"
              selectedKeys={[priority]}
              onAction={(key) => {
                patch({ priority: priorityKeys.indexOf(key as (typeof priorityKeys)[number]) });
              }}
            >
              {priorityKeys.map((key) => (
                <MenuItem key={key} id={key} className="menu-item">
                  {t(`priorityLabel.${key}`)}
                </MenuItem>
              ))}
            </Menu>
          </Popover>
        </MenuTrigger>
        <Button
          className="icon-button detail-close"
          aria-label={t("common.close")}
          onPress={onClose}
        >
          <X size={16} strokeWidth={1.5} aria-hidden="true" />
        </Button>
      </div>

      <div className="detail-body">
        <TitleField task={task} onPatch={patch} />
        <DescriptionField task={task} onPatch={patch} />
        <Checklist taskId={task.id} />
        <dl className="detail-fields">
          <dt>{t("detail.list")}</dt>
          <dd>
            <ListField task={task} />
          </dd>
          <dt>{t("detail.tags")}</dt>
          <dd>
            <TagsField task={task} />
          </dd>
          <dt>{t("detail.reminders")}</dt>
          <dd>
            <RemindersField task={task} />
          </dd>
          <dt>{t("detail.repeat")}</dt>
          <dd>
            <RepeatEditor task={task} onPatch={patch} />
          </dd>
        </dl>
      </div>

      <div className="detail-footer">
        <span>
          {t("detail.created", { date: formatDay(created, profile.timezone, i18n.language) })}
        </span>
        {task.rrule && (
          <Button
            className="header-button detail-skip"
            onPress={() => {
              void record(task.id, "skip");
            }}
          >
            <SkipForward size={16} strokeWidth={1.5} aria-hidden="true" />
            <span>{t("task.skip")}</span>
          </Button>
        )}
        <Button
          className="icon-button"
          aria-label={t("task.delete")}
          onPress={() => {
            void remove();
          }}
        >
          <Trash2 size={16} strokeWidth={1.5} aria-hidden="true" />
        </Button>
      </div>
    </aside>
  );
}

/** The title, saved when the field loses focus or on Enter. */
function TitleField({ task, onPatch }: { task: TaskRow; onPatch: (patch: TaskPatch) => void }) {
  const { t } = useTranslation();
  const [title, setTitle] = useState(task.title);
  const commit = () => {
    const trimmed = title.trim();
    if (trimmed === "" || trimmed === task.title) {
      setTitle(task.title);
      return;
    }
    onPatch({ title: trimmed });
  };
  return (
    <TextField
      className="detail-title"
      aria-label={t("detail.title")}
      value={title}
      onChange={setTitle}
      onBlur={commit}
      onKeyDown={(event) => {
        if (event.key === "Enter") {
          event.preventDefault();
          commit();
        }
      }}
      maxLength={500}
    >
      <TextArea className="detail-title-input" rows={1} />
    </TextField>
  );
}

/**
 * The description: Markdown shown rendered (D-55), edited as plain text.
 * react-markdown builds React elements and ignores raw HTML, so nothing in
 * a description can inject markup.
 */
function DescriptionField({
  task,
  onPatch,
}: {
  task: TaskRow;
  onPatch: (patch: TaskPatch) => void;
}) {
  const { t } = useTranslation();
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(task.description);

  if (editing) {
    return (
      <TextField
        className="field"
        aria-label={t("detail.description")}
        value={text}
        onChange={setText}
        onBlur={() => {
          setEditing(false);
          if (text !== task.description) onPatch({ description: text });
        }}
        maxLength={20000}
        autoFocus
      >
        <TextArea className="input description-input" />
      </TextField>
    );
  }
  return (
    <div className="description">
      {task.description !== "" && (
        <div className="markdown">
          <Markdown
            components={{
              a: ({ href, children }) => (
                <a href={href} target="_blank" rel="noopener noreferrer">
                  {children}
                </a>
              ),
            }}
          >
            {task.description}
          </Markdown>
        </div>
      )}
      <Button className="link-button" onPress={() => setEditing(true)}>
        {task.description === "" ? t("detail.addDescription") : t("detail.editDescription")}
      </Button>
    </div>
  );
}

function ListField({ task }: { task: TaskRow }) {
  const { t } = useTranslation();
  const snapshot = useSnapshot();
  const { actions } = useServices();
  const lists = sortedLists(snapshot);
  return (
    <Select
      className="list-select"
      aria-label={t("detail.list")}
      value={task.list_id}
      onChange={(key) => {
        if (key !== null && key !== task.list_id) void actions.moveTask(task.id, String(key));
      }}
    >
      <Button className="header-button">
        <SelectValue />
      </Button>
      <Popover className="popover">
        <ListBox className="listbox" items={lists}>
          {(list) => (
            <ListBoxItem
              id={list.id}
              className="menu-item"
              textValue={list.is_inbox ? t("nav.inbox") : list.name}
            >
              {list.is_inbox ? t("nav.inbox") : list.name}
            </ListBoxItem>
          )}
        </ListBox>
      </Popover>
      <Label className="visually-hidden">{t("detail.list")}</Label>
    </Select>
  );
}
