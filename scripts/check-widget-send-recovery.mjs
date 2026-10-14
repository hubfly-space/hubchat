/**
 * Regression harness for the visitor-side "cannot send into a conversation I
 * can still see" bug.
 *
 * Scenario mirrors production: the widget resumes a persisted conversation,
 * history loads fine — so the visitor really does see their previous chat —
 * and then the server rejects the write with
 *   403 { "code": "forbidden", "message": "This conversation does not belong to you." }
 *
 * Before the fix the widget left `conversationRef` pointing at the rejected
 * thread and the message bubble in a failed state, so every later message
 * repeated the identical 403 until the visitor reloaded the page. The
 * mount-time rejection leaked a second variant of the same bug: it dropped the
 * conversation but kept the rejected visitor token in memory.
 *
 * After the fix the widget discards the unusable identity, keeps the
 * optimistic bubble, and retries once with a fresh visitor and conversation.
 *
 * Run: node scripts/check-widget-send-recovery.mjs
 */
import { createServer } from "node:http";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { chromium } from "playwright-core";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");

/**
 * Set WIDGET_BUNDLE_URL=https://host to run the same regression against a
 * deployed bundle instead of the local build, which is how the production
 * release is verified: the bytes under test are the ones visitors receive.
 */
const bundleOrigin = process.env.WIDGET_BUNDLE_URL?.replace(/\/$/, "");
const bundle = async (name) => {
  if (!bundleOrigin) return readFileSync(join(root, "embedded/assets/widget", name));
  const response = await fetch(`${bundleOrigin}/widget/${name}`);
  if (!response.ok) throw new Error(`could not fetch ${name} from ${bundleOrigin}: ${response.status}`);
  return Buffer.from(await response.arrayBuffer());
};
const PORT = 4599;
const PK = "pk_regression";
const KEY = `hubchat.visitor.${PK}`;

/** Minimal but complete public widget config, per §11.5 (no privileged data). */
const config = {
  enabled: true,
  online: true,
  modes: ["chat"],
  language: "en",
  appearance: {
    accent: "#4f46e5",
    theme: "light",
    launcher_shape: "rounded",
    launcher_size: "md",
    launcher_label: null,
    position: "bottom-right",
    offset_x: 16,
    offset_y: 16,
    panel_width: 380,
    panel_height: 600,
    radius: 16,
    header_style: "solid",
    bubble_style: "rounded",
    z_index: 2147483000,
    hide_branding: false,
  },
  content: {
    title: "Support",
    subtitle: "We usually reply in a few minutes",
    welcome_message: "Hi there, how can we help?",
    input_placeholder: "Write a message...",
    response_time_text: "We usually reply in a few minutes",
    offline_message: "We are offline.",
  },
  behavior: {
    trigger: "manual",
    delay_seconds: 0,
    scroll_percent: 0,
    allow_anonymous: true,
    persist_conversation: true,
  },
  articles: [],
};

const html = `<!doctype html>
<html><head><meta charset="utf-8"><title>widget regression</title></head>
<body>
<script src="/v1.js"></script>
<script>
  Hubchat("boot", { key: "${PK}", host: "http://127.0.0.1:${PORT}" });
  setTimeout(() => Hubchat("open"), 300);
</script>
</body></html>`;

