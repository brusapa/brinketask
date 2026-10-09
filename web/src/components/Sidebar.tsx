// The sidebar (DESIGN.md section 2): views with their open-task counts, the
// lists with their colour dot and count, the tags, and Trash and Settings
// at the bottom. Lists are drop targets: dropping a task there moves it.
import {
  CalendarDays,
  CalendarRange,
  Ellipsis,
  Hash,
  Inbox,
  Plus,
  Settings,
  Trash2,
  type LucideIcon,
} from "lucide-react";
import { useState, type ReactNode } from "react";
import {
  Button,
  DropZone,
  Menu,
  MenuItem,
  MenuTrigger,
  Popover,
  type DropZoneProps,
} from "react-aria-components";
import { useTranslation } from "react-i18next";
import { NavLink, useLocation, useNavigate } from "react-router";

import type { List, Tag } from "../api/types";
import { useNow, useProfile, useServices, useSnapshot } from "../app/services";
import { counts, sortedLists, sortedTags } from "../data/views";
import { ConfirmDialog, ListDialog, NameDialog } from "./dialogs";
import { taskDragType } from "./drag";

// The event React Aria passes to a drop target, taken from its props.
type DropEvent = Parameters<NonNullable<DropZoneProps["onDrop"]>>[0];

export function Sidebar({ onNavigate }: { onNavigate: () => void }) {
  const { t } = useTranslation();
  const snapshot = useSnapshot();
  const profile = useProfile();
  const now = useNow();
  const { actions } = useServices();
  const [creating, setCreating] = useState(false);

  const c = counts({ snapshot, zone: profile.timezone, now });
  const lists = sortedLists(snapshot).filter((l) => !l.is_inbox);
  const tags = sortedTags(snapshot);

  return (
    <nav className="sidebar" aria-label={t("nav.main")}>
      <div className="sidebar-brand">{t("app.name")}</div>
      <ul className="nav-list">
        <NavItem
          to="/"
          icon={Inbox}
          label={t("nav.inbox")}
          count={c.lists.get(profile.inbox_list_id) ?? 0}
          onNavigate={onNavigate}
          dropListId={profile.inbox_list_id}
        />
        <NavItem
          to="/today"
          icon={CalendarDays}
          label={t("nav.today")}
          count={c.today}
          onNavigate={onNavigate}
        />
        <NavItem
          to="/next7"
          icon={CalendarRange}
          label={t("nav.next7")}
          count={c.next7}
          onNavigate={onNavigate}
        />
      </ul>

      <div className="sidebar-heading">
        <h2>{t("nav.lists")}</h2>
        <Button
          className="icon-button"
          aria-label={t("nav.newList")}
          onPress={() => setCreating(true)}
        >
          <Plus size={16} strokeWidth={1.5} aria-hidden="true" />
        </Button>
      </div>
      <ul className="nav-list">
        {lists.map((list) => (
          <ListItem
            key={list.id}
            list={list}
            count={c.lists.get(list.id) ?? 0}
            onNavigate={onNavigate}
          />
        ))}
      </ul>

      {tags.length > 0 && (
        <>
          <div className="sidebar-heading">
            <h2>{t("nav.tags")}</h2>
          </div>
          <ul className="nav-list">
            {tags.map((tag) => (
              <TagItem key={tag.id} tag={tag} onNavigate={onNavigate} />
            ))}
          </ul>
        </>
      )}

      <ul className="nav-list sidebar-bottom">
        <NavItem to="/trash" icon={Trash2} label={t("nav.trash")} onNavigate={onNavigate} />
        <NavItem to="/settings" icon={Settings} label={t("nav.settings")} onNavigate={onNavigate} />
      </ul>

      {creating && (
        <ListDialog
          isOpen
          onClose={() => setCreating(false)}
          title={t("lists.new")}
          initialName=""
          initialColor={null}
          onSave={(name, color) => {
            void actions.createList(name, color);
          }}
        />
      )}
    </nav>
  );
}

function NavItem({
  to,
  icon: Icon,
  label,
  count,
  onNavigate,
  marker,
  menu,
  dropListId,
}: {
  to: string;
  icon?: LucideIcon;
  label: string;
  count?: number;
  onNavigate: () => void;
  marker?: ReactNode;
  menu?: ReactNode;
  /** Makes the item a drop target that moves tasks into this list. */
  dropListId?: string;
}) {
  const { t } = useTranslation();
  const content = (
    <>
      {/* NavLink marks the current page with aria-current="page". "end"
          keeps "/" from matching every path. */}
      <NavLink to={to} end className="nav-link" onClick={onNavigate}>
        {Icon && <Icon size={16} strokeWidth={1.5} aria-hidden="true" />}
        {marker}
        <span className="nav-label">{label}</span>
        {count !== undefined && count > 0 && (
          <span className="nav-count" aria-label={t("nav.count", { count })}>
            {count}
          </span>
        )}
      </NavLink>
      {menu}
    </>
  );
  return (
    <li className="nav-item">
      {dropListId === undefined ? content : <ListDrop listId={dropListId}>{content}</ListDrop>}
    </li>
  );
}

