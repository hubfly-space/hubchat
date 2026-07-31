/**
 * The HTTP client every surface talks to the Go API through.
 *
 * Three things it does that a bare `fetch` does not, in order of how much they
 * matter:
 *
 *   1. **Retry.** Network failures and 5xx responses are retried with
 *      exponential backoff and full jitter, honouring `Retry-After`. Only for
 *      requests that are safe to repeat — see `isRetryable`.
 *   2. **Dedup.** Identical in-flight GETs share one request. Three components
 *      mounting at once and all asking for the same inbox is normal; three
 *      round trips for it is not.
 *   3. **One error type.** Every failure surfaces as `ApiError` carrying the
 *      server's stable `code` and the `request_id`, so a support report can be
 *      traced to a log line.
 */

/** The error envelope §16 specifies. Every failure has this shape. */
export type ApiErrorBody = {
  code: string;
  message: string;
  request_id: string;
  details?: Record<string, unknown>;
};

export class ApiError extends Error {
  override readonly name = "ApiError";
  readonly code: string;
  readonly status: number;
  readonly requestId: string;
  readonly details?: Record<string, unknown>;
  /**
   * How long the server asked us to wait, from `Retry-After`. Present on 429
   * and on 503s during a restart; overrides our own backoff calculation.
   */
  readonly retryAfterMs?: number;

  constructor(status: number, body: ApiErrorBody & { retryAfterMs?: number }) {
    super(body.message);
    this.status = status;
    this.code = body.code;
    this.requestId = body.request_id;
    this.details = body.details;
    this.retryAfterMs = body.retryAfterMs;
  }

  /**
   * True when the session is gone. Callers redirect to sign-in rather than
   * showing an error, because "you were signed out" is not a failure the user
   * can act on by retrying.
   */
  get isUnauthorized() {
    return this.status === 401;
  }

  /** True when the caller may not do this. Distinct from unauthorized: signing in again will not help. */
  get isForbidden() {
    return this.status === 403;
  }

  get isNotFound() {
    return this.status === 404;
  }

  /** Field-level validation problems, keyed by field name. */
  get fieldErrors(): Record<string, string> {
    const details = this.details ?? {};
    const errors: Record<string, string> = {};
    for (const [field, message] of Object.entries(details)) {
      if (typeof message === "string") errors[field] = message;
    }
    return errors;
  }
}

/** Raised when a request is cancelled, so callers can ignore it rather than render an error. */
export class AbortedError extends Error {
  override readonly name = "AbortedError";
  constructor() {
    super("Request was cancelled");
  }
}

export type RequestOptions = {
  /** Workspace scope. Sent as the `Hubchat-Workspace-Id` header. */
  workspaceId?: string;
  /**
   * Makes a write safe to retry (§16). Generate one when the user commits to
   * the action, not per network attempt — the point is that every attempt of
   * the *same* action carries the *same* key.
   */
  idempotencyKey?: string;
  signal?: AbortSignal;
  /** Skips the in-flight dedup cache. For polls that must actually hit the server. */
  fresh?: boolean;
  /** Overrides the default attempt count. 1 disables retry. */
  attempts?: number;
};

type Method = "GET" | "POST" | "PATCH" | "PUT" | "DELETE";

const API_BASE = "/api/v1";

/**
 * Requests currently in flight, keyed by method+path+body.
 *
 * Only GETs are shared. Two identical POSTs are two intended writes unless the
 * caller said otherwise with an idempotency key, and silently collapsing them
 * would be the client deciding something the user did not.
 */
const inFlight = new Map<string, Promise<unknown>>();

export async function request<T>(
  method: Method,
  path: string,
  body?: unknown,
  options: RequestOptions = {},
): Promise<T> {
  const key = `${method} ${path} ${body === undefined ? "" : JSON.stringify(body)}`;

  const shareable = method === "GET" && !options.fresh;
  if (shareable) {
    const existing = inFlight.get(key) as Promise<T> | undefined;
    if (existing) return existing;
  }

  const promise = execute<T>(method, path, body, options);

  if (shareable) {
    inFlight.set(key, promise);
    // Clear on settle either way: a failed request must not poison the slot
    // for the retry that follows it.
    promise.finally(() => {
      if (inFlight.get(key) === promise) inFlight.delete(key);
    });
  }

  return promise;
}

async function execute<T>(
  method: Method,
  path: string,
  body: unknown,
  options: RequestOptions,
): Promise<T> {
  const maxAttempts = options.attempts ?? (isRetryableMethod(method, options) ? 4 : 1);

  let lastError: unknown;

  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    try {
      return await send<T>(method, path, body, options);
    } catch (error) {
      lastError = error;

      if (error instanceof AbortedError) throw error;
      if (attempt === maxAttempts) break;
      if (!isRetryable(error)) break;

      const delay = backoffDelay(attempt, error);
      await sleep(delay, options.signal);
    }
  }

  throw lastError;
}

