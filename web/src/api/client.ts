// The HTTP client of the API, generated from the contract through
// openapi-fetch (SPEC section 9): paths, parameters and bodies are checked
// against api/openapi.yaml at compile time.
import createClient from "openapi-fetch";

import type { paths } from "./schema.gen";
import type { Problem, ProblemCode } from "./types";

export type Api = ReturnType<typeof createClient<paths>>;

export interface ApiOptions {
  /** Absolute origin of the server, e.g. window.location.origin. */
  origin: string;
  /** Replaces the browser's fetch; tests pass a fake. */
  fetch?: (request: Request) => Promise<Response>;
  /** Called on any 401: the session is gone, the user must log in again. */
  onUnauthenticated: () => void;
}

export function createApi(options: ApiOptions): Api {
  const client = createClient<paths>({
    baseUrl: `${options.origin}/api/v1`,
    // Read lazily: tests replace globalThis.fetch after the client exists.
    fetch: options.fetch ?? ((request: Request) => globalThis.fetch(request)),
  });
  client.use({
    onResponse({ response }) {
      if (response.status === 401) {
        options.onUnauthenticated();
      }
      return undefined;
    },
  });
  return client;
}

/** PATCH bodies are JSON Merge Patch (D-05); the server checks the type. */
export const mergePatch = { "Content-Type": "application/merge-patch+json" };

/**
 * A failed call. `network` is set when no response arrived at all (offline,
 * connection reset); the request may or may not have reached the server.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly problem: Problem | undefined;
  readonly network: boolean;

  constructor(status: number, problem: Problem | undefined, network: boolean) {
    super(network ? "network error" : `HTTP ${status}${problem ? ` ${problem.code}` : ""}`);
    this.name = "ApiError";
    this.status = status;
    this.problem = problem;
    this.network = network;
  }

  get code(): ProblemCode | undefined {
    return this.problem?.code;
  }
}

/** What every openapi-fetch call resolves to. */
interface FetchResult<T> {
  data?: T;
  error?: unknown;
  response: Response;
}

/**
 * Turns an openapi-fetch result into the data, or throws ApiError. A call
 * whose fetch throws (no response) becomes a network ApiError.
 */
export async function unwrap<T>(call: () => Promise<FetchResult<T>>): Promise<T> {
  let result: FetchResult<T>;
  try {
    result = await call();
  } catch (err) {
    if (err instanceof TypeError) {
      // fetch rejects with TypeError when the network fails.
      throw new ApiError(0, undefined, true);
    }
    throw err;
  }
  if (!result.response.ok) {
    throw new ApiError(result.response.status, asProblem(result.error), false);
  }
  // A 204 has no body; callers of such operations ignore the value.
  return result.data as T;
}

function asProblem(body: unknown): Problem | undefined {
  if (typeof body === "object" && body !== null && "code" in body) {
    return body as Problem;
  }
  return undefined;
}

/**
 * Repeats a call that failed without a response, or with a gateway error,
 * up to `attempts` times. Safe because every write is idempotent: a retry
 * sends the same body, with the same client-generated id (D-04, D-10).
 */
export async function withRetry<T>(
  call: () => Promise<T>,
  attempts = 3,
  delayMs = 500,
  sleep: (ms: number) => Promise<void> = (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
): Promise<T> {
  for (let attempt = 1; ; attempt++) {
    try {
      return await call();
    } catch (err) {
      const retryable =
        err instanceof ApiError &&
        (err.network || err.status === 502 || err.status === 503 || err.status === 504);
      if (!retryable || attempt >= attempts) {
        throw err;
      }
      await sleep(delayMs * attempt);
    }
  }
}
