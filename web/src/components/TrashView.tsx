// The trash (SPEC section 9, D-40): deleted lists, then tasks deleted on
// their own in the last 30 days, each with a restore action. Deleted
// resources are not in the replica, so the trash is read from the API.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { RotateCcw } from "lucide-react";
import { Button } from "react-aria-components";
import { useTranslation } from "react-i18next";

import { unwrap, type Api } from "../api/client";
import type { List, Task } from "../api/types";
import { useProfile, useServices, useSnapshot } from "../app/services";
import { dateIn } from "../lib/dates";
import { formatDay } from "../lib/format";
import { ViewHeader } from "./ViewHeader";

/** Reads every page of the deleted tasks. */
async function deletedTasks(api: Api): Promise<Task[]> {
  const result: Task[] = [];
  let cursor: string | undefined;
  do {
    const page = await unwrap(() =>
      api.GET("/tasks", {
        params: { query: { deleted: true, limit: 500, ...(cursor ? { cursor } : {}) } },
      }),
    );
    result.push(...page.items);
    cursor = page.next_cursor ?? undefined;
  } while (cursor !== undefined);
  return result;
}

export function TrashView() {
  const { t } = useTranslation();
  const { api, actions } = useServices();
  const queryClient = useQueryClient();
  // Always read again when the screen opens: deletes happen elsewhere.
  const lists = useQuery({
    queryKey: ["trash", "lists"],
    queryFn: () => unwrap(() => api.GET("/lists", { params: { query: { deleted: true } } })),
    staleTime: 0,
  });
  const tasks = useQuery({
    queryKey: ["trash", "tasks"],
    queryFn: () => deletedTasks(api),
    staleTime: 0,
  });

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["trash"] });
  };
  const deletedLists = lists.data?.items ?? [];
  const deleted = tasks.data ?? [];
  const loaded = lists.isSuccess && tasks.isSuccess;

  return (
    <section className="view" aria-label={t("nav.trash")}>
      <ViewHeader title={t("nav.trash")} />
      <p className="view-note">{t("trash.note")}</p>
      {loaded && deletedLists.length === 0 && deleted.length === 0 && (
        <p className="empty">{t("trash.empty")}</p>
      )}
      {(lists.isError || tasks.isError) && <p className="empty">{t("trash.loadFailed")}</p>}
      {deletedLists.length > 0 && (
        <TrashSection
          heading={t("nav.lists")}
          items={deletedLists}
          onRestore={(list) => {
            void actions.restoreList(list.id).then(refresh);
          }}
        />
      )}
      {deleted.length > 0 && (
        <TrashSection
          heading={t("views.tasks")}
          items={deleted}
          onRestore={(task) => {
            void actions.restoreTask(task.id).then(refresh);
          }}
        />
      )}
    </section>
  );
}

function TrashSection<T extends List | Task>({
  heading,
  items,
  onRestore,
}: {
  heading: string;
  items: T[];
  onRestore: (item: T) => void;
}) {
  const { t, i18n } = useTranslation();
  const zone = useProfile().timezone;
  const snapshot = useSnapshot();
  return (
    <div className="task-section">
      <h2 className="section-heading">
        <span>{heading}</span>
        <span className="section-count">{items.length}</span>
      </h2>
      <ul className="task-list">
        {items.map((item) => {
          const name = "name" in item ? item.name : item.title;
          const listName = "list_id" in item ? snapshot.lists.get(item.list_id)?.name : undefined;
          const deletedOn = item.deleted_at
            ? formatDay(dateIn(new Date(item.deleted_at), zone), zone, i18n.language)
            : "";
          return (
            <li key={item.id} className="task-row">
              <span className="task-title">{name}</span>
              <span className="task-meta">
                {listName !== undefined && <span className="task-list-name">{listName}</span>}
                <span>{t("trash.deletedOn", { date: deletedOn })}</span>
              </span>
              <Button
                className="header-button"
                onPress={() => onRestore(item)}
                aria-label={t("trash.restoreItem", { name })}
              >
                <RotateCcw size={16} strokeWidth={1.5} aria-hidden="true" />
                <span>{t("trash.restore")}</span>
              </Button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
