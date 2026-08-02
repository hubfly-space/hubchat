import { createHash, createHmac } from "node:crypto";
import { createServer } from "node:http";
import { runBrowserJourney } from "./check-browser-journey.mjs";

const baseURL = process.env.HUBCHAT_JOURNEY_BASE_URL;

if (!baseURL) {
  console.error("HUBCHAT_JOURNEY_BASE_URL is required, for example http://127.0.0.1:18080");
  process.exit(1);
}

const origin = new URL(baseURL).origin;
const suffix = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
const email = `journey-${suffix}@example.com`;
const customerEmail = `customer-${suffix}@example.com`;
const password = "journey-password-2026!";
const cookies = new Map();
let requestNumber = 0;

let webhookProbeRequest = null;
const webhookProbe = createServer((req, res) => {
  const chunks = [];
  req.on("data", (chunk) => chunks.push(chunk));
  req.on("end", () => {
    webhookProbeRequest = { headers: req.headers, body: Buffer.concat(chunks) };
    res.writeHead(204);
    res.end();
  });
});
await new Promise((resolve, reject) => {
  webhookProbe.once("error", reject);
  webhookProbe.listen(0, "127.0.0.1", resolve);
});
webhookProbe.unref();
const webhookProbeAddress = webhookProbe.address();
if (!webhookProbeAddress || typeof webhookProbeAddress === "string") throw new Error("could not start webhook probe");
const webhookProbeURL = `http://127.0.0.1:${webhookProbeAddress.port}/delivery`;

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

async function uploadFile(path, { fields, filename, contents, contentType = "text/plain", expected = 201 }) {
  const form = new FormData();
  for (const [name, value] of Object.entries(fields)) form.set(name, value);
  form.set("file", new Blob([contents], { type: contentType }), filename);
  const headers = new Headers({ Accept: "application/json", Origin: origin });
  if (cookieHeader()) headers.set("Cookie", cookieHeader());
  headers.set("X-Idempotency-Key", `production-journey-${++requestNumber}`);
  const response = await fetch(new URL(path, baseURL), { method: "POST", headers, body: form });
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
    throw new Error(`POST ${path}: status ${response.status}, expected ${expected}: ${text.slice(0, 1200)}`);
  }
  return value;
}

async function downloadText(path, expected = 200) {
  const headers = new Headers({ Accept: "text/plain" });
  if (cookieHeader()) headers.set("Cookie", cookieHeader());
  const response = await fetch(new URL(path, baseURL), { headers });
  const text = await response.text();
  if (response.status !== expected) {
    throw new Error(`GET ${path}: status ${response.status}, expected ${expected}: ${text.slice(0, 1200)}`);
  }
  return text;
}

async function downloadBytes(path, expected = 200) {
  const headers = new Headers({ Accept: "application/octet-stream" });
  if (cookieHeader()) headers.set("Cookie", cookieHeader());
  const response = await fetch(new URL(path, baseURL), { headers });
  const bytes = Buffer.from(await response.arrayBuffer());
  if (response.status !== expected) {
    throw new Error(`GET ${path}: status ${response.status}, expected ${expected}: ${bytes.toString("utf8").slice(0, 1200)}`);
  }
  return bytes;
}