const server = createServer(async (req, res) => {
  const path = req.url.split("?")[0].replace(/^\//, "");
  const send = (type, body) => {
    res.writeHead(200, { "content-type": type });
    res.end(body);
  };
  if (path === "" || path.startsWith("?")) return send("text/html", html);
  if (path === "v1.js") return send("text/javascript", readFileSync(join(root, "web/widget/public/v1.js")));
  // The loader imports these lazily from the host origin.
  if (path === "widget/app.js") return send("text/javascript", await bundle("app.js"));
  if (path === "widget/app.css") return send("text/css", await bundle("app.css"));
  res.writeHead(404).end();
});
await new Promise((r) => server.listen(PORT, "127.0.0.1", r));

/* ------------------------------------------------------------------- stubs */
const failures = [];
const calls = [];
let visitorSeq = 0;
const issued = [];

const wire = (id, body, authorType, sequence = 1) => ({
  id,
  kind: "text",
  author_type: authorType,
  author_name: authorType === "visitor" ? "You" : "Support",
  body,
  sequence,
  created_at: new Date().toISOString(),
});

const json = (route, status, body) => {
  const record = calls[calls.length - 1];
  if (record) record.status = status;
  route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
};

const browser = await chromium.launch({
  executablePath: "/usr/bin/google-chrome",
  args: ["--no-sandbox", "--disable-dev-shm-usage"],
});
const page = await browser.newPage();

await page.addInitScript(
  ([key]) => window.localStorage.setItem(key, JSON.stringify({ token: "tok_original", conversationId: "cnv_old" })),
  [KEY],
);

let seq = 0;
await page.route("**/api/v1/widget/**", async (route) => {
  const req = route.request();
  const url = new URL(req.url());
  const path = url.pathname.replace("/api/v1/widget", "");
  const payload = req.postData() ? JSON.parse(req.postData()) : {};
  calls.push({
    n: ++seq,
    method: req.method(),
    path,
    token: url.searchParams.get("token") ?? payload.token ?? null,
  });

  if (path === "/config") return json(route, 200, config);
  if (path === "/visitors") {
    const token = `tok_fresh_${++visitorSeq}`;
    issued.push({ token, at: calls.length });
    return json(route, 200, { token, visitor_id: `vis_${visitorSeq}` });
  }
  if (path === "/conversations" && req.method() === "POST") {
    return json(route, 201, {
      conversation_id: `cnv_new_${calls.length}`,
      token: "",
      message: wire("msg_new", payload.body, "visitor"),
    });
  }
  if (/^\/conversations\/[^/]+\/messages$/.test(path)) {
    if (req.method() === "GET") {
      // History is reachable, which is exactly why the visitor can see the
      // thread they are about to be refused in.
      return json(route, 200, { data: [
        wire("m1", "Hi, I need help with an order", "visitor", 1),
        wire("m2", "An agent will be with you shortly", "agent", 2),
      ] });
    }
    // The production error, verbatim.
    return json(route, 403, {
      error: { code: "forbidden", message: "This conversation does not belong to you.", request_id: "req_regression" },
    });
  }
  return json(route, 404, { error: { code: "not_found", message: "Not found." } });
});

/* ------------------------------------------------------------------ helpers */
await page.goto(`http://127.0.0.1:${PORT}/`, { waitUntil: "domcontentloaded", timeout: 30000 });

const evalIn = (body, arg) => page.evaluate(body, arg);
const mounted = () => evalIn(`!!document.getElementById("hubchat-widget")?.shadowRoot`);
for (let i = 0; i < 24 && !(await mounted()); i++) await page.waitForTimeout(250);
if (!(await mounted())) {
  await browser.close();
  server.close();
  console.error("FAILED: the widget never mounted");
  process.exit(1);
}

const clickButton = (re) =>
  evalIn(`m => {
    const r = document.getElementById("hubchat-widget")?.shadowRoot;
    if (!r) return "NO_ROOT";
    const b = [...r.querySelectorAll("button")].find((x) =>
      new RegExp(m, "i").test((x.textContent || "") + " " + (x.getAttribute("aria-label") || "")));
    if (!b) return "NOT_FOUND";
    b.click();
    return "clicked";
  }`, re);

const hasComposer = () => evalIn(`!!document.getElementById("hubchat-widget")?.shadowRoot?.querySelector("textarea")`);
for (let i = 0; i < 8 && !(await hasComposer()); i++) {
  await page.locator('#hubchat-widget button[aria-label="Open support"]').first().click().catch(() => {});
  await page.waitForTimeout(400);
}
if (!(await hasComposer())) failures.push("composer never appeared, so no message could be attempted");

const historyVisible = await evalIn(
  `(document.getElementById("hubchat-widget")?.shadowRoot?.textContent || "").includes("I need help with an order")`,
);
console.log(`bundle under test:      ${bundleOrigin || "local build (embedded/assets/widget/app.js)"}`);
console.log(`previous chat visible to the visitor: ${historyVisible}`);
if (!historyVisible) failures.push("history did not render, so the reproduction scenario is not set up");

/**
 * Real keystrokes through the shadow root. Dispatching a synthetic `input`
 * event does not reach React's controlled textarea here, so the draft stayed
 * empty and the send button never enabled — the harness has to type like a
 * visitor would.
 */
const sendReal = async (text) => {
  const composer = page.locator("#hubchat-widget textarea").first();
  await composer.click();
  await composer.pressSequentially(text, { delay: 8 });
  await page.waitForTimeout(200);
  const send = page.locator('#hubchat-widget button[aria-label="Send request"]').first();
  await send.click();
  await page.waitForTimeout(300);
};

await sendReal("this is my follow up question");
await page.waitForTimeout(2500);

/* -------------------------------------------------------------- assertions */
const bubbleText = await evalIn(`(() => {
  const r = document.getElementById("hubchat-widget")?.shadowRoot;
  if (!r) return "";
  return [...r.querySelectorAll("*")]
    .map((el) => (el.textContent || "").trim())
    .filter((t) => t.startsWith("this is my follow up question"))
    .join(" | ");
})()`);

const stored = JSON.parse((await evalIn(`window.localStorage.getItem("${KEY}")`)) || "null");

const rejectedAt = calls.find((c) => c.method === "POST" && c.path === "/conversations/cnv_old/messages" && c.status === 403)?.n;
const startedNew = calls.find((c) => c.method === "POST" && c.path === "/conversations");
const recovered = startedNew && rejectedAt && startedNew.n > rejectedAt;
const freshToken = issued.find((t) => t.at > (rejectedAt ?? Infinity));
const usedFreshToken = freshToken && calls.some((c) => c.token === freshToken.token && c.n > (rejectedAt ?? Infinity));
const retryUsedOldToken = calls.some((c) => c.n > (rejectedAt ?? Infinity) && c.token === "tok_original");

console.log("\n--- widget API calls ---");
for (const c of calls) console.log(`  ${String(c.n).padStart(2)} ${c.method} ${c.path}${c.token ? "  token=" + c.token : ""}${c.status ? "  -> " + c.status : ""}`);
console.log(`\nbubble text:            ${JSON.stringify(bubbleText.slice(0, 120))}`);
console.log(`stuck in a failed state: ${/failed|not delivered|could not send/i.test(bubbleText)}`);
console.log(`delivered after retry:   ${/sent/i.test(bubbleText)}`);
console.log(`retried with a fresh visitor token: ${Boolean(usedFreshToken)}`);
console.log(`reused the rejected token after recovery: ${retryUsedOldToken}`);
console.log(`started a replacement conversation:      ${Boolean(recovered)}`);
console.log(`stored session afterwards: ${JSON.stringify(stored)}`);

if (!rejectedAt) failures.push("the rejected write never happened, so this run proves nothing");
if (/failed|not delivered|could not send/i.test(bubbleText)) failures.push("the visitor's message is stuck in a failed state");
if (!/sent/i.test(bubbleText)) failures.push("the visitor's message was never delivered after the refused send");
if (!usedFreshToken) failures.push("no fresh visitor token was issued after the rejection, so recovery never happened");
if (retryUsedOldToken) failures.push("the rejected token was reused after recovery");
if (!recovered) failures.push("no replacement conversation was started");
if (stored?.conversationId === "cnv_old") failures.push("stored session still points at the unusable conversation");
if (stored?.token === "tok_original") failures.push("stored session still holds the rejected token");
if (stored?.conversationId && stored.conversationId !== "cnv_old" && issued.length) {
  const anyIssued = issued.some((t) => t.token === stored.token);
  if (!anyIssued) failures.push("stored session holds a token this run never issued");
}

await browser.close();
server.close();

if (failures.length) {
  console.error("\nFAILED:");
  for (const f of failures) console.error(` - ${f}`);
  process.exit(1);
}
console.log("\nWidget send recovery OK — visitor recovers automatically from a refused thread");