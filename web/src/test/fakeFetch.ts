// A fake of the server's HTTP interface for tests: routes answer with
// canned responses, and every request is recorded with its body, so tests
// can check what the client sent (for instance, that a retry repeats the
// same id).
import { createApi, type Api } from "../api/client";

export interface Recorded {
  method: string;
  path: string;
  query: URLSearchParams;
  body: unknown;
  contentType: string | null;
}

type Reply = Response | Error | ((request: Recorded) => Response | Error);

export const origin = "http://app.test";

export class FakeServer {
  readonly requests: Recorded[] = [];
  private routes: { method: string; path: RegExp; replies: Reply[] }[] = [];
  unauthenticated = 0;

  /**
   * Answers matching requests with the replies in order; the last one
   * repeats. An Error makes fetch reject, as a network failure does.
   */
  on(method: string, path: RegExp | string, ...replies: Reply[]): this {
    const pattern =
      typeof path === "string"
        ? new RegExp(`^${path.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}$`)
        : path;
    this.routes.unshift({ method, path: pattern, replies });
    return this;
  }

  api(): Api {
    return createApi({
      origin,
      fetch: (request) => this.handle(request),
      onUnauthenticated: () => {
        this.unauthenticated++;
      },
    });
  }

  /** The requests to one method and path. */
  calls(method: string, path: string): Recorded[] {
    return this.requests.filter((r) => r.method === method && r.path === path);
  }

  private async handle(request: Request): Promise<Response> {
    const url = new URL(request.url);
    const path = url.pathname.replace(/^\/api\/v1/, "");
    const text = await request.text();
    const recorded: Recorded = {
      method: request.method,
      path,
      query: url.searchParams,
      body: text === "" ? undefined : (JSON.parse(text) as unknown),
      contentType: request.headers.get("Content-Type"),
    };
    this.requests.push(recorded);
    const route = this.routes.find((r) => r.method === request.method && r.path.test(path));
    if (route === undefined) {
      return problem(404, "not_found");
    }
    const reply = route.replies.length > 1 ? route.replies.shift() : route.replies[0];
    const value = typeof reply === "function" ? reply(recorded) : reply;
    if (value instanceof Error) {
      throw value;
    }
    if (value === undefined) {
      throw new Error("route without replies");
    }
    return value.clone();
  }
}

export function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

export function noContent(): Response {
  return new Response(null, { status: 204 });
}

export function problem(status: number, code: string): Response {
  return new Response(JSON.stringify({ type: "about:blank", title: "x", status, code }), {
    status,
    headers: { "Content-Type": "application/problem+json" },
  });
}

/** What fetch throws when the network fails. */
export function networkError(): TypeError {
  return new TypeError("Failed to fetch");
}
