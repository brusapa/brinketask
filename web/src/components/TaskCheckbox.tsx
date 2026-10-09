// The round-cornered checkbox of task rows, the detail header and the
// checklist (DESIGN.md section 5). React Aria draws nothing; the box is a
// span styled by app.css from the data-selected attribute it sets.
import { CheckboxButton, CheckboxField } from "react-aria-components";

export function TaskCheckbox({
  isSelected,
  isDisabled = false,
  onChange,
  label,
  className = "",
}: {
  isSelected: boolean;
  isDisabled?: boolean;
  onChange: (selected: boolean) => void;
  /** The accessible name, e.g. "Complete task, high priority". */
  label: string;
  className?: string;
}) {
  return (
    <CheckboxField
      className={`checkbox ${className}`}
      isSelected={isSelected}
      isDisabled={isDisabled}
      onChange={onChange}
      aria-label={label}
    >
      <CheckboxButton className="checkbox-button">
        <span className="checkbox-box" aria-hidden="true" />
      </CheckboxButton>
    </CheckboxField>
  );
}
