/**
 * The widget's own hand-rolled API client.
 *
 * Deliberately not @hubchat/shared's `client.ts` — that layer assumes cookie
 * auth and the dashboard's origin. The widget runs on someone else's domain,
 * authenticates with a bearer-style visitor token it manages itself, and has
 * to stay tiny (the bundle-budget rule in docs/frontend.md), so it talks to
 * `/api/v1/widget/*` with plain `fetch`.
 */

export type WireMessage = {
  id: string;
  kind: string;
  author_type: string;
  author_name: string;
  body: string;
  sequence: number;
  created_at: string;
  attachments?: { id: string; name: string; mime_type?: string; size_bytes?: number; url: string }[];
};

export type WidgetForm = {
  id: string;
  name: string;
  slug: string;
  description?: string | null;
  purpose: string;
  fields: {
    key: string;
    label: string;
    type: string;
    placeholder?: string | null;
    description?: string | null;
    options?: string[];
    required: boolean;
    condition?: Record<string, unknown>;
  }[];
  confirmation?: Record<string, unknown>;
};

export type WidgetFeedbackBoard = { id: string; name: string; slug: string; description?: string; allow_voting: boolean };
export type WidgetFeedbackItem = { id: string; title: string; description: string; status: string; vote_count: number; viewer_has_voted: boolean; viewer_subscribed: boolean };
export type WidgetArticle = { slug: string; title: string; excerpt: string; body?: string; language?: string; helpful_count?: number; unhelpful_count?: number };

export function isRTL(language: string): boolean {
  const base = language.trim().toLowerCase().replaceAll("_", "-").split("-", 1)[0] ?? "";
  return ["ar", "dv", "fa", "he", "ku", "ps", "ur", "yi"].includes(base);
}

function preferredLanguage(): string {
  return typeof navigator !== "undefined" ? (navigator.language || "").trim().toLowerCase().replaceAll("_", "-") : "";
}

export type StartConversationResponse = {
  conversation_id: string;
  token: string;
  message: WireMessage;
};

function endpoint(host: string, path: string): string {
  return `${host}/api/v1/widget${path}`;
}

async function parse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    let message = `Request failed (${response.status})`;
    try {
      const body = (await response.json()) as { error?: { message?: string } };
      if (body.error?.message) message = body.error.message;
    } catch {
      /* non-JSON error body — keep the generic message */
    }
    throw new Error(message);
  }
  return response.json() as Promise<T>;
}

