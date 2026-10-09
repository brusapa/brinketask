// Renders the whole app against a FakeServer, at a given URL and time, the
// way main.tsx does but with a memory router.
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";

import type { ChangesPage, User } from "../api/types";
import { AppRoutes } from "../App";
import { Root } from "../app/Root";
import { FixedClock } from "../lib/clock";
import { FakeServer, json, origin } from "./fakeFetch";
import { list, page } from "./fixtures";

export const inboxId = "00000000-0000-7000-8000-00000000000a";

export const testUser: User = {
  id: "00000000-0000-7000-8000-000000000001",
  email: "dev@example.com",
  display_name: "Dev",
  timezone: "Europe/Madrid",
  all_day_reminder_time: "09:00",
  inbox_list_id: inboxId,
};

export const inbox = list({ id: inboxId, name: "Inbox", is_inbox: true, position: "a0" });

/** Friday 2026-10-09, 12:00 in Madrid. */
export const testNow = "2026-10-09T10:00:00Z";

export interface Rendered {
  server: FakeServer;
  clock: FixedClock;
  left: string[];
  user: ReturnType<typeof userEvent.setup>;
}

export async function renderApp(
  options: {
    path?: string;
    state?: Partial<ChangesPage>;
    server?: FakeServer;
    search?: string;
  } = {},
): Promise<Rendered> {
  const server = options.server ?? new FakeServer();
  if (options.server === undefined) {
    server.on("GET", "/me", json(testUser));
    server.on(
      "GET",
      "/sync/changes",
      json(page({ ...options.state, lists: [inbox, ...(options.state?.lists ?? [])] })),
    );
    server.on("GET", "/completions", json({ items: [], next_cursor: null }));
  }
  const clock = new FixedClock(testNow);
  const left: string[] = [];
  const user = userEvent.setup();
  render(
    <Root
      env={{
        origin,
        search: options.search ?? "",
        clock,
        fetch: (request) => server.handleRequest(request),
        leave: (path) => left.push(path),
      }}
    >
      <MemoryRouter initialEntries={[options.path ?? "/"]}>
        <AppRoutes />
      </MemoryRouter>
    </Root>,
  );
  if (options.search === undefined) {
    // Wait for the first screen after /me and the first sync.
    await screen.findByRole("navigation", {}, { timeout: 3000 }).catch(() => undefined);
  }
  return { server, clock, left, user };
}
