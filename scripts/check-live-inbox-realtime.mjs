/**
 * Regression harness for the agent-side "visitor wrote back but my open
 * conversation never moved" bug.
 *
 * Scenario mirrors production: the agent has the conversation already open and
 * is watching it, then a visitor replies from the real widget API. The
 * realtime event arrives and the sidebar preview updates — but the messages in
 * the open thread stay frozen until the agent reloads or navigates away, so
 * from the agent's seat the conversation looks dead.
 *
 * Cause: `message.created` was invalidated against the conversation list and
 * the conversation header (`["conversations"]`, `["conversation", id]`) but not
 * against the thread's own window (`["conversation-messages", id]`). Those are
 * sibling namespaces, and cache invalidation matches by prefix, so the open
 * thread kept serving its first page until something else refetched it.
 *
 * Run: node scripts/check-live-inbox-realtime.mjs
 * Set HUBCHAT_JOURNEY_BASE_URL (plus HUBCHAT_BINARY_DATABASE_URL for the
 * parent harness) to point at a running server; the parent production binary
 * check starts one automatically and passes its base URL down.
 */
import { chromium } from "playwright-core";

const baseURL = (process.env.HUBCHAT_JOURNEY_BASE_URL ?? "http://127.0.0.1:8080").replace(/\/$/, "");
const pageHost = new URL(baseURL).hostname;

let cookie = "";
async function api(path, { method = "GET", body } = {}) {
  const response = await fetch(`${baseURL}${path}`, {
    method,
    headers: {
      "content-type": "application/json",
      // Same-origin POSTs carry an Origin header in a browser; the CSRF
      // middleware requires one and rejects anything that is not the
      // server's own public URL.
      origin: baseURL,
      ...(cookie ? { cookie } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const setCookie = response.headers.get("set-cookie");
  if (setCookie) cookie = setCookie.split(";")[0];
  const text = await response.text();
  if (!response.ok) throw new Error(`${method} ${path} -> ${response.status} ${text.slice(0, 300)}`);
  return text ? JSON.parse(text) : null;
}

const widgetCall = (path, payload) =>
  fetch(`${baseURL}/api/v1/widget${path}`, {
    method: "POST",
    headers: { "content-type": "application/json", origin: baseURL },
    body: JSON.stringify({ public_key: publicKey, url: `${baseURL}/`, ...payload }),
  }).then(async (response) => {
    if (!response.ok) throw new Error(`widget ${path} -> ${response.status} ${(await response.text()).slice(0, 300)}`);
    return response.json();
  });

let publicKey = "";

const suffix = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
await api("/api/v1/auth/signup", {
  method: "POST",
  body: { name: "Realtime Agent", email: `realtime-${suffix}@example.com`, password: "sup3r-secret-pass!" },
});
const workspace = await api("/api/v1/workspaces", {
  method: "POST",
  body: { name: "Realtime Workspace", slug: `realtime-${suffix}` },
});
const { inboxes } = await api("/api/v1/bootstrap");
const inbox = inboxes[0];
const widget = await api("/api/v1/widgets", {
  method: "POST",
  body: { name: "Realtime widget", inbox_id: inbox.id },
});
publicKey = widget.public_key;
await api(`/api/v1/widgets/${widget.id}`, {
  method: "PUT",
  body: {
    name: widget.name,
    inbox_id: inbox.id,
    domains: [pageHost],
    enabled: true,
    modes: ["chat"],
    rollout_percent: 100,
    environment: "live",
  },
});

const visitor = await widgetCall("/visitors", {});
const opened = await widgetCall("/conversations", { token: visitor.token, body: "Hello, my order never arrived" });
const conversationID = opened.conversation_id;

const browser = await chromium.launch({
  executablePath: process.env.CHROME_PATH ?? "/usr/bin/google-chrome",
  args: ["--no-sandbox", "--disable-dev-shm-usage"],
});
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const agent = await context.newPage();
const pageErrors = [];
agent.on("pageerror", (error) => pageErrors.push(String(error).slice(0, 200)));

const [cookieName, cookieValue] = cookie.split("=");
await context.addCookies([{ name: cookieName, value: cookieValue, domain: pageHost, path: "/", httpOnly: true }]);
await agent.goto(`${baseURL}/app/inbox`, { waitUntil: "domcontentloaded" });
await agent.waitForTimeout(3000);
await agent.locator("text=my order never arrived").first().click({ timeout: 30_000 });
await agent.waitForTimeout(2000);

/**
 * Assert against the conversation pane only. The sidebar row preview is
 * refreshed by the conversation-list invalidation that has always worked, so
 * checking the whole document body can never fail — that ambiguity is exactly
 * what made the bug look intermittent in the first place.
 */
const inOpenThread = (text) =>
  agent.evaluate((needle) => {
    const composer = document.querySelector('[aria-label="Public reply"]');
    if (!composer) return { found: false, reason: "reply composer not mounted" };
    let panel = composer;
    while (panel && !panel.innerText.includes("my order never arrived")) panel = panel.parentElement;
    if (!panel) return { found: false, reason: "conversation pane not located" };
    return { found: panel.innerText.includes(needle) };
  }, text);

if (!(await inOpenThread("my order never arrived")).found) {
  throw new Error(`agent never saw the conversation it opened (${workspace.id}/${conversationID})`);
}

const reply = `did you get this? ${Date.now()}`;
await widgetCall(`/conversations/${conversationID}/messages`, { token: visitor.token, body: reply });

const deadline = Date.now() + 10_000;
let arrived = false;
let reason = "";
while (Date.now() < deadline) {
  const probe = await inOpenThread(reply);
  reason = probe.reason ?? "";
  if (probe.found) {
    arrived = true;
    break;
  }
  await new Promise((resolve) => setTimeout(resolve, 200));
}

const sidebarUpdated = await agent.evaluate((text) => document.body.innerText.includes(text), reply);
const stillInThread = (await inOpenThread("my order never arrived")).found;
await browser.close();

console.log(`  inbound message reached the open conversation: ${arrived}`);
console.log(`  sidebar preview updated too (the reported symptom): ${sidebarUpdated}`);
console.log(`  agent stayed on the same conversation:            ${stillInThread}`);
console.log(`  page errors: ${pageErrors.length ? pageErrors.join(" | ") : "none"}`);

if (!arrived) {
  throw new Error(
    `the open conversation never received the inbound message${reason ? ` (${reason})` : ""}; ` +
      "an agent watching a thread would sit on stale messages until they reloaded",
  );
}
if (!stillInThread) throw new Error("the agent was navigated away from the conversation under test");
if (pageErrors.length) throw new Error(`the inbox raised page errors: ${pageErrors.join(" | ")}`);

console.log("Live inbox realtime check OK (visitor reply lands in the conversation the agent already has open)");