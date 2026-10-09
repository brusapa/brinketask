// The toast region (DESIGN.md section 6: bottom centre, above the quick-add
// field on touch devices). role="status" makes screen readers announce new
// messages without moving focus.
import { X } from "lucide-react";
import { useSyncExternalStore } from "react";
import { Button } from "react-aria-components";
import { useTranslation } from "react-i18next";

import { useServices } from "../app/services";

export function Toasts() {
  const { t } = useTranslation();
  const { toasts } = useServices();
  const current = useSyncExternalStore(toasts.subscribe, toasts.getSnapshot);
  return (
    <div className="toasts" role="status" aria-live="polite">
      {current.map((toast) => (
        <div key={toast.id} className={toast.tone === "error" ? "toast toast-error" : "toast"}>
          <span>{toast.message}</span>
          {toast.action && (
            <Button
              className="toast-action"
              onPress={() => {
                toast.action?.run();
                toasts.dismiss(toast.id);
              }}
            >
              {toast.action.label}
            </Button>
          )}
          <Button
            className="icon-button"
            aria-label={t("common.dismiss")}
            onPress={() => toasts.dismiss(toast.id)}
          >
            <X size={16} strokeWidth={1.5} aria-hidden="true" />
          </Button>
        </div>
      ))}
    </div>
  );
}
