// Completing or skipping a task from anywhere in the app, followed by the
// undo toast (SPEC section 9). For a recurring task that moved on, the
// toast names the next date, since the row stays where it was.
import { parseDate } from "@internationalized/date";
import { useTranslation } from "react-i18next";

import { useProfile, useServices } from "../app/services";
import { formatDay } from "../lib/format";

export function useRecord(): (taskId: string, action: "complete" | "skip") => Promise<void> {
  const { t, i18n } = useTranslation();
  const { actions, toasts } = useServices();
  const zone = useProfile().timezone;

  return async (taskId, action) => {
    const recorded =
      action === "complete" ? await actions.complete(taskId) : await actions.skip(taskId);
    if (recorded === undefined) return;
    const { task } = recorded;
    const next =
      task.status === "open" && task.due_date
        ? formatDay(parseDate(task.due_date), zone, i18n.language)
        : null;
    let message: string;
    if (action === "skip") {
      message = next === null ? t("task.skipped") : t("task.skippedNext", { date: next });
    } else {
      message = next === null ? t("task.completed") : t("task.completedNext", { date: next });
    }
    toasts.show({
      message,
      tone: "info",
      action: {
        label: t("common.undo"),
        run: () => {
          void actions.uncomplete(taskId, recorded.completionId);
        },
      },
    });
  };
}
