// public/pressable.css stands in for a <style> element React Aria inserts
// at run time, found by id. If an upgrade renamed the id or the attribute,
// the browser would block React Aria's own insertion again (CSP) and the
// rule would apply to nothing; this test fails first.
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";

import { expect, test } from "vitest";

test("React Aria still uses the id and attribute public/pressable.css relies on", () => {
  const require = createRequire(import.meta.url);
  const root = dirname(require.resolve("react-aria/package.json"));
  const source = readFileSync(join(root, "dist/private/interactions/usePress.mjs"), "utf8");
  expect(source).toContain("'react-aria-pressable-style'");
  expect(source).toContain("'data-react-aria-pressable'");

  const css = readFileSync(join(import.meta.dirname, "../../public/pressable.css"), "utf8");
  expect(css).toContain("[data-react-aria-pressable]");
  const html = readFileSync(join(import.meta.dirname, "../../index.html"), "utf8");
  expect(html).toContain('id="react-aria-pressable-style"');
});
