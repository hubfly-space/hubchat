/**
 * Typed client for the live Hubchat API.
 *
 * This is real — it calls the actual Go server (internal/api), not a mock.
 * Cookies carry the session, so every call passes `credentials: "include"`
 * and the dev proxy in vite.config.ts forwards /api and /ws to the Go server
 * on :8080.
 *
 * Scope note: this compatibility client is used by the explicit live-demo
 * route and older core screens. New production pages should use the shared
 * `@hubchat/shared` client/query layer so cancellation, retries, cache
 * invalidation, pagination, and request diagnostics behave consistently.
 */

const API_BASE = "/api/v1";

export class ApiError extends Error {
  code: string;
  requestId: string;
  status: number;

  constructor(status: number, code: string, message: string, requestId: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.requestId = requestId;
  }
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  opts: { workspaceId?: string; idempotencyKey?: string } = {},
): Promise<T> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (opts.workspaceId) headers["Hubchat-Workspace-Id"] = opts.workspaceId;
  if (opts.idempotencyKey) headers["Idempotency-Key"] = opts.idempotencyKey;

  const response = await fetch(`${API_BASE}${path}`, {
    method,
    headers,
    credentials: "include",
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });

  if (response.status === 204) return undefined as T;

  const payload = await response.json().catch(() => null);

  if (!response.ok) {
    const error = payload?.error;
    throw new ApiError(
      response.status,
      error?.code ?? "unknown_error",
      error?.message ?? `Request failed with status ${response.status}`,
      error?.request_id ?? "",
    );
  }

  return payload as T;
}

// ---------------------------------------------------------------- auth

export type AuthUser = { id: string; name: string; email: string };

export function signUp(name: string, email: string, password: string) {
  return request<AuthUser>("POST", "/auth/signup", { name, email, password });
}

export function logIn(email: string, password: string) {
  return request<AuthUser>("POST", "/auth/login", { email, password });
}

export function logOut() {
  return request<void>("POST", "/auth/logout");
}

export type Me = {
  member_id: string;
  role: string;
  workspace: { id: string; name: string; slug: string };
};

export function me(workspaceId?: string) {
  return request<Me>("GET", "/auth/me", undefined, { workspaceId });
}

// ----------------------------------------------------------- workspaces

export type WorkspaceSummary = { id: string; name: string; slug: string };

export function createWorkspace(name: string, slug: string) {
  return request<WorkspaceSummary>("POST", "/workspaces", { name, slug });
}

export type Inbox = { id: string; name: string; slug: string; is_default: boolean };

export function listInboxes(workspaceId: string) {
  return request<{ data: Inbox[] }>("GET", "/inboxes", undefined, { workspaceId });
}

// --------------------------------------------------------- conversations

export type ApiConversation = {
  id: string;
  workspace_id: string;
  inbox_id: string;
  channel: string;
  subject: string | null;
  state: string;
  priority: string;
  customer_id: string | null;
  assignee_id: string | null;
  team_id: string | null;
  message_count: number;
  last_message_preview: string;
  last_message_at: string;
  created_at: string;
};

export type ApiMessage = {
  id: string;
  client_id: string | null;
  conversation_id: string;
  kind: "reply" | "note" | "event";
  author_type: "customer" | "agent" | "system" | "automation";
  author_id: string | null;
  author_name: string;
  body: string;
  sequence: number;
  created_at: string;
};

export function listConversations(workspaceId: string, inboxId: string, limit = 50) {
  return request<{ data: ApiConversation[] }>(
    "GET",
    `/inboxes/${inboxId}/conversations?limit=${limit}`,
    undefined,
    { workspaceId },
  );
}

export function getConversation(workspaceId: string, conversationId: string) {
  return request<ApiConversation>("GET", `/conversations/${conversationId}`, undefined, { workspaceId });
}

export function listMessages(workspaceId: string, conversationId: string, afterSequence = 0) {
  return request<{ data: ApiMessage[] }>(
    "GET",
    `/conversations/${conversationId}/messages?after=${afterSequence}`,
    undefined,
    { workspaceId },
  );
}

export function startConversation(
  workspaceId: string,
  params: {
    inboxId: string;
    channel: string;
    subject?: string | null;
    customerId?: string | null;
    authorName: string;
    body: string;
  },
) {
  return request<{ conversation: ApiConversation; message: ApiMessage }>(
    "POST",
    "/conversations",
    {
      inbox_id: params.inboxId,
      channel: params.channel,
      subject: params.subject ?? null,
      customer_id: params.customerId ?? null,
      author_name: params.authorName,
      body: params.body,
    },
    { workspaceId },
  );
}

/**
 * Sends a reply or internal note. `idempotencyKey` should be a
 * client-generated id kept stable across retries of the *same* send (§9) —
 * generate one when the user hits "send", not on every network attempt.
 */
export function postMessage(
  workspaceId: string,
  conversationId: string,
  params: { kind: "reply" | "note"; authorName: string; body: string; idempotencyKey?: string },
) {
  return request<ApiMessage>(
    "POST",
    `/conversations/${conversationId}/messages`,
    { kind: params.kind, author_name: params.authorName, body: params.body },
    { workspaceId, idempotencyKey: params.idempotencyKey },
  );
}

// ------------------------------------------------------------- realtime

export type RealtimeEnvelope<T = unknown> = {
  id: string;
  type: string;
  workspace_id: string;
  occurred_at: string;
  sequence: number;
  data: T;
};

/**
 * Opens the realtime socket for one workspace. Reconnection, resume-from-
 * sequence, and heartbeats beyond the browser's own ping/pong handling are not
 * implemented here yet — this is the minimal client that matches what
 * internal/realtime currently sends (§9 describes the fuller contract this
 * will grow into).
 */
export function connectRealtime(
  workspaceId: string,
  onEvent: (envelope: RealtimeEnvelope) => void,
  onClose?: (event: CloseEvent) => void,
): () => void {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const socket = new WebSocket(`${protocol}//${window.location.host}/ws/conversations?workspace_id=${workspaceId}`);

  socket.onmessage = (event) => {
    try {
      onEvent(JSON.parse(event.data) as RealtimeEnvelope);
    } catch {
      // Malformed frame — ignore rather than crash the whole connection.
    }
  };
  socket.onclose = (event) => onClose?.(event);

  return () => socket.close();
}
