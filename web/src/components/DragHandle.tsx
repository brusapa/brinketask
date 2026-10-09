// The handle of a draggable row. React Aria needs one so keyboard and
// screen reader users can start a drag (it labels the button itself, in
// the UI language); mouse users can drag the whole row.
import { GripVertical } from "lucide-react";
import { Button } from "react-aria-components";

export function DragHandle() {
  return (
    <Button slot="drag" className="drag-handle">
      <GripVertical size={16} strokeWidth={1.5} aria-hidden="true" />
    </Button>
  );
}
