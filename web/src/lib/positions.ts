// Manual order with fractional indexes (D-12): every item carries a string,
// items sort by it, and moving an item only needs a new string between its
// neighbours. Strings compare by code point (D-51), which is what `<` does
// for the ASCII strings used here.
import { generateKeyBetween } from "fractional-indexing";

/** Compares two positions, then ids, as the server does (D-43). */
export function comparePositions(
  a: { position: string; id: string },
  b: { position: string; id: string },
): number {
  if (a.position !== b.position) {
    return a.position < b.position ? -1 : 1;
  }
  if (a.id === b.id) {
    return 0;
  }
  return a.id < b.id ? -1 : 1;
}

/**
 * Returns a position strictly between before and after; null means "no
 * neighbour on that side". The library expects keys in its own format; a
 * position written by another client may not be one, so fallbackBetween
 * covers any string.
 */
export function positionBetween(before: string | null, after: string | null): string {
  if (before !== null && after !== null && before >= after) {
    // Equal neighbours (two clients picked the same string) leave no room
    // between them; the server orders such ties by id. Going just after
    // "before" is the closest valid place.
    return positionBetween(before, null);
  }
  try {
    return generateKeyBetween(before, after);
  } catch {
    return fallbackBetween(before, after);
  }
}

/** A position after every one given. */
export function positionAtEnd(positions: readonly string[]): string {
  let last: string | null = null;
  for (const p of positions) {
    if (last === null || p > last) {
      last = p;
    }
  }
  return positionBetween(last, null);
}

/** A position before every one given. */
export function positionAtStart(positions: readonly string[]): string {
  let first: string | null = null;
  for (const p of positions) {
    if (first === null || p < first) {
      first = p;
    }
  }
  return positionBetween(null, first);
}

// "V" sits in the middle of the alphabet, leaving room on both sides.
const middle = "V";

function fallbackBetween(before: string | null, after: string | null): string {
  if (before === null && after === null) {
    return "a0";
  }
  if (after === null) {
    // Any string that starts with `before` and is longer sorts after it.
    return (before ?? "") + middle;
  }
  if (before === null) {
    return below(after);
  }
  const candidate = before + middle;
  if (candidate < after) {
    return candidate;
  }
  // `after` starts with `before` and continues with a character at or below
  // the middle: go below that continuation.
  return before + below(after.slice(before.length));
}

/** A non-empty string that sorts before s (s must be non-empty). */
function below(s: string): string {
  if (s.length > 1) {
    // A proper prefix sorts before the string.
    return s.slice(0, 1);
  }
  const code = s.charCodeAt(0);
  // One character lower, then the middle, so there is room on both sides.
  return String.fromCharCode(Math.max(code - 1, 1)) + middle;
}

/**
 * The new position of an item dropped before or after another one in an
 * ordered list, or null when the target is not in the list. `items` is
 * the list in display order, the moved item included.
 */
export function positionForMove(
  items: readonly { id: string; position: string }[],
  movedId: string,
  targetId: string,
  where: "before" | "after" | "on",
): string | null {
  const others = items.filter((item) => item.id !== movedId);
  const target = others.findIndex((item) => item.id === targetId);
  if (target < 0) return null;
  const insertAt = where === "before" ? target : target + 1;
  return positionBetween(
    others[insertAt - 1]?.position ?? null,
    others[insertAt]?.position ?? null,
  );
}
