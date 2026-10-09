// The objects every screen needs (API, replica, writes, clock, profile),
// handed down through React context so tests can build them with fakes.
import { QueryClient } from "@tanstack/react-query";
import { createContext, useContext, useEffect, useState, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";

import { ApiError, type Api } from "../api/client";
import type { User } from "../api/types";
import { Actions } from "../data/actions";
import { Replica, type Snapshot } from "../data/replica";
import { Syncer } from "../data/sync";
import type { Clock } from "../lib/clock";
import { msUntilNextDay } from "../lib/dates";
import type { PushEnvironment } from "../push/browser";
import { ToastStore } from "./toasts";

export interface Services {
  api: Api;
  replica: Replica;
  syncer: Syncer;
  actions: Actions;
  clock: Clock;
  toasts: ToastStore;
  queryClient: QueryClient;
  /** The signed-in user's profile; replaced when it changes. */
  profile: ProfileStore;
  /** The browser's own time zone, to compare with the profile's (D-47). */
  browserZone: string;
  /** Ends the session (POST /auth/logout) and shows the signed-out page. */
  logout: () => Promise<void>;
  /** The browser's push APIs (a fake in tests). */
  push: PushEnvironment;
}

/** The profile, kept outside React so Actions can read the zone. */
export class ProfileStore {
  private user: User;
  private listeners = new Set<() => void>();

  constructor(user: User) {
    this.user = user;
  }

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  get = (): User => this.user;

  set(user: User): void {
    this.user = user;
    for (const listener of this.listeners) listener();
  }
}

/** Builds the services once the profile is known. */
export function createServices(options: {
  api: Api;
  clock: Clock;
  user: User;
  browserZone: string;
  logout: () => Promise<void>;
  push: PushEnvironment;
  onError: (error: unknown, toasts: ToastStore) => void;
}): Services {
  const replica = new Replica();
  const syncer = new Syncer(options.api, replica);
  const toasts = new ToastStore();
  const profile = new ProfileStore(options.user);
  const queryClient = new QueryClient({
    defaultOptions: {
      // The replica is refreshed on focus; queries (the Completed section)
      // follow the same rhythm.
      queries: { retry: 1, refetchOnWindowFocus: true, staleTime: 30_000 },
    },
  });
  const actions = new Actions({
    api: options.api,
    replica,
    syncer,
    clock: options.clock,
    zone: () => profile.get().timezone,
    onCompletionsChanged: () => {
      void queryClient.invalidateQueries({ queryKey: ["completions"] });
    },
    onError: (error) => {
      options.onError(error, toasts);
    },
  });
  return {
    api: options.api,
    replica,
    syncer,
    actions,
    clock: options.clock,
    toasts,
    queryClient,
    profile,
    browserZone: options.browserZone,
    logout: options.logout,
    push: options.push,
  };
}

export const ServicesContext = createContext<Services | null>(null);

export function useServices(): Services {
  const services = useContext(ServicesContext);
  if (services === null) {
    throw new Error("useServices outside <ServicesContext>");
  }
  return services;
}

/** The current replica snapshot; the component re-renders when it changes. */
export function useSnapshot(): Snapshot {
  const { replica } = useServices();
  return useSyncExternalStore(replica.subscribe, replica.getSnapshot);
}

export function useProfile(): User {
  const { profile } = useServices();
  return useSyncExternalStore(profile.subscribe, profile.get);
}

/**
 * The current time, refreshed every minute and right after local midnight,
 * so "today", overdue states and the Completed section move with the clock.
 */
export function useNow(): Date {
  const { clock } = useServices();
  const zone = useProfile().timezone;
  const [now, setNow] = useState(() => clock.now());
  useEffect(() => {
    const untilMidnight = msUntilNextDay(clock.now(), zone);
    const delay = Math.min(60_000, untilMidnight + 1000);
    const timer = setTimeout(() => {
      setNow(clock.now());
    }, delay);
    return () => {
      clearTimeout(timer);
    };
  }, [clock, zone, now]);
  return now;
}

/** Reports failed writes as error toasts, in the user's language. */
export function useErrorMessage(): (error: unknown) => string {
  const { t } = useTranslation();
  return (error) => errorMessage(error, t);
}

type Translate = ReturnType<typeof useTranslation>["t"];

export function errorMessage(error: unknown, t: Translate): string {
  if (error instanceof ApiError) {
    if (error.network) return t("errors.network");
    switch (error.code) {
      case "conflict":
        return t("errors.conflict");
      case "not_found":
        return t("errors.notFound");
      case "validation_failed":
        return t("errors.invalid");
      case "rate_limited":
        return t("errors.rateLimited");
      default:
        break;
    }
  }
  return t("errors.generic");
}
