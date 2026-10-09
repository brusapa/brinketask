import { screen, waitFor } from "@testing-library/react";
import { expect, test } from "vitest";

import { FakeServer, json, problem } from "../test/fakeFetch";
import { page } from "../test/fixtures";
import { inbox, renderApp, testUser } from "../test/render";

test("without a session the browser goes to the login", async () => {
  const server = new FakeServer().on("GET", "/me", problem(401, "unauthenticated"));
  const { left } = await renderApp({ server, search: "" });
  await waitFor(() => {
    expect(left).toEqual(["/auth/login"]);
  });
});

// SPEC section 7: the callback reports failures with ?auth_error=<code>.
// The app shows the reason and does not send the browser back to the login
// by itself, which could loop.
test("a failed login shows its reason and a way to try again", async () => {
  const server = new FakeServer();
  const { left } = await renderApp({ server, search: "?auth_error=provider_unavailable" });
  expect(screen.getByRole("alert").textContent).toBe(
    "The sign-in service is not reachable. Please try again in a moment.",
  );
  expect(screen.getByRole("link", { name: "Sign in" }).getAttribute("href")).toBe("/auth/login");
  expect(server.requests).toHaveLength(0);
  expect(left).toHaveLength(0);
});

test("an unknown auth_error reads as a failed login", async () => {
  await renderApp({ server: new FakeServer(), search: "?auth_error=<script>" });
  expect(screen.getByRole("alert").textContent).toBe("Sign-in failed. Please try again.");
});

test("after signing out, the app waits for the user", async () => {
  const server = new FakeServer();
  await renderApp({ server, search: "?signed_out=1" });
  expect(screen.getByText("You are signed out.")).toBeDefined();
  expect(server.requests).toHaveLength(0);
});

test("loads the profile and the full state, then shows the inbox", async () => {
  const server = new FakeServer()
    .on("GET", "/me", json(testUser))
    .on("GET", "/sync/changes", json(page({ lists: [inbox] })))
    .on("GET", "/completions", json({ items: [] }));
  await renderApp({ server, search: "" });
  expect(await screen.findByRole("heading", { level: 1, name: "Inbox" })).toBeDefined();
  expect(server.calls("GET", "/sync/changes")[0]?.query.has("cursor")).toBe(false);
});

test("a server error offers to try again", async () => {
  const server = new FakeServer().on("GET", "/me", problem(500, "internal"), json(testUser));
  server.on("GET", "/sync/changes", json(page({ lists: [inbox] })));
  const { user } = await renderApp({ server, search: "" });
  const retry = await screen.findByRole("button", { name: "Try again" });
  await user.click(retry);
  expect(await screen.findByRole("heading", { level: 1, name: "Inbox" })).toBeDefined();
});
