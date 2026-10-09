// Priority 0-3 (SPEC section 4) as message keys: "priority.<key>" for the
// word inside a sentence, "priorityLabel.<key>" for a label on its own. The
// checkbox outline shows it by colour, and its accessible name by text
// (DESIGN.md section 1: never by colour alone).
export const priorityKeys = ["none", "low", "medium", "high"] as const;
export type PriorityKey = (typeof priorityKeys)[number];

export function priorityKey(priority: number): PriorityKey {
  return priorityKeys[priority] ?? "none";
}