/** A drop target that moves dropped tasks into a list. */
function ListDrop({ listId, children }: { listId: string; children: ReactNode }) {
  const { t } = useTranslation();
  const { actions } = useServices();
  const onDrop = async (event: DropEvent) => {
    for (const dropped of event.items) {
      if (dropped.kind === "text" && dropped.types.has(taskDragType)) {
        const taskId = await dropped.getText(taskDragType);
        await actions.moveTask(taskId, listId);
      }
    }
  };
  return (
    <DropZone
      className="drop-zone"
      aria-label={t("nav.moveHere")}
      getDropOperation={(types) => (types.has(taskDragType) ? "move" : "cancel")}
      onDrop={(event) => {
        void onDrop(event);
      }}
    >
      {children}
    </DropZone>
  );
}

function ListItem({
  list,
  count,
  onNavigate,
}: {
  list: List;
  count: number;
  onNavigate: () => void;
}) {
  const { t } = useTranslation();
  const { actions, toasts } = useServices();
  const navigate = useNavigate();
  const location = useLocation();
  const [editing, setEditing] = useState(false);
  const path = `/lists/${list.id}`;

  const remove = async () => {
    const ok = await actions.deleteList(list.id);
    if (!ok) return;
    if (location.pathname === path) {
      void navigate("/");
    }
    toasts.show({
      message: t("lists.deleted"),
      tone: "info",
      action: {
        label: t("common.undo"),
        run: () => {
          void actions.restoreList(list.id);
        },
      },
    });
  };

  return (
    <>
      <NavItem
        to={path}
        dropListId={list.id}
        label={list.name}
        count={count}
        onNavigate={onNavigate}
        marker={
          <span
            className="list-dot"
            style={list.color ? { backgroundColor: list.color } : undefined}
            aria-hidden="true"
          />
        }
        menu={
          <MenuTrigger>
            <Button
              className="icon-button nav-menu"
              aria-label={t("lists.actions", { name: list.name })}
            >
              <Ellipsis size={16} strokeWidth={1.5} aria-hidden="true" />
            </Button>
            <Popover className="popover">
              <Menu
                className="menu"
                onAction={(key) => {
                  if (key === "edit") setEditing(true);
                  if (key === "delete") void remove();
                }}
              >
                <MenuItem id="edit" className="menu-item">
                  {t("lists.edit")}
                </MenuItem>
                <MenuItem id="delete" className="menu-item menu-item-danger">
                  {t("lists.delete")}
                </MenuItem>
              </Menu>
            </Popover>
          </MenuTrigger>
        }
      />
      {editing && (
        <ListDialog
          isOpen
          onClose={() => setEditing(false)}
          title={t("lists.edit")}
          initialName={list.name}
          initialColor={list.color ?? null}
          onSave={(name, color) => {
            void actions.updateList(list.id, { name, color });
          }}
        />
      )}
    </>
  );
}

function TagItem({ tag, onNavigate }: { tag: Tag; onNavigate: () => void }) {
  const { t } = useTranslation();
  const { actions } = useServices();
  const navigate = useNavigate();
  const location = useLocation();
  const [dialog, setDialog] = useState<"rename" | "delete" | null>(null);
  const path = `/tags/${tag.id}`;

  return (
    <>
      <NavItem
        to={path}
        icon={Hash}
        label={tag.name}
        onNavigate={onNavigate}
        menu={
          <MenuTrigger>
            <Button
              className="icon-button nav-menu"
              aria-label={t("tags.actions", { name: tag.name })}
            >
              <Ellipsis size={16} strokeWidth={1.5} aria-hidden="true" />
            </Button>
            <Popover className="popover">
              <Menu
                className="menu"
                onAction={(key) => {
                  setDialog(key === "rename" ? "rename" : "delete");
                }}
              >
                <MenuItem id="rename" className="menu-item">
                  {t("tags.rename")}
                </MenuItem>
                <MenuItem id="delete" className="menu-item menu-item-danger">
                  {t("tags.delete")}
                </MenuItem>
              </Menu>
            </Popover>
          </MenuTrigger>
        }
      />
      {dialog === "rename" && (
        <NameDialog
          isOpen
          onClose={() => setDialog(null)}
          title={t("tags.rename")}
          label={t("tags.name")}
          initialName={tag.name}
          maxLength={50}
          onSave={(name) => {
            void actions.updateTag(tag.id, { name });
          }}
        />
      )}
      {dialog === "delete" && (
        <ConfirmDialog
          isOpen
          onClose={() => setDialog(null)}
          title={t("tags.delete")}
          message={t("tags.deleteConfirm", { name: tag.name })}
          confirmLabel={t("tags.delete")}
          onConfirm={() => {
            if (location.pathname === path) void navigate("/");
            void actions.deleteTag(tag.id);
          }}
        />
      )}
    </>
  );
}