async function send<T>(
  method: Method,
  path: string,
  body: unknown,
  options: RequestOptions,
): Promise<T> {
	const headers: Record<string, string> = {};
	const multipart = typeof FormData !== "undefined" && body instanceof FormData;
	if (body !== undefined && !multipart) headers["Content-Type"] = "application/json";
  if (options.workspaceId) headers["Hubchat-Workspace-Id"] = options.workspaceId;
  if (options.idempotencyKey) headers["Idempotency-Key"] = options.idempotencyKey;

  let response: Response;
  try {
    response = await fetch(`${API_BASE}${path}`, {
      method,
      headers,
      // The session is a cookie, so every call must send it. The server's CSRF
      // check is what makes that safe.
      credentials: "include",
		body: body === undefined ? undefined : multipart ? body : JSON.stringify(body),
      signal: options.signal,
    });
  } catch (error) {
    if (isAbort(error)) throw new AbortedError();
    // A fetch rejection is a transport failure — offline, DNS, connection
    // reset. Worth retrying, unlike anything the server actually answered.
    throw new NetworkError(error);
  }

  if (response.status === 204) return undefined as T;

  const payload = await response.json().catch(() => null);

  if (!response.ok) {
    throw new ApiError(response.status, {
      code: payload?.error?.code ?? "unknown_error",
      message: payload?.error?.message ?? `Request failed with status ${response.status}`,
      request_id: payload?.error?.request_id ?? "",
      details: payload?.error?.details,
      ...retryAfterOf(response),
    });
  }

  return payload as T;
}

/** A transport-level failure, before the server said anything. */
export class NetworkError extends Error {
  override readonly name = "NetworkError";
  override readonly cause: unknown;
  constructor(cause: unknown) {
    super("The server could not be reached");
    this.cause = cause;
  }
}

/**
 * Whether repeating this request is safe.
 *
 * GET is safe by definition. A write is safe only when it carries an
 * idempotency key, because that is precisely the promise the key makes: the
 * server will recognise the retry and not do the work twice. Retrying an
 * unkeyed POST would be the client gambling that a timeout meant the server
 * never received it — and a timeout means no such thing.
 */
function isRetryableMethod(method: Method, options: RequestOptions): boolean {
  if (method === "GET") return true;
  return Boolean(options.idempotencyKey);
}

/** Whether this particular failure is worth another attempt. */
function isRetryable(error: unknown): boolean {
  if (error instanceof NetworkError) return true;
  if (!(error instanceof ApiError)) return false;

  // 429 and 5xx are transient by definition. 4xx otherwise means the request
  // was wrong, and sending it again unchanged will be wrong again.
  if (error.status === 429) return true;
  return error.status >= 500 && error.status < 600;
}

/**
 * Exponential backoff with full jitter, capped.
 *
 * Full jitter — a random point in `[0, delay]` rather than `delay ± noise` —
 * because the failure being recovered from is usually shared. When a server
 * restarts, every open tab fails at the same instant; without jitter they all
 * retry at the same instant too, and the first thing the recovering server
 * sees is the same stampede that is still failing.
 *
 * `Retry-After` overrides the calculation entirely: the server knows better
 * than we do, and ignoring it is how a client gets itself rate-limited harder.
 */
function backoffDelay(attempt: number, error: unknown): number {
  if (error instanceof ApiError && error.retryAfterMs) {
    return error.retryAfterMs;
  }

  const base = 300;
  const cap = 8_000;
  const exponential = Math.min(cap, base * 2 ** (attempt - 1));
  return Math.random() * exponential;
}

function retryAfterOf(response: Response): { retryAfterMs?: number } {
  const header = response.headers.get("Retry-After");
  if (!header) return {};

  const seconds = Number(header);
  if (Number.isFinite(seconds)) return { retryAfterMs: seconds * 1000 };

  const date = Date.parse(header);
  if (Number.isFinite(date)) return { retryAfterMs: Math.max(0, date - Date.now()) };

  return {};
}

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new AbortedError());
      return;
    }
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    function onAbort() {
      clearTimeout(timer);
      reject(new AbortedError());
    }
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

function isAbort(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

/** Convenience wrappers. Every module's client is built from these. */
export const api = {
  get: <T>(path: string, options?: RequestOptions) => request<T>("GET", path, undefined, options),
  post: <T>(path: string, body?: unknown, options?: RequestOptions) =>
    request<T>("POST", path, body, options),
  patch: <T>(path: string, body?: unknown, options?: RequestOptions) =>
    request<T>("PATCH", path, body, options),
  put: <T>(path: string, body?: unknown, options?: RequestOptions) =>
    request<T>("PUT", path, body, options),
  delete: <T>(path: string, options?: RequestOptions) =>
    request<T>("DELETE", path, undefined, options),
};

/**
 * Generates an idempotency key.
 *
 * Call once when the user commits to an action and reuse it for every retry of
 * that action — including retries the caller drives itself, such as a "try
 * again" button after a failure.
 */
export function idempotencyKey(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return `k_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 10)}`;
}
