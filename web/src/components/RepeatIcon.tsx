// The repeat icon of a recurring task (DESIGN.md section 5, item 4). It
// carries meaning on its own, so it has an accessible name.
import { Repeat } from "lucide-react";
import { useTranslation } from "react-i18next";

export function RepeatIcon() {
  const { t } = useTranslation();
  return (
    <span className="repeat-icon" role="img" aria-label={t("task.repeats")}>
      <Repeat size={14} strokeWidth={1.5} aria-hidden="true" />
    </span>
  );
}
