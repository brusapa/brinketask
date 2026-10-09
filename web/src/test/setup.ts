// Runs before every test file: loads the real catalog, so tests find
// elements by the text users see.
import "../i18n";

import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// Unmount what a test rendered, so the next test starts from an empty page.
afterEach(() => {
  cleanup();
});
