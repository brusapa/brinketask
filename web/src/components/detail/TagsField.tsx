// The tags of a task: chips that can be removed, and a combo box that adds
// an existing tag or creates a new one with the typed name.
import { useState } from "react";
import {
  Button,
  ComboBox,
  Input,
  ListBox,
  ListBoxItem,
  Popover,
  Tag,
  TagGroup,
  TagList,
  Label,
} from "react-aria-components";
import { useTranslation } from "react-i18next";

import { useServices, useSnapshot } from "../../app/services";
import type { TaskRow } from "../../data/replica";
import { sortedTags } from "../../data/views";
import { X } from "lucide-react";

/** The id of the "Create tag" option, which no real tag can have. */
const createOption = "create";

export function TagsField({ task }: { task: TaskRow }) {
  const { t } = useTranslation();
  const snapshot = useSnapshot();
  const { actions } = useServices();
  const [input, setInput] = useState("");

  const current = task.tag_ids.flatMap((id) => {
    const tag = snapshot.tags.get(id);
    return tag ? [tag] : [];
  });
  const typed = input.trim();
  const available = sortedTags(snapshot).filter((tag) => !task.tag_ids.includes(tag.id));
  const filtered = available.filter((tag) => tag.name.toLowerCase().includes(typed.toLowerCase()));
  const exists = sortedTags(snapshot).some((tag) => tag.name.toLowerCase() === typed.toLowerCase());
  const options = [
    ...filtered.map((tag) => ({ id: tag.id, name: tag.name })),
    ...(typed !== "" && !exists
      ? [{ id: createOption, name: t("tags.create", { name: typed }) }]
      : []),
  ];

  const add = async (key: string) => {
    setInput("");
    const tagId = key === createOption ? await actions.createTag(typed) : key;
    if (tagId === undefined) return;
    // Read the task again: it may have changed while the tag was created.
    const latest = actions.currentTask(task.id);
    if (latest === undefined || latest.tag_ids.includes(tagId)) return;
    void actions.updateTask(task.id, { tag_ids: [...latest.tag_ids, tagId] });
  };

  return (
    <div className="tags-field">
      {current.length > 0 && (
        <TagGroup
          className="tag-group"
          aria-label={t("detail.tags")}
          onRemove={(keys) => {
            void actions.updateTask(task.id, {
              tag_ids: task.tag_ids.filter((id) => !keys.has(id)),
            });
          }}
        >
          <TagList className="tag-list" items={current}>
            {(tag) => (
              <Tag id={tag.id} className="chip chip-removable" textValue={tag.name}>
                {tag.name}
                <Button
                  slot="remove"
                  className="chip-remove"
                  aria-label={t("tags.remove", { name: tag.name })}
                >
                  <X size={12} strokeWidth={1.5} aria-hidden="true" />
                </Button>
              </Tag>
            )}
          </TagList>
        </TagGroup>
      )}
      <ComboBox
        className="tag-combo"
        items={options}
        inputValue={input}
        onInputChange={setInput}
        value={null}
        onChange={(key) => {
          if (key !== null) void add(String(key));
        }}
        allowsEmptyCollection
        menuTrigger="focus"
      >
        <Label className="visually-hidden">{t("tags.add")}</Label>
        <Input className="input tag-input" placeholder={t("tags.add")} maxLength={50} />
        <Popover className="popover combo-popover">
          <ListBox
            className="listbox"
            renderEmptyState={() => <p className="empty">{t("tags.noneLeft")}</p>}
          >
            {(option: { id: string; name: string }) => (
              <ListBoxItem id={option.id} className="menu-item">
                {option.name}
              </ListBoxItem>
            )}
          </ListBox>
        </Popover>
      </ComboBox>
    </div>
  );
}
