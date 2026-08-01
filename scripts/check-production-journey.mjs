const baseURL = process.env.HUBCHAT_JOURNEY_BASE_URL;

if (!baseURL) {
  console.error("HUBCHAT_JOURNEY_BASE_URL is required, for example http://127.0.0.1:18080");
  process.exit(1);
}

const origin = new URL(baseURL).origin;
const suffix = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
const email = `journey-${suffix}@example.com`;
const password = "journey-password-2026!";
const cookies = new Map();
let requestNumber = 0;

function rememberCookies(response) {
  const setCookies = typeof response.headers.getSetCookie === "function"
    ? response.headers.getSetCookie()
    : (response.headers.get("set-cookie") ?? "").split(/,(?=[^;]+=[^;]+)/);
  for (const value of setCookies) {
    const pair = value.split(";", 1)[0];
    const separator = pair.indexOf("=");
    if (separator > 0) cookies.set(pair.slice(0, separator), pair.slice(separator + 1));
  }
}

function cookieHeader() {
  return [...cookies].map(([name, value]) => `${name}=${value}`).join("; ");
}

async function request(path, { method = "GET", body, expected = 200 } = {}) {
  const headers = new Headers({ Accept: "application/json" });
  if (body !== undefined) headers.set("Content-Type", "application/json");
  if (cookieHeader()) headers.set("Cookie", cookieHeader());
  // Public widget calls need this header for the widget-domain allowlist;
  // cookie-authenticated calls need it for the server's CSRF origin check.
  if (method !== "GET") headers.set("Origin", origin);
  if (method !== "GET") headers.set("X-Idempotency-Key", `production-journey-${++requestNumber}`);

  const response = await fetch(new URL(path, baseURL), {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  rememberCookies(response);
  const text = await response.text();
  let value = null;
  if (text) {
    try {
      value = JSON.parse(text);
    } catch {
      value = text;
    }
  }
  if (response.status !== expected) {
    throw new Error(`${method} ${path}: status ${response.status}, expected ${expected}: ${text.slice(0, 1200)}`);
  }
  return value;
}

function requireValue(value, label) {
  if (typeof value !== "string" || value.length === 0) throw new Error(`${label} was missing`);
  return value;
}

function log(label) {
  console.log(`OK ${label}`);
}

const setup = await request("/api/v1/setup/state");
if (setup?.migrations_ready !== true) throw new Error("setup state reported migrations are not ready");
log("setup state");

await request("/api/v1/auth/signup", {
  method: "POST",
  body: { name: "Production Journey Owner", email, password },
  expected: 201,
});
log("owner signup and session");

const workspace = await request("/api/v1/workspaces", {
  method: "POST",
  body: { name: "Production Journey Workspace", slug: `journey-${suffix}` },
  expected: 201,
});
const workspaceID = requireValue(workspace?.id, "workspace id");
log("workspace bootstrap");

const bootstrap = await request("/api/v1/bootstrap");
if (bootstrap?.workspace?.id !== workspaceID || !Array.isArray(bootstrap?.inboxes) || bootstrap.inboxes.length === 0) {
  throw new Error("bootstrap did not return the created workspace and its default inbox");
}
const inboxID = requireValue(bootstrap.inboxes[0].id, "default inbox id");
log("authenticated bootstrap");

const portal = await request("/api/v1/portals", {
  method: "POST",
  body: { name: "Production journey portal", subdomain: `journey-${suffix}`, default_inbox_id: inboxID },
  expected: 201,
});
const portalID = requireValue(portal?.id, "portal id");
const portalBootstrap = await request(`/api/v1/portal/bootstrap?portal=${encodeURIComponent(portalID)}`);
if (portalBootstrap?.portal?.id !== portalID || portalBootstrap?.portal?.workspace_id !== workspaceID) {
  throw new Error("public portal bootstrap did not return the created portal and workspace");
}
log("portal creation and public bootstrap");

const widget = await request("/api/v1/widgets", {
  method: "POST",
  body: { name: "Production journey widget", inbox_id: inboxID },
  expected: 201,
});
const widgetID = requireValue(widget?.id, "widget id");
const publicKey = requireValue(widget?.public_key, "widget public key");
log("widget installation");

await request(`/api/v1/widgets/${widgetID}/domains`, {
  method: "POST",
  body: { domain: "127.0.0.1" },
  expected: 201,
});
const widgetURL = `${origin}/journey-page`;
const config = await request(`/api/v1/widget/config?key=${encodeURIComponent(publicKey)}&url=${encodeURIComponent(widgetURL)}`, {
});
if (config?.enabled !== true || !Array.isArray(config?.modes) || !config?.appearance) {
  throw new Error("widget config was not enabled or did not include its public appearance");
}
log("widget public config");

const visitor = await request("/api/v1/widget/visitors", {
  method: "POST",
  body: { public_key: publicKey, url: widgetURL },
  expected: 201,
});
const visitorToken = requireValue(visitor?.token, "visitor token");
const visitorBody = { public_key: publicKey, url: widgetURL, token: visitorToken };

const identified = await request("/api/v1/widget/identify", {
  method: "POST",
  body: { ...visitorBody, name: "Journey Customer", email: `customer-${suffix}@example.com`, external_id: `journey-customer-${suffix}` },
});
const customerID = requireValue(identified?.customer?.id, "identified customer id");
log("anonymous visitor identity");

const feedbackBoard = await request("/api/v1/feedback/boards", {
  method: "POST",
  body: {
    name: "Production journey feedback",
    slug: `journey-feedback-${suffix}`,
    description: "Feedback created by the release journey.",
    visibility: "public",
    allow_comments: true,
    allow_voting: true,
    moderation: false,
    position: 0,
  },
  expected: 201,
});
const feedbackSlug = requireValue(feedbackBoard?.slug, "feedback board slug");
const feedbackItem = await request(`/api/v1/widget/feedback/boards/${encodeURIComponent(feedbackSlug)}/items`, {
  method: "POST",
  body: { ...visitorBody, title: "Production journey feedback", description: "The widget feedback path is working.", type: "feature_request" },
  expected: 201,
});
const feedbackItemID = requireValue(feedbackItem?.item?.id, "feedback item id");
const feedbackToken = feedbackItem?.token || visitorToken;
await request(`/api/v1/widget/feedback/items/${feedbackItemID}/votes`, {
  method: "POST",
  body: { public_key: publicKey, url: widgetURL, token: feedbackToken },
  expected: 201,
});
log("feedback submission and vote");

const started = await request("/api/v1/widget/conversations", {
  method: "POST",
  body: { ...visitorBody, body: "I need help from the production widget." },
  expected: 201,
});
const conversationID = requireValue(started?.conversation_id, "widget conversation id");
const tokenAfterStart = started?.token || visitorToken;
const conversationBody = { ...visitorBody, token: tokenAfterStart };

const posted = await request(`/api/v1/widget/conversations/${conversationID}/messages`, {
  method: "POST",
  body: { ...conversationBody, body: "The follow-up message should be visible to the agent." },
  expected: 201,
});
if (!posted?.id || posted.author_type !== "customer") throw new Error("widget reply was not created as a customer message");
log("visitor conversation and reply");

const ticket = await request(`/api/v1/conversations/${conversationID}/ticket`, {
  method: "POST",
  body: { title: "Production journey request", description: "Created by the release journey.", priority: "normal", customer_id: customerID },
  expected: 201,
});
if (!ticket?.id || ticket.conversation_id !== conversationID) throw new Error("ticket conversion did not preserve the conversation link");
log("conversation to ticket conversion");

const exportPreview = await request("/api/v1/portability/exports/preview", {
  method: "POST",
  body: { kind: "workspace", scope: {} },
});
if (!Array.isArray(exportPreview?.tables) || typeof exportPreview?.row_count !== "number") {
  throw new Error("workspace export preview did not return table summaries");
}
log("workspace export preview");

console.log("Production HTTP journey OK (setup, widget, visitor identity, conversation, ticket conversion, export preview)");
