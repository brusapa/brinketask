import { screen, waitFor, within } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import { json, noContent } from "../test/fakeFetch";
import { list, task } from "../test/fixtures";
import { inboxId, renderApp, testUser } from "../test/render";

describe("search (D-52)", () => {
  test("typing in the header searches open tasks, ignoring accents", async () => {
    const { user } = await renderApp({
      state: {
        tasks: [
          task({ id: "a", list_id: inboxId, title: "Café con leche" }),
          task({ id: "b", list_id: inboxId, title: "Tea" }),
          task({ id: "c", list_id: inboxId, title: "cafe", status: "done" }),
        ],
      },
    });
    await user.type(await screen.findByRole("searchbox", { name: "Search tasks" }), "cafe");
    expect(await screen.findByRole("heading", { level: 1, name: "Search" })).toBeDefined();
    const rows = await screen.findAllByRole("row");
    expect(rows.map((r) => r.textContent)).toEqual([expect.stringContaining("Café con leche")]);
    // Search has no Completed section (D-54).
    expect(screen.queryByRole("button", { name: /Completed/ })).toBeNull();
  });
});

describe("trash (D-40)", () => {
  test("lists deleted lists and tasks, and restores them", async () => {
    const deletedList = list({
      id: "old",
      name: "Old project",
      deleted_at: "2026-10-05T10:00:00Z",
      version: 2,
    });
    const deletedTask = task({
      id: "t",
      list_id: inboxId,
      title: "Old task",
      deleted_at: "2026-10-08T10:00:00Z",
      version: 2,
    });
    const { server, user } = await renderApp({
      path: "/trash",
      setup: (s) => {
        s.on("GET", "/lists", json({ items: [deletedList] }));
        s.on("GET", "/tasks", json({ items: [deletedTask], next_cursor: null }));
        s.on("POST", "/tasks/t/restore", json({ ...deletedTask, deleted_at: null, version: 3 }));
        s.on("POST", "/lists/old/restore", json({ ...deletedList, deleted_at: null, version: 3 }));
      },
    });
    const main = screen.getByRole("main");
    expect(await within(main).findByText("Deleted Mon, Oct 5")).toBeDefined();
    await user.click(await screen.findByRole("button", { name: "Restore “Old task”" }));
    await waitFor(() => {
      expect(server.calls("POST", "/tasks/t/restore")).toHaveLength(1);
    });
    await user.click(screen.getByRole("button", { name: "Restore “Old project”" }));
    await waitFor(() => {
      expect(server.calls("POST", "/lists/old/restore")).toHaveLength(1);
    });
    // The restored list is back in the sidebar.
    expect(
      await within(screen.getByRole("navigation")).findByRole("row", { name: /Old project/ }),
    ).toBeDefined();
    expect(server.calls("GET", "/tasks")[0]?.query.get("deleted")).toBe("true");
  });

  test("an empty trash says so", async () => {
    await renderApp({
      path: "/trash",
      setup: (s) => {
        s.on("GET", "/lists", json({ items: [] }));
        s.on("GET", "/tasks", json({ items: [], next_cursor: null }));
      },
    });
    expect(await screen.findByText("The trash is empty.")).toBeDefined();
  });
});

describe("settings", () => {
  test("changing the time zone saves the profile", async () => {
    const { server, user } = await renderApp({ path: "/settings" });
    server.on("PATCH", "/me", (r) => json({ ...testUser, ...(r.body as object) }));
    const zone = await screen.findByRole("combobox", { name: "Time zone" });
    await user.clear(zone);
    await user.type(zone, "Tokyo");
    await user.click(await screen.findByRole("option", { name: "Asia/Tokyo" }));
    await waitFor(() => {
      expect(server.calls("PATCH", "/me")[0]?.body).toEqual({ timezone: "Asia/Tokyo" });
    });
    expect(server.calls("PATCH", "/me")[0]?.contentType).toBe("application/merge-patch+json");
  });

  test("signing out ends the session and shows the signed-out page", async () => {
    const { server, user, left } = await renderApp({ path: "/settings" });
    server.on("POST", /\/auth\/logout$/, noContent());
    await user.click(await screen.findByRole("button", { name: "Sign out" }));
    await waitFor(() => {
      expect(left).toEqual(["/?signed_out=1"]);
    });
    expect(server.requests.some((r) => r.method === "POST" && r.path === "/auth/logout")).toBe(
      true,
    );
  });
});

// SPEC section 9: the client offers to update the profile zone.
describe("time zone banner", () => {
  test("offers the device's zone when it differs from the profile's", async () => {
    const { server, user } = await renderApp({ browserZone: "America/New_York" });
    server.on("PATCH", "/me", (r) => json({ ...testUser, ...(r.body as object) }));
    const banner = await screen.findByRole("region", { name: "Time zone" });
    expect(banner.textContent).toContain("America/New_York");
    await user.click(within(banner).getByRole("button", { name: "Use America/New_York" }));
    await waitFor(() => {
      expect(server.calls("PATCH", "/me")[0]?.body).toEqual({ timezone: "America/New_York" });
    });
    await waitFor(() => {
      expect(screen.queryByRole("region", { name: "Time zone" })).toBeNull();
    });
  });

  test("keeping the profile zone hides the offer", async () => {
    window.localStorage.clear();
    const { user } = await renderApp({ browserZone: "Asia/Tokyo" });
    await user.click(await screen.findByRole("button", { name: "Keep Europe/Madrid" }));
    expect(screen.queryByRole("region", { name: "Time zone" })).toBeNull();
    window.localStorage.clear();
  });

  test("no offer when the zones match", async () => {
    await renderApp();
    expect(screen.queryByRole("region", { name: "Time zone" })).toBeNull();
  });
});
