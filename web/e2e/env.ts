// What deploy/e2e.sh tells the tests about the environment it started.

function required(name: string, fallback: string): string {
  return process.env[name] ?? fallback;
}

export const pocketIdURL = required("E2E_POCKET_ID_URL", "http://localhost:11411");
export const pocketIdAPIKey = required("E2E_POCKET_ID_API_KEY", "brinketask-dev-static-api-key");
export const fakePushURL = required("E2E_FAKEPUSH_URL", "http://localhost:18091");
// The user deploy/dev-oidc-setup.sh creates.
export const userID = "00000000-0000-4000-8000-00000000d001";
