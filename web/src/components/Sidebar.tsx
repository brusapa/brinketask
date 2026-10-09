// The sidebar (DESIGN.md section 2): views with their open-task counts, the
// lists with their colour dot and count, the tags, and Trash and Settings
// at the bottom. Lists can be reordered by dragging (or with the keyboard),
// and a task dropped on a list, or on the inbox, moves there.
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
  DropIndicator,
  DropZone,
  GridList,
  GridListItem,
  Menu,
  MenuItem,
  MenuTrigger,
  Popover,
  useDragAndDrop,
  type DropZoneProps,
} from "react-aria-components";
import { useTranslation } from "react-i18next";
import { NavLink, useLocation, useNavigate } from "react-router";

import type { List, Tag } from "../api/types";
import { useNow, useProfile, useServices, useSnapshot } from "../app/services";
import { counts, sortedLists, sortedTags } from "../data/views";
import { positionForMove } from "../lib/positions";
import { ConfirmDialog, ListDialog, NameDialog } from "./dialogs";
import { listDragType, taskDragType } from "./drag";

// The event React Aria passes to a drop target, taken from its props.
type DropEvent = Parameters<NonNullable<DropZoneProps["onDrop"]>>[0];

export function Sidebar() {
  const { t } = useTranslation();
  const snapshot = useSnapshot();
  const profile = useProfile();
  const now = useNow();
  const { actions } = useServices();
  const [creating, setCreating] = useState(false);

  const c = counts({ snapshot, zone: profile.timezone, now });
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
          dropListId={profile.inbox_list_id}
        />
        <NavItem to="/today" icon={CalendarDays} label={t("nav.today")} count={c.today} />
        <NavItem to="/next7" icon={CalendarRange} label={t("nav.next7")} count={c.next7} />
      </ul>

      <div className="sidebar-heading">
        <h2 id="sidebar-lists">{t("nav.lists")}</h2>
        <Button
          className="icon-button"
          aria-label={t("nav.newList")}
          onPress={() => setCreating(true)}
        >
          <Plus size={16} strokeWidth={1.5} aria-hidden="true" />
        </Button>
      </div>
      <Lists counts={c.lists} />

      {tags.length > 0 && (
        <>
          <div className="sidebar-heading">
            <h2>{t("nav.tags")}</h2>
          </div>
          <ul className="nav-list">
            {tags.map((tag) => (
              <TagItem key={tag.id} tag={tag} />
            ))}
          </ul>
        </>
      )}

      <ul className="nav-list sidebar-bottom">
        <NavItem to="/trash" icon={Trash2} label={t("nav.trash")} />
        <NavItem to="/settings" icon={Settings} label={t("nav.settings")} />
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
  menu,
  dropListId,
}: {
  to: string;
  icon: LucideIcon;
  label: string;
  count?: number;
  menu?: ReactNode;
  /** Makes the item a drop target that moves tasks into this list. */
  dropListId?: string;
}) {
  const content = (
    <>
      {/* NavLink marks the current page with aria-current="page". "end"
          keeps "/" from matching every path. */}
      <NavLink to={to} end className="nav-link">
        <Icon size={16} strokeWidth={1.5} aria-hidden="true" />
        <span className="nav-label">{label}</span>
        <Count count={count} />
      </NavLink>
      {menu}
    </>
  );
  return (
    <li className="nav-item">
      {dropListId === undefined ? content : <InboxDrop listId={dropListId}>{content}</InboxDrop>}
    </li>
  );
}

function Count({ count }: { count: number | undefined }) {
  const { t } = useTranslation();
  if (count === undefined || count === 0) return null;
  return (
    <span className="nav-count" aria-label={t("nav.count", { count })}>
      {count}
    </span>
  );
}

/** The inbox entry as a drop target that moves dropped tasks into it. */
function InboxDrop({ listId, children }: { listId: string; children: ReactNode }) {
  const { t } = useTranslation();
  const { actions } = useServices();
  const onDrop = async (event: DropEvent) => {
    for (const dropped of event.items) {
      if (dropped.kind === "text" && dropped.types.has(taskDragType)) {
        await actions.moveTask(await dropped.getText(taskDragType), listId);
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

/**
 * The user's lists, other than the inbox, in manual order (D-12). A grid
 * rather than plain links because React Aria's drag and drop, keyboard
 * included, works on collections: dragging a list reorders it; dropping
 * a task on a list moves the task.
 */
function Lists({ counts: listCounts }: { counts: ReadonlyMap<string, number> }) {
  const snapshot = useSnapshot();
  const { actions } = useServices();
  const location = useLocation();
  const lists = sortedLists(snapshot).filter((l) => !l.is_inbox);

  const { dragAndDropHooks } = useDragAndDrop({
    getItems: (keys) =>
      [...keys].map((key) => ({
        [listDragType]: String(key),
        "text/plain": lists.find((l) => l.id === key)?.name ?? "",
      })),
    acceptedDragTypes: [listDragType, taskDragType],
    // Tasks drop on a list ("on"); lists drop between lists.
    shouldAcceptItemDrop: (_target, types) => types.has(taskDragType),
    onItemDrop: (event) => {
      const listId = String(event.target.key);
      for (const dropped of event.items) {
        if (dropped.kind === "text" && dropped.types.has(taskDragType)) {
          void dropped.getText(taskDragType).then((taskId) => actions.moveTask(taskId, listId));
        }
      }
    },
    onReorder: (event) => {
      const moved = String([...event.keys][0]);
      // All lists, the inbox included, so a list dropped first still sorts
      // after the inbox's "a0".
      const position = positionForMove(
        sortedLists(snapshot),
        moved,
        String(event.target.key),
        event.target.dropPosition,
      );
      if (position !== null) void actions.updateList(moved, { position });
    },
    renderDropIndicator: (target) => <DropIndicator target={target} className="drop-indicator" />,
  });

  return (
    <GridList
      className="nav-list"
      aria-labelledby="sidebar-lists"
      items={lists}
      dragAndDropHooks={dragAndDropHooks}
      renderEmptyState={() => null}
    >
      {(list) => {
        const path = `/lists/${list.id}`;
        return (
          // href makes the row a link (React Aria navigates through the
          // RouterProvider set up in Layout).
          <GridListItem
            id={list.id}
            href={path}
            textValue={list.name}
            className={location.pathname === path ? "nav-row nav-row-current" : "nav-row"}
          >
            <span
              className="list-dot"
              style={list.color ? { backgroundColor: list.color } : undefined}
              aria-hidden="true"
            />
            <span className="nav-label">{list.name}</span>
            <Count count={listCounts.get(list.id)} />
            <ListMenu list={list} />
          </GridListItem>
        );
      }}
    </GridList>
  );
}

function ListMenu({ list }: { list: List }) {
  const { t } = useTranslation();
  const { actions, toasts } = useServices();
  const navigate = useNavigate();
  const location = useLocation();
  const [editing, setEditing] = useState(false);

  const remove = async () => {
    const ok = await actions.deleteList(list.id);
    if (!ok) return;
    if (location.pathname === `/lists/${list.id}`) {
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

function TagItem({ tag }: { tag: Tag }) {
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
