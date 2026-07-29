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
};

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
): Promise<WireMessage> {
  return post(host, `/conversations/${conversationId}/messages`, { public_key: publicKey, url: location.href, token, body });
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
  payload: { name?: string; email?: string; external_id?: string; signed_token?: string },
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