async function post<T>(host: string, path: string, body: Record<string, unknown>): Promise<T> {
  const response = await fetch(endpoint(host, path), {
    method: "POST",
    credentials: "omit",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return parse<T>(response);
}

export function issueVisitor(host: string, publicKey: string): Promise<{ token: string; visitor_id: string }> {
  return post(host, "/visitors", { public_key: publicKey, url: location.href });
}

export function startConversation(
  host: string,
  publicKey: string,
  token: string,
  body: string,
): Promise<StartConversationResponse> {
  return post(host, "/conversations", { public_key: publicKey, url: location.href, token, body });
}

export function postMessage(
  host: string,
  publicKey: string,
  token: string,
  conversationId: string,
  body: string,
  fileIDs: string[] = [],
): Promise<WireMessage> {
  return post(host, `/conversations/${conversationId}/messages`, { public_key: publicKey, url: location.href, token, body, file_ids: fileIDs });
}

export type WidgetFormsPage = { data: WidgetForm[]; next_cursor: string | null; has_more: boolean };

export async function listForms(host: string, publicKey: string, cursor?: string | null): Promise<WidgetFormsPage> {
  const params = new URLSearchParams({ key: publicKey, url: location.href, limit: "25" });
  if (cursor) params.set("cursor", cursor);
  const response = await fetch(`${endpoint(host, "/forms")}?${params.toString()}`, { credentials: "omit" });
  return parse<WidgetFormsPage>(response);
}

export function submitForm(
  host: string,
  publicKey: string,
  token: string | null,
  slug: string,
  values: Record<string, unknown>,
  fileIDs: Record<string, string> = {},
): Promise<{ id: string; status: string; token?: string }> {
  return post(host, `/forms/${encodeURIComponent(slug)}/submissions`, {
    public_key: publicKey, url: location.href, token: token ?? "", values, file_ids: fileIDs,
  });
}

export async function uploadFormFile(
  host: string,
  publicKey: string,
  token: string,
  slug: string,
  file: File,
): Promise<{ id: string; name: string; mime_type?: string; size_bytes?: number; token?: string }> {
  const body = new FormData();
  body.append("file", file);
  body.append("public_key", publicKey);
  body.append("url", location.href);
  body.append("token", token);
  const params = new URLSearchParams({ key: publicKey, url: location.href, token });
  const response = await fetch(`${endpoint(host, `/forms/${encodeURIComponent(slug)}/files`)}?${params.toString()}`, {
    method: "POST", credentials: "omit", body,
  });
  return parse(response);
}

export type WidgetFeedbackBoardsPage = { data: WidgetFeedbackBoard[]; next_cursor: string | null; has_more: boolean };

export async function listFeedbackBoards(host: string, publicKey: string, cursor?: string | null): Promise<WidgetFeedbackBoardsPage> {
  const params = new URLSearchParams({ key: publicKey, url: location.href, limit: "25" });
  if (cursor) params.set("cursor", cursor);
  const response = await fetch(`${endpoint(host, "/feedback/boards")}?${params.toString()}`, { credentials: "omit" });
  return parse<WidgetFeedbackBoardsPage>(response);
}

export type WidgetFeedbackItemsPage = { data: WidgetFeedbackItem[]; next_cursor: string | null; has_more: boolean };

export async function listFeedbackItems(host: string, publicKey: string, token: string | null, slug: string, cursor?: string | null): Promise<WidgetFeedbackItemsPage> {
  const params = new URLSearchParams({ key: publicKey, url: location.href, token: token ?? "", limit: "25" });
  if (cursor) params.set("cursor", cursor);
  const response = await fetch(`${endpoint(host, `/feedback/boards/${encodeURIComponent(slug)}/items`)}?${params.toString()}`, { credentials: "omit" });
  return parse<WidgetFeedbackItemsPage>(response);
}

export function createFeedbackItem(host: string, publicKey: string, token: string | null, slug: string, title: string, description: string): Promise<{ item: WidgetFeedbackItem; token?: string }> {
  return post(host, `/feedback/boards/${encodeURIComponent(slug)}/items`, { public_key: publicKey, url: location.href, token: token ?? "", title, description, type: "feature_request" });
}

export function voteFeedbackItem(host: string, publicKey: string, token: string, itemID: string): Promise<{ status: string }> {
  return post(host, `/feedback/items/${encodeURIComponent(itemID)}/votes`, { public_key: publicKey, url: location.href, token });
}

export function subscribeFeedbackItem(host: string, publicKey: string, token: string, itemID: string): Promise<{ subscribed: boolean }> {
  return post(host, `/feedback/items/${encodeURIComponent(itemID)}/subscription`, { public_key: publicKey, url: location.href, token });
}

export async function unsubscribeFeedbackItem(host: string, publicKey: string, token: string, itemID: string): Promise<{ subscribed: boolean }> {
  const response = await fetch(endpoint(host, `/feedback/items/${encodeURIComponent(itemID)}/subscription`), {
    method: "DELETE",
    credentials: "omit",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ public_key: publicKey, url: location.href, token }),
  });
  return parse<{ subscribed: boolean }>(response);
}

export type WidgetArticlesPage = { data: WidgetArticle[]; next_cursor: string | null; has_more: boolean };

