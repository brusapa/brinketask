import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";

import { App } from "./App";

test("renders through the i18n layer", () => {
  render(<App />);
  expect(screen.getByText("Loading…")).toBeDefined();
});
