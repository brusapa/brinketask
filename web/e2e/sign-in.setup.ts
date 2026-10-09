// Signs in once through the real OIDC flow and saves the session for the
// tests (SPEC section 11: "log in (test OIDC provider)").
import { expect, test as setup } from "@playwright/test";

import { sessionState } from "../playwright.config";
import { pocketIdAPIKey, pocketIdURL, userID } from "./env";

setup("sign in through Pocket ID", async ({ page, request }) => {
  // Pocket ID's one-time access token signs the user in to Pocket ID
  // without a passkey, which a headless browser does not have.
  const created = await request.post(`${pocketIdURL}/api/users/${userID}/one-time-access-token`, {
    headers: { "X-API-Key": pocketIdAPIKey },
    data: {},
  });
  expect(created.ok()).toBe(true);
  const { token } = (await created.json()) as { token: string };
  const signedIn = await page.request.post(`${pocketIdURL}/api/one-time-access-token/${token}`);
  expect(signedIn.ok()).toBe(true);

  // Without a session the web client goes to /auth/login itself: to
  // Pocket ID, which knows the user now, and back through the callback.
  const callback = page.waitForResponse((r) => new URL(r.url()).pathname === "/auth/callback");
  await page.goto("/");
  // A redirect into the app, not to an error.
  const answer = await callback;
  expect(answer.status()).toBe(303);
  expect(answer.headers().location).not.toContain("auth_error");
  await expect(page.getByRole("navigation").getByRole("link", { name: /^Inbox/ })).toBeVisible();
  await page.context().storageState({ path: sessionState });
});
