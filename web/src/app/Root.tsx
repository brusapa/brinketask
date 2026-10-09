// Start of the app: checks the session, loads the profile and the replica,
// and only then shows the screens. Without a session it sends the browser
// to /auth/login, unless the login itself just failed or the user signed
// out, which get a page of their own so the browser does not loop.
import { QueryClientProvider } from "@tanstack/react-query";
import { useEffect, useState, type ReactNode } from "react";
import { I18nProvider } from "react-aria-components";
import { useTranslation } from "react-i18next";

import { ApiError, createApi, unwrap } from "../api/client";
import { i18n } from "../i18n";
import type { Clock } from "../lib/clock";
import { errorMessage, ServicesContext, createServices, type Services } from "./services";

export interface Environment {
  /** The page's origin, e.g. window.location.origin. */
  origin: string;
  /** The query string the page was opened with. */
  search: string;
  clock: Clock;
  /** Leaves the app for a server route (login) or a full reload. */
  leave: (path: string) => void;
  /** The browser's IANA time zone. */
  browserZone: string;
  fetch?: (request: Request) => Promise<Response>;
}

/** The codes /auth/callback may send back (SPEC section 7). */
const authErrors = ["access_denied", "login_failed", "provider_unavailable"] as const;
type AuthError = (typeof authErrors)[number];

// The translation function for messages built outside components.
const i18nT = i18n.t.bind(i18n);

type Boot =
  | { kind: "loading" }
  | { kind: "signedOut"; error: AuthError | null }
  | { kind: "failed" }
  | { kind: "ready"; services: Services };

export function Root({ env, children }: { env: Environment; children: ReactNode }) {
  const [boot, setBoot] = useState<Boot>(() => initialState(env.search));
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (boot.kind !== "loading") return;
    let cancelled = false;
    const api = createApi({
      origin: env.origin,
      fetch: env.fetch,
      onUnauthenticated: () => {
        env.leave("/auth/login");
      },
    });
    const start = async () => {
      try {
        const user = await unwrap(() => api.GET("/me"));
        const services = createServices({
          api,
          clock: env.clock,
          user,
          browserZone: env.browserZone,
          logout: async () => {
            // An operational route, outside the API contract (SPEC
            // section 7); it answers 204, with or without a session.
            const fetcher = env.fetch ?? ((request: Request) => globalThis.fetch(request));
            await fetcher(new Request(`${env.origin}/auth/logout`, { method: "POST" })).catch(
              () => undefined,
            );
            env.leave("/?signed_out=1");
          },
          onError: (error, toasts) => {
            // Rendered lazily: the translation is looked up at show time.
            toasts.show({ message: errorMessage(error, i18nT), tone: "error" });
          },
        });
        await services.syncer.pull();
        if (!cancelled) setBoot({ kind: "ready", services });
      } catch (err) {
        // A 401 already sent the browser to the login.
        if (!cancelled && !(err instanceof ApiError && err.status === 401)) {
          setBoot({ kind: "failed" });
        }
      }
    };
    void start();
    return () => {
      cancelled = true;
    };
    // `attempt` restarts the effect when the user presses "Try again".
  }, [boot.kind, attempt, env]);

  // The replica refreshes when the user comes back to the app (SPEC
  // section 8): on window focus and when the tab becomes visible.
  const services = boot.kind === "ready" ? boot.services : null;
  useEffect(() => {
    if (services === null) return;
    const refresh = () => {
      if (document.visibilityState === "visible") {
        services.syncer.pull().catch(() => {
          // A failed refresh is retried on the next focus; the data shown
          // stays as it was.
        });
      }
    };
    window.addEventListener("focus", refresh);
    document.addEventListener("visibilitychange", refresh);
    return () => {
      window.removeEventListener("focus", refresh);
      document.removeEventListener("visibilitychange", refresh);
    };
  }, [services]);

  switch (boot.kind) {
    case "loading":
      return <Loading />;
    case "signedOut":
      return <SignedOut error={boot.error} />;
    case "failed":
      return (
        <Failed
          onRetry={() => {
            setBoot({ kind: "loading" });
            setAttempt((n) => n + 1);
          }}
        />
      );
    case "ready":
      return (
        <ServicesContext.Provider value={boot.services}>
          <QueryClientProvider client={boot.services.queryClient}>
            {/* React Aria formats dates and reads labels in this locale. */}
            <I18nProvider locale="en">{children}</I18nProvider>
          </QueryClientProvider>
        </ServicesContext.Provider>
      );
  }
}

function initialState(search: string): Boot {
  const params = new URLSearchParams(search);
  const error = params.get("auth_error");
  if (error !== null) {
    const known = authErrors.find((code) => code === error);
    return { kind: "signedOut", error: known ?? "login_failed" };
  }
  if (params.has("signed_out")) {
    return { kind: "signedOut", error: null };
  }
  return { kind: "loading" };
}

function Loading() {
  const { t } = useTranslation();
  return (
    <p className="boot" role="status">
      {t("app.loading")}
    </p>
  );
}

function SignedOut({ error }: { error: AuthError | null }) {
  const { t } = useTranslation();
  return (
    <main className="boot">
      <h1 className="boot-title">{t("app.name")}</h1>
      <p role={error ? "alert" : undefined}>
        {error ? t(`auth.errors.${error}`) : t("auth.signedOut")}
      </p>
      {/* A plain link: /auth/login is a server route, not a page of the app. */}
      <a className="button button-primary" href="/auth/login">
        {t("auth.signIn")}
      </a>
    </main>
  );
}

function Failed({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation();
  return (
    <main className="boot">
      <p role="alert">{t("auth.loadFailed")}</p>
      <button type="button" className="button" onClick={onRetry}>
        {t("auth.retry")}
      </button>
    </main>
  );
}
