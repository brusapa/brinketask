// The checklist of a task (D-08): items with a checkbox, editable titles,
// delete, reorder by drag (or keyboard), and a field to add one.
import { Plus, X } from "lucide-react";
import { useState } from "react";
import {
  Button,
  DropIndicator,
  Form,
  GridList,
  GridListItem,
  Input,
  TextField,
  useDragAndDrop,
} from "react-aria-components";
import { useTranslation } from "react-i18next";

import type { ChecklistItem } from "../../api/types";
import { useServices, useSnapshot } from "../../app/services";
import { comparePositions, positionForMove } from "../../lib/positions";
import { TaskCheckbox } from "../TaskCheckbox";

export function Checklist({ taskId }: { taskId: string }) {
  const { t } = useTranslation();
  const snapshot = useSnapshot();
  const { actions } = useServices();
  const items = [...snapshot.items.values()]
    .filter((i) => i.task_id === taskId)
    .sort(comparePositions);

  const { dragAndDropHooks } = useDragAndDrop({
    getItems: (keys) =>
      [...keys].map((key) => ({ "text/plain": items.find((i) => i.id === key)?.title ?? "" })),
    onReorder: (event) => {
      const moved = String([...event.keys][0]);
      const position = positionForMove(
        items,
        moved,
        String(event.target.key),
        event.target.dropPosition,
      );
      if (position !== null) void actions.updateItem(moved, { position });
    },
    renderDropIndicator: (target) => <DropIndicator target={target} className="drop-indicator" />,
  });

  return (
    <section className="checklist" aria-label={t("checklist.label")}>
      {items.length > 0 && (
        <GridList
          className="checklist-list"
          aria-label={t("checklist.label")}
          items={items}
          dragAndDropHooks={dragAndDropHooks}
        >
          {(item) => <ChecklistRow item={item} />}
        </GridList>
      )}
      <AddItem taskId={taskId} />
    </section>
  );
}

function ChecklistRow({ item }: { item: ChecklistItem }) {
  const { t } = useTranslation();
  const { actions } = useServices();
  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState(item.title);

  const commit = () => {
    setEditing(false);
    const trimmed = title.trim();
    if (trimmed === "" || trimmed === item.title) {
      setTitle(item.title);
      return;
    }
    void actions.updateItem(item.id, { title: trimmed });
  };

  return (
    <GridListItem id={item.id} textValue={item.title} className="checklist-row">
      <TaskCheckbox
        isSelected={item.is_done}
        onChange={(done) => {
          void actions.updateItem(item.id, { is_done: done });
        }}
        label={
          item.is_done
            ? t("checklist.uncheck", { title: item.title })
            : t("checklist.check", { title: item.title })
        }
      />
      {editing ? (
        <TextField
          className="checklist-edit"
          aria-label={t("checklist.title")}
          value={title}
          onChange={setTitle}
          onBlur={commit}
          onKeyDown={(event) => {
            if (event.key === "Enter") commit();
            if (event.key === "Escape") {
              setTitle(item.title);
              setEditing(false);
            }
          }}
          maxLength={500}
          autoFocus
        >
          <Input className="input" />
        </TextField>
      ) : (
        <Button
          className={item.is_done ? "checklist-title checklist-title-done" : "checklist-title"}
          onPress={() => setEditing(true)}
          aria-label={t("checklist.edit", { title: item.title })}
        >
          {item.title}
        </Button>
      )}
      <Button
        className="icon-button"
        aria-label={t("checklist.delete", { title: item.title })}
        onPress={() => {
          void actions.deleteItem(item.id);
        }}
      >
        <X size={16} strokeWidth={1.5} aria-hidden="true" />
      </Button>
    </GridListItem>
  );
}

function AddItem({ taskId }: { taskId: string }) {
  const { t } = useTranslation();
  const { actions } = useServices();
  const [title, setTitle] = useState("");
  return (
    <Form
      className="checklist-add"
      onSubmit={(event) => {
        event.preventDefault();
        const trimmed = title.trim();
        if (trimmed === "") return;
        setTitle("");
        void actions.addItem(taskId, trimmed);
      }}
    >
      <TextField
        className="quick-add-field"
        aria-label={t("checklist.add")}
        value={title}
        onChange={setTitle}
        maxLength={500}
      >
        <Plus size={16} strokeWidth={1.5} aria-hidden="true" className="quick-add-icon" />
        <Input className="checklist-add-input" placeholder={t("checklist.add")} />
      </TextField>
    </Form>
  );
}
