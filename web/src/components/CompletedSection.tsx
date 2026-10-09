// The collapsed "Completed" section at the end of a view (SPEC sections 8
// and 9): what was completed today in the user's zone, in the view's scope
// (D-54). It reads completion records, not tasks, so a completed
// occurrence of a recurring task will show up too (D-09).
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight } from "lucide-react";
import { Button, Disclosure, DisclosurePanel, Heading } from "react-aria-components";
import { useTranslation } from "react-i18next";

import { unwrap } from "../api/client";
import type { CompletionEntry } from "../api/types";
import { useProfile, useServices } from "../app/services";
import { dateIn, dayBounds } from "../lib/dates";
import { formatTime } from "../lib/format";
import { TaskCheckbox } from "./TaskCheckbox";

export interface Scope {
  list_id?: string;
  tag_id?: string;
  due_to?: string;
}

export function CompletedSection({ scope, now }: { scope: Scope; now: Date }) {
  const { t, i18n } = useTranslation();
  const { api, actions } = useServices();
  const zone = useProfile().timezone;
  const { from, to } = dayBounds(dateIn(now, zone), zone);

  const query = useQuery({
    // The key names everything the result depends on, so a new day or
    // scope fetches again.
    queryKey: ["completions", scope, from, to],
    queryFn: () =>
      unwrap(() =>
        api.GET("/completions", {
          params: { query: { ...scope, completed_from: from, completed_to: to, limit: 500 } },
        }),
      ),
  });
  const entries: CompletionEntry[] = query.data?.items ?? [];
  if (entries.length === 0) {
    return null;
  }

  return (
    <Disclosure className="completed">
      {({ isExpanded }) => (
        <>
          <Heading className="section-heading">
            <Button slot="trigger" className="section-toggle">
              {isExpanded ? (
                <ChevronDown size={16} strokeWidth={1.5} aria-hidden="true" />
              ) : (
                <ChevronRight size={16} strokeWidth={1.5} aria-hidden="true" />
              )}
              <span>{t("sections.completed")}</span>
              <span className="section-count">{entries.length}</span>
            </Button>
          </Heading>
          <DisclosurePanel>
            <ul className="completed-list">
              {entries.map((entry) => (
                <li key={entry.id} className="task-row task-row-completed">
                  {/* Only the latest completion of a task can be undone
                      (SPEC section 8); older ones show no active box. */}
                  <TaskCheckbox
                    isSelected
                    isDisabled={!entry.can_undo}
                    onChange={() => {
                      void actions.uncomplete(entry.task.id, entry.id);
                    }}
                    label={t("task.uncomplete", { title: entry.task.title })}
                  />
                  <span className="task-title">{entry.task.title}</span>
                  <span className="task-due">
                    <time dateTime={entry.completed_at}>
                      {formatTime(new Date(entry.completed_at), zone, i18n.language)}
                    </time>
                  </span>
                </li>
              ))}
            </ul>
          </DisclosurePanel>
        </>
      )}
    </Disclosure>
  );
}