async function openVisitorSocket({ publicKey, token, pageURL, conversationID }) {
  const socketURL = new URL("/ws/visitor", baseURL);
  socketURL.protocol = socketURL.protocol === "https:" ? "wss:" : "ws:";
  socketURL.searchParams.set("key", publicKey);
  socketURL.searchParams.set("token", token);
  socketURL.searchParams.set("url", pageURL);

  const socket = new WebSocket(socketURL);
  const frames = [];
  const waiters = [];
  let opened = false;
  let closed = false;

  const rejectAll = (error) => {
    while (waiters.length > 0) waiters.shift().reject(error);
  };

  socket.addEventListener("open", () => { opened = true; });
  socket.addEventListener("message", (event) => {
    let frame;
    try {
      frame = JSON.parse(String(event.data));
    } catch {
      return;
    }
    for (let index = 0; index < waiters.length; index += 1) {
      if (!waiters[index].predicate(frame)) continue;
      const waiter = waiters.splice(index, 1)[0];
      clearTimeout(waiter.timeout);
      waiter.resolve(frame);
      return;
    }
    frames.push(frame);
  });
  socket.addEventListener("error", () => rejectAll(new Error("visitor websocket failed")));
  socket.addEventListener("close", () => {
    closed = true;
    rejectAll(new Error("visitor websocket closed before the expected frame arrived"));
  });

  const waitFor = (predicate, label, timeoutMs = 10000) => {
    for (let index = 0; index < frames.length; index += 1) {
      if (!predicate(frames[index])) continue;
      return Promise.resolve(frames.splice(index, 1)[0]);
    }
    if (closed) return Promise.reject(new Error(`visitor websocket closed while waiting for ${label}`));
    return new Promise((resolve, reject) => {
      const timeout = setTimeout(() => {
        const index = waiters.findIndex((item) => item.resolve === resolve);
        if (index >= 0) waiters.splice(index, 1);
        reject(new Error(`timed out waiting for visitor websocket ${label}`));
      }, timeoutMs);
      waiters.push({ predicate, resolve, reject, timeout });
    });
  };

  await waitFor((frame) => frame.type === "hub.ready", "hub.ready");
  if (!opened) throw new Error("visitor websocket emitted hub.ready before opening");
  socket.send(JSON.stringify({ action: "subscribe", topics: [`conversation:${conversationID}`] }));
  return {
    socket,
    waitFor,
    close: () => socket.close(),
  };
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

const publicForm = await request("/api/v1/forms", {
  method: "POST",
  body: {
    name: "Production journey request form",
    slug: `journey-form-${suffix}`,
    purpose: "ticket",
    access: "public",
    confirmation: { message: "Your request form was received." },
    enabled: true,
    fields: [
      { key: "kind", label: "Request type", type: "enum", options: ["question", "bug"], required: true, position: 0 },
      { key: "details", label: "Bug details", type: "text", required: true, condition: { field: "kind", operator: "is", value: "bug" }, position: 1 },
      { key: "attachment", label: "Attachment", type: "file", required: true, position: 2 },
    ],
  },
  expected: 201,
});
const publicFormSlug = requireValue(publicForm?.slug, "public form slug");
const authenticatedForm = await request("/api/v1/forms", {
  method: "POST",
  body: {
    name: "Production journey authenticated form",
    slug: `journey-auth-form-${suffix}`,
    purpose: "customer",
    access: "authenticated",
    enabled: true,
    fields: [{ key: "email", label: "Email", type: "email", required: true, position: 0 }],
  },
  expected: 201,
});
const authenticatedFormSlug = requireValue(authenticatedForm?.slug, "authenticated form slug");
const anonymousPortalForms = await request(`/api/v1/portal/forms?portal=${encodeURIComponent(portalID)}`);
if (!Array.isArray(anonymousPortalForms?.data) || anonymousPortalForms.data.length !== 1 || anonymousPortalForms.data[0]?.slug !== publicFormSlug) {
  throw new Error("anonymous portal form list exposed an authenticated form or omitted the public form");
}
const publicPortalForm = await request(`/api/v1/portal/forms/${encodeURIComponent(publicFormSlug)}?portal=${encodeURIComponent(portalID)}`);
if (publicPortalForm?.slug !== publicFormSlug || publicPortalForm?.workspace_id !== undefined) {
  throw new Error("portal form detail did not return the public form without internal workspace data");
}
await request(`/api/v1/portal/forms/${encodeURIComponent(authenticatedFormSlug)}?portal=${encodeURIComponent(portalID)}`, { expected: 401 });
const formAttachment = await uploadFile(`/api/v1/portal/forms/${encodeURIComponent(publicFormSlug)}/files?portal=${encodeURIComponent(portalID)}`, {
  fields: {},
  filename: `journey-form-${suffix}.txt`,
  contents: "Portal form attachment payload.",
});
const formSubmission = await request(`/api/v1/portal/forms/${encodeURIComponent(publicFormSlug)}/submissions?portal=${encodeURIComponent(portalID)}`, {
  method: "POST",
  body: {
    // Selecting question leaves the required bug-details field hidden. The
    // server must apply the same conditional rule as the portal UI.
    values: { kind: "question" },
    file_ids: { attachment: requireValue(formAttachment?.id, "form attachment id") },
    source_url: `${origin}/portal/forms/${publicFormSlug}`,
  },
  expected: 201,
});
if (formSubmission?.status !== "accepted" || formSubmission?.confirmation?.message !== "Your request form was received.") {
  throw new Error("portal form submission did not preserve its accepted status and confirmation");
}
log("portal forms, conditional answers, and staged attachment submission");

const weeklyHours = Array.from({ length: 7 }, () => [{ start: "09:00", end: "17:00" }]);
const calendar = await request("/api/v1/sla/calendars", {
  method: "POST",
  body: {
    name: "Production journey business hours",
    timezone: "UTC",
    weekly: weeklyHours,
    holidays: [],
    is_default: true,
  },
  expected: 201,
});
const calendarID = requireValue(calendar?.id, "SLA calendar id");
const slaPolicy = await request("/api/v1/sla/policies", {
  method: "POST",
  body: {
    name: "Production journey response policy",
    description: "SLA policy created by the release journey.",
    calendar_id: calendarID,
    targets: [{ priority: "normal", first_response_minutes: 0, next_response_minutes: 0, resolution_minutes: 0 }],
    pause_states: ["waiting_for_customer"],
    warning_threshold_percent: 50,
    escalation_actions: [],
    applies_to: {},
    enabled: true,
  },
  expected: 201,
});
if (slaPolicy?.calendar_id !== calendarID || slaPolicy?.enabled !== true) {
  throw new Error("SLA policy did not preserve its calendar or enabled state");
}
log("SLA calendar and policy configuration");

const webhook = await request("/api/v1/webhooks", {
  method: "POST",
  body: {
    url: webhookProbeURL,
    description: "Production journey webhook",
    events: ["ticket.created", "conversation.created"],
    enabled: true,
  },
  expected: 201,
});
const webhookID = requireValue(webhook?.data?.id, "webhook id");
if (typeof webhook?.secret !== "string" || !webhook.secret.startsWith("whsec_")) {
  throw new Error("webhook creation did not return a signing secret");
}
const webhookTest = await request(`/api/v1/webhooks/${webhookID}/test`, {
  method: "POST",
  expected: 202,
});
const webhookDeliveryID = requireValue(webhookTest?.id, "webhook test delivery id");
let webhookDelivery;
for (let attempt = 0; attempt < 40; attempt += 1) {
  const webhookDeliveries = await request(`/api/v1/webhooks/${webhookID}/deliveries`);
  webhookDelivery = webhookDeliveries?.data?.find((item) => item.id === webhookDeliveryID);
  if (webhookDelivery?.status === "delivered") break;
  await new Promise((resolve) => setTimeout(resolve, 100));
}
if (webhookDelivery?.status !== "delivered" || !webhookProbeRequest) {
  throw new Error("webhook worker did not deliver the test request");
}
const webhookTimestamp = String(webhookProbeRequest.headers["x-hubchat-timestamp"] ?? "");
const webhookSignature = String(webhookProbeRequest.headers["x-hubchat-signature"] ?? "");
const expectedWebhookSignature = `v1=${createHmac("sha256", webhook.secret).update(`${webhookTimestamp}.${webhookProbeRequest.body.toString()}`).digest("hex")}`;
if (!webhookTimestamp || webhookSignature !== expectedWebhookSignature) {
  throw new Error("webhook delivery signature did not match the raw request body");
}
const replay = await request(`/api/v1/webhooks/${webhookID}/deliveries/${webhookDeliveryID}/replay`, {
  method: "POST",
  expected: 202,
});
if (!replay?.id || replay.id === webhookDeliveryID) throw new Error("webhook replay did not create a new delivery");
webhookProbe.close();
log("webhook signing, test delivery, and replay");

const knowledgeBase = await request("/api/v1/knowledge-bases", {
  method: "POST",
  body: {
    name: "Production journey help center",
    slug: `journey-help-${suffix}`,
    default_language: "en",
    languages: ["en"],
    visibility: "public",
  },
  expected: 201,
});
const knowledgeBaseID = requireValue(knowledgeBase?.id, "knowledge-base id");
const knowledgeBaseSlug = requireValue(knowledgeBase?.slug, "knowledge-base slug");
const collection = await request(`/api/v1/knowledge-bases/${knowledgeBaseID}/collections`, {
  method: "POST",
  body: {
    name: "Production journey guides",
    slug: `journey-guides-${suffix}`,
    description: "Guides created by the release journey.",
    position: 0,
  },
  expected: 201,
});
const collectionID = requireValue(collection?.id, "knowledge-base collection id");
const article = await request("/api/v1/articles", {
  method: "POST",
  body: {
    knowledge_base_id: knowledgeBaseID,
    collection_id: collectionID,
    title: "Production journey guide",
    slug: `journey-guide-${suffix}`,
    excerpt: "A searchable guide created by the release journey.",
    body: "This production journey verifies published knowledge-base search.",
    state: "draft",
    language: "en",
  },
  expected: 201,
});
const articleID = requireValue(article?.id, "knowledge-base article id");
const articleSlug = requireValue(article?.slug, "knowledge-base article slug");
const publishedArticle = await request(`/api/v1/articles/${articleID}/publish`, {
  method: "POST",
  expected: 200,
});
if (publishedArticle?.state !== "published") throw new Error("article was not published");
const articleSearch = await request(
  `/api/v1/public/knowledge-bases/${workspaceID}/search?knowledge_base=${encodeURIComponent(knowledgeBaseSlug)}&q=${encodeURIComponent("production journey")}&language=en&surface=portal`,
);
if (!Array.isArray(articleSearch?.data) || !articleSearch.data.some((item) => item?.article?.id === articleID)) {
  throw new Error("published article was not returned by public knowledge-base search");
}
const publicArticle = await request(`/api/v1/public/knowledge-bases/${workspaceID}/articles/${encodeURIComponent(articleSlug)}?surface=portal`);
if (publicArticle?.id !== articleID || publicArticle?.state !== "published") {
  throw new Error("public article lookup did not return the published article");
}
await request(`/api/v1/public/knowledge-bases/${workspaceID}/articles/${encodeURIComponent(articleSlug)}/feedback`, {
  method: "POST",
  body: { helpful: true, comment: "The production journey guide was useful." },
  expected: 201,
});
log("knowledge-base publishing, search, and helpfulness feedback");

const survey = await request("/api/v1/surveys", {
  method: "POST",
  body: {
    name: "Production journey satisfaction",
    type: "csat",
    delivery: ["portal"],
    anonymous: true,
    questions: [{ prompt: "How was this support journey?", type: "number", required: true, position: 0 }],
  },
  expected: 201,
});
const surveyID = requireValue(survey?.id, "survey id");
const questionID = requireValue(survey?.questions?.[0]?.id, "survey question id");
const publicSurvey = await request(`/api/v1/public/surveys/${workspaceID}/${surveyID}`);
if (publicSurvey?.id !== surveyID || publicSurvey?.enabled !== true) throw new Error("public survey was not enabled");
const surveyResponse = await request(`/api/v1/public/surveys/${workspaceID}/${surveyID}/responses`, {
  method: "POST",
  body: { score: 5, answers: { [questionID]: 5 }, comment: "The journey was clear." },
  expected: 201,
});
if (surveyResponse?.survey_id !== surveyID || surveyResponse?.score !== 5) {
  throw new Error("public survey response was not recorded");
}
log("survey delivery and response");

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

const firstJourneyURL = `${origin}/journey-pricing`;
const currentJourneyURL = `${origin}/journey-checkout`;
for (const page of [
  { url: firstJourneyURL, title: "Pricing", referrer_origin: `${origin}/search` },
  { url: currentJourneyURL, title: "Checkout", referrer_origin: firstJourneyURL },
]) {
  await request("/api/v1/widget/events", {
    method: "POST",
    body: {
      public_key: publicKey,
      url: page.url,
      token: visitorToken,
      type: "page.viewed",
      page_url: page.url,
      payload: {
        page: { origin, path: new URL(page.url).pathname },
        title: page.title,
        referrer_origin: page.referrer_origin,
        language: "en-US",
        timezone: "UTC",
        platform: "Linux x86_64",
        user_agent: "Hubchat production journey browser",
        device: "desktop",
        browser: "Chrome",
        os: "Linux",
        viewport: { width: 1440, height: 900, device_pixel_ratio: 1 },
      },
    },
    expected: 204,
  });
}

const identified = await request("/api/v1/widget/identify", {
  method: "POST",
  body: { ...visitorBody, name: "Journey Customer", email: customerEmail, external_id: `journey-customer-${suffix}` },
});
const customerID = requireValue(identified?.customer?.id, "identified customer id");
log("anonymous visitor identity");

await request("/api/v1/portal/auth/magic-link", {
  method: "POST",
  body: { portal: portalID, email: customerEmail, next: "/tickets" },
  expected: 202,
});
const emailJobs = await request("/api/v1/jobs?queue=email&limit=100");
const magicJob = emailJobs?.data?.find((job) => job?.type === "email.send" && job?.payload?.to === customerEmail && String(job?.payload?.body ?? "").includes("/portal/sign-in"));
const magicLinkBody = String(magicJob?.payload?.body ?? "");
const tokenMatch = magicLinkBody.match(/[?&]token=([^&\s]+)/);
if (!tokenMatch) throw new Error("portal magic-link email job did not contain a redeemable token");
const portalToken = decodeURIComponent(tokenMatch[1]);
const portalSession = await request("/api/v1/portal/auth/magic-link/redeem", {
  method: "POST",
  body: { token: portalToken },
  expected: 200,
});
if (portalSession?.session?.portal_id !== portalID || portalSession?.viewer?.id !== customerID) {
  throw new Error("portal magic-link redemption did not create the identified customer session");
}
const portalMe = await request(`/api/v1/portal/me?portal=${encodeURIComponent(portalID)}`);
if (portalMe?.viewer?.id !== customerID) throw new Error("portal session did not resolve the customer profile");
const authenticatedPortalForms = await request(`/api/v1/portal/forms?portal=${encodeURIComponent(portalID)}`);
if (!Array.isArray(authenticatedPortalForms?.data) || !authenticatedPortalForms.data.some((item) => item?.slug === authenticatedFormSlug)) {
  throw new Error("authenticated portal form list did not include the signed-in form");
}
const authenticatedPortalForm = await request(`/api/v1/portal/forms/${encodeURIComponent(authenticatedFormSlug)}?portal=${encodeURIComponent(portalID)}`);
if (authenticatedPortalForm?.access !== "authenticated") throw new Error("authenticated portal form detail did not enforce its access mode");
log("portal magic-link delivery and authenticated session");

if (process.env.HUBCHAT_JOURNEY_BROWSER === "1") {
  const portalCookie = cookies.get("hubchat_portal_session");
  const browserResult = await runBrowserJourney({
    baseURL,
    publicKey,
    portalID,
    portalCookie,
    portalFormSlug: publicFormSlug,
    viewerName: "Journey Customer",
    dashboardCookie: cookies.get("hubchat_session"),
    workspaceName: "Production Journey Workspace",
  });
  if (!browserResult.portalChecked) throw new Error("browser journey did not receive the authenticated portal session");
  if (!browserResult.portalFormsChecked) throw new Error("browser journey did not render the authenticated portal forms flow");
  if (!browserResult.dashboardChecked) throw new Error("browser journey did not receive the authenticated dashboard session");
  log("browser widget CSS isolation, accessible dialog, portal forms/navigation, and dashboard navigation");
}

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

// Exercise the offline/reconnect command path before opening the visitor
// socket. The dashboard creates and invokes the host-owned binding; the
// visitor then claims it through the public endpoint and acknowledges it.
const commandBinding = await request("/api/v1/customer-command-bindings", {
  method: "POST",
  body: { name: `reload_page_${suffix}`, description: "Reload the customer page for diagnostics." },
  expected: 201,
});
const commandBindingID = requireValue(commandBinding?.id, "customer command binding id");
const commandInvocation = await request(`/api/v1/conversations/${conversationID}/customer-commands`, {
  method: "POST",
  body: { binding_id: commandBindingID, payload: { reason: "production-journey" } },
  expected: 202,
});
const commandID = requireValue(commandInvocation?.id, "customer command invocation id");
const pendingCommands = await request(`/api/v1/widget/conversations/${conversationID}/commands?key=${encodeURIComponent(publicKey)}&url=${encodeURIComponent(widgetURL)}&token=${encodeURIComponent(tokenAfterStart)}`);
if (pendingCommands?.data?.length !== 1 || pendingCommands.data[0]?.command_id !== commandID || pendingCommands.data[0]?.name !== commandBinding.name || pendingCommands.data[0]?.payload?.reason !== "production-journey") {
  throw new Error("offline customer command was not returned by the reconnect endpoint");
}
const pendingAgain = await request(`/api/v1/widget/conversations/${conversationID}/commands?key=${encodeURIComponent(publicKey)}&url=${encodeURIComponent(widgetURL)}&token=${encodeURIComponent(tokenAfterStart)}`);
if (pendingAgain?.data?.length !== 0) throw new Error("claimed customer command was returned twice");
await request(`/api/v1/widget/conversations/${conversationID}/commands/${commandID}/ack`, {
  method: "POST",
  body: { public_key: publicKey, url: widgetURL, token: tokenAfterStart, status: "acknowledged" },
  expected: 204,
});
log("customer command reconnect delivery and acknowledgement");

const visitorSocket = await openVisitorSocket({
  publicKey,
  token: tokenAfterStart,
  pageURL: widgetURL,
  conversationID,
});
await visitorSocket.waitFor(
  (frame) => frame.type === "hub.topics" && frame.data?.topics?.includes(`conversation:${conversationID}`),
  "conversation subscription",
);
const liveCommandInvocation = await request(`/api/v1/conversations/${conversationID}/customer-commands`, {
  method: "POST",
  body: { binding_id: commandBindingID, payload: { reason: "realtime" } },
  expected: 202,
});
const liveCommandFrame = await visitorSocket.waitFor(
  (frame) => frame.type === "customer.command" && frame.entity_type === "conversation" && frame.entity_id === conversationID && frame.data?.command_id === liveCommandInvocation?.id,
  "live customer command",
);
if (liveCommandFrame.data?.name !== commandBinding.name || liveCommandFrame.data?.payload?.reason !== "realtime") {
  visitorSocket.close();
  throw new Error("visitor websocket delivered an unexpected customer command");
}
await request(`/api/v1/widget/conversations/${conversationID}/commands/${liveCommandInvocation.id}/ack`, {
  method: "POST",
  body: { public_key: publicKey, url: widgetURL, token: tokenAfterStart, status: "acknowledged" },
  expected: 204,
});
log("customer command realtime delivery and acknowledgement");
const realtimeAgentReply = await request(`/api/v1/conversations/${conversationID}/messages`, {
  method: "POST",
  body: { kind: "reply", author_name: "Journey Agent", body: "The realtime transport is working." },
  expected: 201,
});
const realtimeFrame = await visitorSocket.waitFor(
  (frame) => frame.type === "message.created" && frame.data?.id === realtimeAgentReply?.id,
  "agent message.created",
);
if (realtimeFrame.data?.author_type !== "agent" || realtimeFrame.data?.body !== "The realtime transport is working.") {
  visitorSocket.close();
  throw new Error("visitor websocket delivered an unexpected agent reply");
}
visitorSocket.close();
log("visitor realtime delivery and topic authorization");

const posted = await request(`/api/v1/widget/conversations/${conversationID}/messages`, {
  method: "POST",
  body: { ...conversationBody, body: "The follow-up message should be visible to the agent." },
  expected: 201,
});
if (!posted?.id || posted.author_type !== "customer") throw new Error("widget reply was not created as a customer message");
log("visitor conversation and reply");

const bulkAssigneeID = requireValue(bootstrap?.viewer?.id, "bootstrap viewer member id");
const bulkUpdate = await request("/api/v1/conversations/bulk", {
  method: "POST",
  body: { ids: [conversationID], action: "assign", assignee_id: bulkAssigneeID },
  expected: 200,
});
if (bulkUpdate?.count !== 1 || bulkUpdate?.data?.[0]?.assignee_id !== bulkAssigneeID) {
  throw new Error("bulk conversation assignment did not return the assigned conversation");
}
log("transactional conversation bulk assignment");

const customer360 = await request(`/api/v1/customers/${encodeURIComponent(customerID)}/360`);
if (!Array.isArray(customer360?.conversations) || !customer360.conversations.some((item) => item?.id === conversationID)) {
  throw new Error("customer 360 did not include the identified visitor conversation");
}
if (!Array.isArray(customer360?.feedback) || !customer360.feedback.some((item) => item?.id === feedbackItemID)) {
  throw new Error("customer 360 did not include the identified visitor feedback");
}
if (customer360.events?.some((item) => Object.prototype.hasOwnProperty.call(item, "payload"))) {
  throw new Error("customer 360 exposed a raw event payload instead of redacted event metadata");
}
if (!customer360.current_page?.url || customer360.current_page.url !== currentJourneyURL) {
  throw new Error("customer 360 did not preserve the current page from widget events");
}
if (!Array.isArray(customer360.page_journey) || customer360.page_journey.length < 2 || !customer360.page_journey.some((item) => item?.url === firstJourneyURL)) {
  throw new Error("customer 360 did not preserve the visitor page journey");
}
const latestSession = customer360.sessions?.[0];
if (latestSession?.current_title !== "Checkout" || latestSession?.language !== "en-US" || latestSession?.platform !== "Linux x86_64" || latestSession?.viewport?.width !== 1440) {
  throw new Error("customer 360 did not preserve rich session context");
}
log("unified customer 360 context and redacted event metadata");

const ticket = await request(`/api/v1/conversations/${conversationID}/ticket`, {
  method: "POST",
  body: { title: "Production journey request", description: "Created by the release journey.", priority: "normal", customer_id: customerID },
  expected: 201,
});
if (!ticket?.id || ticket.conversation_id !== conversationID) throw new Error("ticket conversion did not preserve the conversation link");
log("conversation to ticket conversion");

let breachedSLA;
for (let attempt = 0; attempt < 40; attempt += 1) {
  const breached = await request("/api/v1/sla/instances?state=breached&limit=100");
  breachedSLA = breached?.data?.find((item) => item?.ticket_id === ticket.id);
  if (breachedSLA?.breached_at && breachedSLA?.warned_at) break;
  await new Promise((resolve) => setTimeout(resolve, 100));
}
if (!breachedSLA?.breached_at || !breachedSLA?.warned_at) {
  throw new Error("SLA scheduler did not persist warning and breach state for the ticket");
}
log("SLA warning and breach evaluation");

const portalTickets = await request(`/api/v1/portal/tickets?portal=${encodeURIComponent(portalID)}`);
if (!Array.isArray(portalTickets?.data) || !portalTickets.data.some((item) => item.id === ticket.id)) {
  throw new Error("authenticated portal ticket list did not include the customer ticket");
}
const portalAttachment = await uploadFile(`/api/v1/portal/tickets/${ticket.id}/files?portal=${encodeURIComponent(portalID)}`, {
  fields: {},
  filename: `portal-${suffix}.txt`,
  contents: "Portal reply attachment payload.",
});
const portalAttachmentID = requireValue(portalAttachment?.id, "portal attachment id");
const portalReplyBody = {
  client_id: `production-journey-portal-reply-${suffix}`,
  body: "The customer portal reply is idempotent and supports attachments.",
  file_ids: [portalAttachmentID],
};
const firstPortalReply = await request(`/api/v1/portal/tickets/${ticket.id}/replies?portal=${encodeURIComponent(portalID)}`, {
  method: "POST",
  body: portalReplyBody,
  expected: 201,
});
const replayedPortalReply = await request(`/api/v1/portal/tickets/${ticket.id}/replies?portal=${encodeURIComponent(portalID)}`, {
  method: "POST",
  body: portalReplyBody,
  expected: 201,
});
if (!firstPortalReply?.id || replayedPortalReply?.id !== firstPortalReply.id || firstPortalReply?.attachments?.length !== 1) {
  throw new Error("portal reply retry did not preserve one message and its attachment");
}
const portalDownloadedAttachment = await downloadText(`/api/v1/portal/files/${portalAttachmentID}?portal=${encodeURIComponent(portalID)}`);
if (portalDownloadedAttachment !== "Portal reply attachment payload.") {
  throw new Error("portal attachment download did not preserve the uploaded content");
}
const portalTicketDetail = await request(`/api/v1/portal/tickets/${ticket.id}?portal=${encodeURIComponent(portalID)}`);
if (!Array.isArray(portalTicketDetail?.messages) || !portalTicketDetail.messages.some((item) => item.id === firstPortalReply.id)) {
  throw new Error("portal ticket detail did not include the customer reply");
}
log("authenticated portal ticket reply, idempotency, and attachment access");

const attachment = await uploadFile("/api/v1/files", {
  fields: { owner_type: "ticket", owner_id: ticket.id },
  filename: `journey-${suffix}.txt`,
  contents: "Production journey attachment payload.",
});
const attachmentID = requireValue(attachment?.id, "attachment id");
const downloadedAttachment = await downloadText(`/api/v1/files/${attachmentID}`);
if (downloadedAttachment !== "Production journey attachment payload.") {
  throw new Error("downloaded attachment did not preserve the uploaded content");
}
log("ticket attachment upload and authorized download");

const automationRule = await request("/api/v1/automation/rules", {
  method: "POST",
  body: {
    name: "Production journey priority rule",
    description: "Dry-run rule created by the release journey.",
    trigger: "ticket.created",
    conditions: {},
    actions: [{ type: "set_priority", params: { priority: "high" } }],
    position: 0,
    enabled: true,
    max_runs_per_hour: 10,
  },
  expected: 201,
});
const automationRuleID = requireValue(automationRule?.id, "automation rule id");
const execution = await request(`/api/v1/automation/rules/${automationRuleID}/dry-run`, {
  method: "POST",
  body: {
    subject_type: "ticket",
    subject_id: ticket.id,
    depth: 0,
    causation_id: `journey-causation-${suffix}`,
  },
  expected: 200,
});
if (execution?.outcome !== "matched" || execution?.dry_run !== true) {
  throw new Error("automation dry-run did not match without applying changes");
}
const liveAutomationRule = await request("/api/v1/automation/rules", {
  method: "POST",
  body: {
    name: "Production journey live priority rule",
    description: "Updates a normal ticket once when its priority changes.",
    trigger: "ticket.updated",
    conditions: { conditions: [{ field: "priority", operator: "is", value: "normal" }] },
    actions: [{ type: "set_priority", params: { priority: "high" } }],
    position: 1,
    enabled: true,
    max_runs_per_hour: 10,
  },
  expected: 201,
});
const liveAutomationRuleID = requireValue(liveAutomationRule?.id, "live automation rule id");
await request(`/api/v1/tickets/${ticket.id}/priority`, {
  method: "PATCH",
  body: { priority: "normal" },
  expected: 200,
});
let liveExecution;
let liveTicket;
for (let attempt = 0; attempt < 40; attempt += 1) {
  const executions = await request(`/api/v1/automation/executions?rule_id=${encodeURIComponent(liveAutomationRuleID)}&limit=20`);
  liveExecution = executions?.data?.find((item) => item?.dry_run === false && item?.outcome === "matched");
  liveTicket = await request(`/api/v1/tickets/${encodeURIComponent(ticket.id)}`);
  if (liveExecution?.outcome === "matched" && liveTicket?.priority === "high") break;
  await new Promise((resolve) => setTimeout(resolve, 100));
}
if (liveExecution?.outcome !== "matched" || liveTicket?.priority !== "high") {
  throw new Error("automation event consumer did not apply the live priority rule");
}
log("automation dry-run and live event execution");

const exportPreview = await request("/api/v1/portability/exports/preview", {
  method: "POST",
  body: { kind: "workspace", scope: {} },
});
if (!Array.isArray(exportPreview?.tables) || typeof exportPreview?.row_count !== "number") {
  throw new Error("workspace export preview did not return table summaries");
}
const exportRequest = await request("/api/v1/portability/exports", {
  method: "POST",
  body: { kind: "workspace", scope: {} },
  expected: 202,
});
const exportID = requireValue(exportRequest?.id, "workspace export id");
let completedExport;
for (let attempt = 0; attempt < 60; attempt += 1) {
  completedExport = await request(`/api/v1/portability/exports/${encodeURIComponent(exportID)}`);
  if (completedExport?.state === "completed" || completedExport?.state === "failed") break;
  await new Promise((resolve) => setTimeout(resolve, 100));
}
if (completedExport?.state !== "completed" || typeof completedExport?.file_id !== "string") {
  throw new Error(`workspace export did not complete: ${completedExport?.state ?? "unknown"}`);
}
const exportManifest = await request(`/api/v1/portability/exports/${encodeURIComponent(exportID)}/manifest`);
if (exportManifest?.export_id !== exportID || exportManifest?.file_id !== completedExport.file_id || !exportManifest?.checksum) {
  throw new Error("workspace export manifest did not verify the completed archive");
}
const exportBytes = await downloadBytes(`/api/v1/files/${encodeURIComponent(completedExport.file_id)}`);
const exportChecksum = createHash("sha256").update(exportBytes).digest("hex");
if (exportBytes.length !== exportManifest.size_bytes || exportChecksum !== exportManifest.checksum) {
  throw new Error(`workspace export download failed integrity verification: ${exportBytes.length} bytes, ${exportChecksum}`);
}
const downloadAudit = await request(`/api/v1/audit-logs?action=data.file_downloaded&entity_type=file&entity_id=${encodeURIComponent(completedExport.file_id)}&limit=20`);
if (!downloadAudit?.data?.some((entry) => entry.action === "data.file_downloaded" && entry.entity_id === completedExport.file_id && entry.request_id)) {
  throw new Error("workspace export download was not recorded in the audit log");
}
const importRequest = await request("/api/v1/portability/imports", {
  method: "POST",
  body: { file_id: completedExport.file_id, kind: "workspace", auto_start: false },
  expected: 202,
});
const importID = requireValue(importRequest?.id, "workspace import id");
const importPreview = await request(`/api/v1/portability/imports/${encodeURIComponent(importID)}/preview`, {
  method: "POST",
  expected: 200,
});
if (!Array.isArray(importPreview?.data) || !importPreview.data.some((item) => item?.name === "inboxes")) {
  throw new Error("workspace import preview did not include archive tables");
}
await request(`/api/v1/portability/imports/${encodeURIComponent(importID)}/confirm`, {
  method: "POST",
  body: { backup_verified: true },
  expected: 202,
});
let completedImport;
for (let attempt = 0; attempt < 60; attempt += 1) {
  completedImport = await request(`/api/v1/portability/imports/${encodeURIComponent(importID)}`);
  if (completedImport?.state === "completed" || completedImport?.state === "failed") break;
  await new Promise((resolve) => setTimeout(resolve, 100));
}
if (completedImport?.state !== "completed" || completedImport?.processed_rows !== completedImport?.total_rows) {
  throw new Error(`workspace import did not complete: ${completedImport?.state ?? "unknown"}`);
}
log("workspace export, byte checksum verification, download audit, and validated import");

console.log("Production HTTP/realtime journey OK (setup, portal, SLA, webhook, knowledge base, survey, widget, feedback, conversation, realtime, bulk actions, customer 360, ticket, portal reply, attachments, automation, export/import)");
