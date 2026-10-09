// Playwright configuration of the end-to-end test (SPEC section 11), run by
// deploy/e2e.sh against the built image. The tests are e2e/*.e2e.ts, so
// Vitest, which picks up *.test.* and *.spec.*, never runs them.
import { defineConfig, devices } from "@playwright/test";

const baseURL = process.env.E2E_BASE_URL ?? "http://localhost:18080";
// Where the setup project leaves the signed-in session for the tests.
export const sessionState = "e2e/.auth/session.json";

export default defineConfig({
  testDir: "e2e",
  testMatch: "*.e2e.ts",
  // One user and one database: the tests run one after another.
  workers: 1,
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: {
    ...devices["Desktop Chrome"],
    baseURL,
    // The reminder test shows notifications.
    permissions: ["notifications"],
    // The profile's default zone, so the zone banner stays away.
    timezoneId: "Europe/Madrid",
    trace: "retain-on-failure",
    launchOptions: {
      // Playwright's own Chromium, or the one given (it cannot run on
      // every Linux distribution).
      executablePath: process.env.PLAYWRIGHT_CHROMIUM || undefined,
    },
  },
  projects: [
    { name: "sign-in", testMatch: "sign-in.setup.ts" },
    {
      name: "chromium",
      dependencies: ["sign-in"],
      use: { storageState: sessionState },
    },
  ],
});