export async function searchArticles(host: string, publicKey: string, query: string, cursor?: string | null): Promise<WidgetArticlesPage> {
  const params = new URLSearchParams({ key: publicKey, url: location.href, language: preferredLanguage(), limit: "20" });
  if (query.trim()) params.set("q", query.trim());
  if (cursor) params.set("cursor", cursor);
  const response = await fetch(`${endpoint(host, "/articles")}?${params.toString()}`, { credentials: "omit" });
  return parse<WidgetArticlesPage>(response);
}

export function getArticle(host: string, publicKey: string, slug: string): Promise<WidgetArticle> {
  const params = new URLSearchParams({ key: publicKey, url: location.href, language: preferredLanguage() });
  return fetch(`${endpoint(host, `/articles/${encodeURIComponent(slug)}`)}?${params.toString()}`, { credentials: "omit" }).then(parse<WidgetArticle>);
}

export function submitArticleFeedback(host: string, publicKey: string, slug: string, helpful: boolean, comment: string): Promise<{ status: string }> {
  return post(host, `/articles/${encodeURIComponent(slug)}/feedback`, { public_key: publicKey, url: location.href, helpful, comment, language: preferredLanguage() });
}

export async function uploadFile(
  host: string,
  publicKey: string,
  token: string,
  conversationId: string,
  messageId: string,
  file: File,
): Promise<{ id: string; name: string; mime_type?: string; size_bytes?: number; url?: string }> {
  const body = new FormData();
  body.append("file", file);
  body.append("public_key", publicKey);
  body.append("url", location.href);
  body.append("token", token);
  body.append("message_id", messageId);
  const params = new URLSearchParams({ key: publicKey, url: location.href, token });
  const response = await fetch(`${endpoint(host, `/conversations/${conversationId}/files`)}?${params.toString()}`, { method: "POST", credentials: "omit", body });
  return parse(response);
}

export async function listMessages(
  host: string,
  publicKey: string,
  token: string,
  conversationId: string,
  after = 0,
): Promise<WireMessage[]> {
  const params = new URLSearchParams({ key: publicKey, url: location.href, token, after: String(after) });
  const response = await fetch(`${endpoint(host, `/conversations/${conversationId}/messages`)}?${params.toString()}`, {
    credentials: "omit",
  });
  const parsed = await parse<{ data: WireMessage[] }>(response);
  return parsed.data;
}

export function identify(
  host: string,
  publicKey: string,
  token: string,
  payload: { name?: string; email?: string; external_id?: string; signed_token?: string; attributes?: Record<string, unknown> },
): Promise<{ customer: { id: string; name: string | null; email: string | null } }> {
  return post(host, "/identify", { public_key: publicKey, url: location.href, token, ...payload });
}

export function track(
  host: string,
  publicKey: string,
  token: string,
  type: string,
  payload: Record<string, unknown>,
): Promise<void> {
  return post(host, "/events", { public_key: publicKey, url: location.href, token, type, page_url: location.href, payload });
}

/* --------------------------------------------------------------- session */

// Keyed by public key so a page embedding more than one widget (production
// and test, say) keeps separate visitor identities and conversations rather
// than one clobbering the other's storage.
function storageKey(publicKey: string): string {
  return `hubchat.visitor.${publicKey}`;
}

export type StoredSession = { token: string; conversationId: string | null };

export function loadSession(publicKey: string): StoredSession | null {
  try {
    const raw = localStorage.getItem(storageKey(publicKey));
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Partial<StoredSession>;
    if (typeof parsed.token !== "string") return null;
    return { token: parsed.token, conversationId: typeof parsed.conversationId === "string" ? parsed.conversationId : null };
  } catch {
    return null;
  }
}

export function saveSession(publicKey: string, session: StoredSession): void {
  try {
    localStorage.setItem(storageKey(publicKey), JSON.stringify(session));
  } catch {
    /* storage unavailable (private browsing, quota) — the session just does not persist */
  }
}

export function clearSession(publicKey: string): void {
  try {
    localStorage.removeItem(storageKey(publicKey));
  } catch {
    /* nothing to do */
  }
}
