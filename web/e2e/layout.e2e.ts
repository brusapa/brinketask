// Layout checks in a real browser that jsdom cannot make: rendered sizes.
import { expect, test } from "@playwright/test";

const run = Date.now().toString(36);

// Phones: touch input, so (pointer: coarse) applies. 320 px is the
// narrowest width supported; 393 px a common one.
for (const width of [320, 393]) {
  test.describe(`a ${width} px phone`, () => {
    test.use({ viewport: { width, height: 852 }, hasTouch: true, isMobile: true });

    // SPEC section 9, DESIGN section 5: on touch devices every pressable
    // target is at least 44 × 44, the days of the due-date calendar
    // included, and lies within the screen.
    test("calendar days are 44 px targets on a phone", async ({ page }) => {
      const title = `Pick a day ${width} ${run}`;
      await page.goto("/");
      const quickAdd = page.getByRole("textbox", { name: "Add a task" });
      await quickAdd.fill(title);
      await quickAdd.press("Enter");
      await page.getByRole("row").filter({ hasText: title }).getByText(title).tap();
      await page.getByRole("button", { name: /^Due: / }).tap();

      const cells = page.locator(".calendar-cell:not([data-outside-month])");
      await expect(cells.first()).toBeVisible();
      for (const cell of await cells.all()) {
        const box = await cell.boundingBox();
        if (box === null) throw new Error("a day of the month has no box");
        expect(box.width, "calendar day width").toBeGreaterThanOrEqual(44);
        expect(box.height, "calendar day height").toBeGreaterThanOrEqual(44);
        expect(box.x, "calendar day left edge").toBeGreaterThanOrEqual(0);
        expect(box.x + box.width, "calendar day right edge").toBeLessThanOrEqual(width);
      }
    });
  });
}
