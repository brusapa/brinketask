// Search over the local replica (D-52): a substring of the title or the
// description, ignoring case and accents.

/**
 * Removes accents and case: NFD splits "é" into "e" plus a combining accent,
 * and \p{M} matches every combining mark.
 */
export function fold(text: string): string {
  return text.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase();
}

/** Builds a matcher for a query; an empty query matches nothing. */
export function matcher(query: string): (title: string, description: string) => boolean {
  const needle = fold(query.trim());
  if (needle === "") {
    return () => false;
  }
  return (title, description) => fold(title).includes(needle) || fold(description).includes(needle);
}
