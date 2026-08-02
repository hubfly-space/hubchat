import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Small browser acceptance runner with no package or browser download.
 *
 * The release image is deliberately only a Go binary. This check uses the
 * machine's installed Chrome through its DevTools protocol and is therefore
 * an opt-in development/CI gate, not a runtime dependency of Hubchat.
 */

const DEFAULT_TIMEOUT = 20_000;

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

async function fetchJSON(url, options) {
  const response = await fetch(url, options);
  const text = await response.text();
  if (!response.ok) throw new Error(`${options?.method ?? "GET"} ${url}: ${response.status} ${text.slice(0, 500)}`);
  return JSON.parse(text);
}

function reservePort() {
  return new Promise((resolve, reject) => {
    const server = createServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      if (!address || typeof address === "string") {
        server.close();
        reject(new Error("could not reserve a browser debugging port"));
        return;
      }
      server.close((error) => (error ? reject(error) : resolve(address.port)));
    });
  });
}

async function waitFor(fn, label, timeout = DEFAULT_TIMEOUT) {
  const deadline = Date.now() + timeout;
  let lastError;
  while (Date.now() < deadline) {
    try {
      const value = await fn();
      if (value) return value;
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(`timed out waiting for ${label}${lastError ? `: ${lastError.message}` : ""}`);
}

class DevToolsPage {
  constructor(socketURL) {
    this.socketURL = socketURL;
    this.nextID = 1;
    this.pending = new Map();
    this.socket = null;
  }

  async connect() {
    this.socket = new WebSocket(this.socketURL);
    await new Promise((resolve, reject) => {
      const timeout = setTimeout(() => reject(new Error("timed out connecting to Chrome DevTools")), DEFAULT_TIMEOUT);
      this.socket.addEventListener("open", () => {
        clearTimeout(timeout);
        resolve();
      }, { once: true });
      this.socket.addEventListener("error", () => {
        clearTimeout(timeout);
        reject(new Error("Chrome DevTools WebSocket failed"));
      }, { once: true });
    });
    this.socket.addEventListener("message", async (event) => {
      let text = event.data;
      if (typeof text !== "string") {
        if (text instanceof ArrayBuffer) text = Buffer.from(text).toString("utf8");
        else if (ArrayBuffer.isView(text)) text = Buffer.from(text.buffer).toString("utf8");
        else text = String(text);
      }
      const message = JSON.parse(text);
      if (!message.id) return;
      const waiter = this.pending.get(message.id);
      if (!waiter) return;
      this.pending.delete(message.id);
      if (message.error) waiter.reject(new Error(message.error.message));
      else waiter.resolve(message.result ?? {});
    });
    await this.send("Page.enable");
    await this.send("Runtime.enable");
    await this.send("Network.enable");
  }

  send(method, params = {}) {
    const id = this.nextID++;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.socket.send(JSON.stringify({ id, method, params }));
    });
  }

  async evaluate(expression) {
    const result = await this.send("Runtime.evaluate", {
      expression,
      awaitPromise: true,
      returnByValue: true,
      userGesture: true,
    });
    if (result.exceptionDetails) {
      throw new Error(result.exceptionDetails.text ?? "browser expression failed");
    }
    return result.result?.value;
  }

  async navigate(url) {
    await this.send("Page.navigate", { url });
    await waitFor(
      () => this.evaluate("document.readyState === 'complete' || document.readyState === 'interactive'"),
      `browser navigation to ${url}`,
    );
  }

  async close() {
    for (const waiter of this.pending.values()) waiter.reject(new Error("browser closed"));
    this.pending.clear();
    this.socket?.close();
  }
}

async function launchChrome() {
  const debuggingPort = await reservePort();
  const profile = await mkdtemp(join(tmpdir(), "hubchat-browser-"));
  const executable = process.env.HUBCHAT_CHROME_PATH ?? "google-chrome";
  const chrome = spawn(executable, [
    "--headless=new",
    "--no-sandbox",
    "--disable-gpu",
    "--disable-dev-shm-usage",
    "--disable-web-security",
    "--ignore-certificate-errors",
    `--user-data-dir=${profile}`,
    `--remote-debugging-address=127.0.0.1`,
    `--remote-debugging-port=${debuggingPort}`,
    "about:blank",
  ], { stdio: ["ignore", "ignore", "ignore"] });

  const versionURL = `http://127.0.0.1:${debuggingPort}/json/version`;
  const version = await waitFor(async () => {
    try {
      return await fetchJSON(versionURL);
    } catch {
      return null;
    }
  }, "Chrome DevTools endpoint");
  const target = await fetchJSON(`http://127.0.0.1:${debuggingPort}/json/new?about:blank`, { method: "PUT" });
  const page = new DevToolsPage(target.webSocketDebuggerUrl);
  await page.connect();

  return {
    page,
    async close() {
      await page.close();
      if (!chrome.killed) chrome.kill("SIGTERM");
      if (chrome.exitCode === null) {
        await Promise.race([
          new Promise((resolve) => chrome.once("exit", resolve)),
          new Promise((resolve) => setTimeout(resolve, 5_000)),
        ]);
      }
      for (let attempt = 0; attempt < 5; attempt += 1) {
        try {
          await rm(profile, { recursive: true, force: true });
          break;
        } catch (error) {
          if (attempt === 4) throw error;
          await new Promise((resolve) => setTimeout(resolve, 100));
        }
      }
      // Keep the version read above observable to avoid treating a stale
      // /json response as a successful launch in diagnostics.
      void version.webSocketDebuggerUrl;
    },
  };
}

async function startHostPage({ baseURL, publicKey }) {
  const widgetScript = new URL("/widget/v1.js", baseURL).href;
  const widgetOrigin = new URL(baseURL).origin;
  const encodedKey = JSON.stringify(publicKey);
  const html = `<!doctype html>
<html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'self'; script-src 'self' 'unsafe-inline' ${widgetOrigin}; style-src 'self' ${widgetOrigin}; connect-src *; img-src 'self' data: ${widgetOrigin}; font-src 'self' data: ${widgetOrigin}"><link rel="stylesheet" href="/host.css"></head><body>
  <main><h1>Host page</h1><button id="host-open" type="button">Open support</button></main>
  <script>
    window.__hubchatReady = false;
    window.__hubchatKey = ${encodedKey};
    window.__hubchatEvents = [];
    window.addEventListener("error", function (event) { window.__hubchatEvents.push("error:" + event.message); });
    window.addEventListener("unhandledrejection", function (event) { window.__hubchatEvents.push("rejection:" + String(event.reason)); });
  </script>
  <script src=${JSON.stringify(widgetScript)}></script>
  <script>
    Hubchat("on", { event: "ready", handler: function () { window.__hubchatReady = true; } });
    document.querySelector("#host-open").addEventListener("click", function () { Hubchat("show"); });
    Hubchat("boot", { key: ${encodedKey} });
    // Exercise the real pre-config command queue: the first interaction is
    // intentionally issued in the same task as boot().
    Hubchat("show");
  </script>
</body></html>`;
  const server = createServer((request, response) => {
    if (request.url === "/" || request.url === "/journey-page") {
      response.writeHead(200, { "content-type": "text/html; charset=utf-8" });
      response.end(html);
      return;
    }
    if (request.url === "/host.css") {
      response.writeHead(200, { "content-type": "text/css; charset=utf-8" });
      response.end("* { box-sizing: content-box } body { margin: 0; font: 32px Georgia, serif; line-height: 2.4; background: #f4f1ea; color: #2a2622 } button { background: hotpink !important; border: 8px solid red !important; font-size: 32px !important; color: black !important }");
      return;
    }
    response.writeHead(204);
    response.end();
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  assert(address && typeof address !== "string", "could not start browser host page");
  return {
    url: `http://127.0.0.1:${address.port}/journey-page`,
    close: () => new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve()))),
  };
}

export async function runBrowserJourney({
  baseURL,
  publicKey,
  portalID,
  portalCookie,
  portalFormSlug,
  viewerName = "Journey Customer",
  dashboardCookie,
  workspaceName = "Production Journey Workspace",
}) {
  assert(baseURL, "browser journey baseURL is required");
  assert(publicKey, "browser journey public key is required");

  const host = await startHostPage({ baseURL, publicKey });
  const browser = await launchChrome();
  try {
    await browser.page.navigate(host.url);
    try {
      await waitFor(() => browser.page.evaluate("window.__hubchatReady === true"), "widget ready event");
    } catch (error) {
      const diagnostics = await browser.page.evaluate(`(async () => ({
        ready: window.__hubchatReady,
        hubchatType: typeof window.Hubchat,
        container: Boolean(document.querySelector("#hubchat-widget")),
        events: window.__hubchatEvents,
        resources: performance.getEntriesByType("resource").map((entry) => entry.name).filter((name) => name.includes("widget")),
        config: await fetch(${JSON.stringify(new URL("/api/v1/widget/config", baseURL).href)} + "?key=" + encodeURIComponent(window.__hubchatKey) + "&url=" + encodeURIComponent(location.href), { credentials: "omit" }).then(async (response) => ({ status: response.status, body: await response.text() })).catch((error) => ({ error: String(error) })),
      }))()`);
      throw new Error(`${error.message}; browser diagnostics: ${JSON.stringify(diagnostics)}`);
    }

    const widgetState = await browser.page.evaluate(`(() => {
      const hostButton = document.querySelector("#host-open");
      const container = document.querySelector("#hubchat-widget");
      const shadow = container?.shadowRoot;
      const launcher = shadow?.querySelector('button[aria-label="Open support"], button[aria-label="Close support"]');
      if (!hostButton || !container || !shadow || !launcher) return null;
      const hostStyle = getComputedStyle(hostButton);
      const widgetStyle = getComputedStyle(launcher);
      return {
        hostBackground: hostStyle.backgroundColor,
        hostFontSize: hostStyle.fontSize,
        widgetBackground: widgetStyle.backgroundColor,
        widgetFontSize: widgetStyle.fontSize,
        widgetFontFamily: widgetStyle.fontFamily,
        widgetWidth: widgetStyle.width,
        hasStylesheet: Boolean(shadow.querySelector('style[data-hubchat-widget-styles]')),
        hasExternalStylesheet: Boolean(shadow.querySelector('link[data-hubchat-widget-external-styles]')?.sheet),
        stylesheetRules: shadow.querySelector('style[data-hubchat-widget-styles]')?.sheet?.cssRules.length ?? 0,
        supportsConstructableStylesheets: typeof CSSStyleSheet !== "undefined" && "replaceSync" in CSSStyleSheet.prototype && "adoptedStyleSheets" in (shadow ?? {}),
        constructableStylesheetCount: shadow?.adoptedStyleSheets?.length ?? 0,
        launcherLabel: launcher.getAttribute("aria-label"),
      };
    })()`);
    assert(widgetState, "widget did not mount inside its shadow root");
    assert(widgetState.hostBackground === "rgb(255, 105, 180)", "host CSS regression: hostile button background changed");
    assert(widgetState.hostFontSize === "32px", "host CSS regression: hostile button font size changed");
    assert(widgetState.widgetBackground === "rgb(59, 110, 246)", "widget CSS regression: accent style did not load");
    assert(widgetState.widgetFontSize !== "32px", "widget CSS regression: host font leaked into shadow root");
    assert(widgetState.widgetFontFamily.includes("Inter"), "widget CSS regression: widget font reset did not load");
    assert(widgetState.hasStylesheet, "widget CSS was not installed in the shadow root");
    assert(widgetState.hasExternalStylesheet, "widget external CSS fallback did not load under host CSP");
    assert(widgetState.stylesheetRules > 0 || widgetState.widgetFontFamily.includes("Inter"), "widget CSS regression: installed stylesheet contains no usable rules");
    assert(!widgetState.supportsConstructableStylesheets || widgetState.constructableStylesheetCount > 0, "widget CSS regression: constructable stylesheet was not installed");

    const alreadyOpen = await browser.page.evaluate("Boolean(document.querySelector('#hubchat-widget')?.shadowRoot?.querySelector('[role=dialog]'))");
    if (!alreadyOpen) await browser.page.evaluate("document.querySelector('#host-open').click()");
    await waitFor(() => browser.page.evaluate("Boolean(document.querySelector('#hubchat-widget')?.shadowRoot?.querySelector('[role=dialog]'))"), "widget dialog to open");
    const dialogState = await browser.page.evaluate(`(() => {
      const dialog = document.querySelector('#hubchat-widget')?.shadowRoot?.querySelector('[role="dialog"]');
      const close = document.querySelector('#hubchat-widget')?.shadowRoot?.querySelector('button[aria-label="Close support"]');
      return { label: dialog?.getAttribute("aria-label") ?? "", closeFocusable: Boolean(close && close.tabIndex >= 0) };
    })()`);
    assert(dialogState?.label, "widget dialog has no accessible label");
    assert(dialogState.closeFocusable, "widget close control is not keyboard-focusable");

    // The visitor session is browser-local by design. Prove the real customer
    // journey, not only the HTTP endpoint: start a conversation, wait until
    // the widget has persisted its conversation id, reload the host page, and
    // require the widget to reopen on the same chat with the same message.
    const persistenceBody = `Browser persistence check ${Date.now()}`;
    await browser.page.evaluate(`(() => {
      const shadow = document.querySelector('#hubchat-widget')?.shadowRoot;
      const button = [...(shadow?.querySelectorAll('button') ?? [])].find((item) => item.textContent?.includes('Start a conversation'));
      if (!button) throw new Error('widget start conversation control is missing');
      button.click();
    })()`);
    await waitFor(() => browser.page.evaluate(`Boolean(document.querySelector('#hubchat-widget')?.shadowRoot?.querySelector('textarea[aria-label="Message"]'))`), "widget conversation composer");
    await browser.page.evaluate(`(() => {
      const textarea = document.querySelector('#hubchat-widget')?.shadowRoot?.querySelector('textarea[aria-label="Message"]');
      if (!textarea) throw new Error('widget message composer is missing');
      const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set;
      setter?.call(textarea, ${JSON.stringify(persistenceBody)});
      textarea.dispatchEvent(new Event('input', { bubbles: true }));
      textarea.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', code: 'Enter', bubbles: true }));
    })()`);
    await waitFor(() => browser.page.evaluate(`(() => {
      const raw = Object.entries(localStorage).find(([key]) => key.startsWith('hubchat.visitor.'))?.[1];
      if (!raw) return false;
      try { return Boolean(JSON.parse(raw).conversationId); } catch { return false; }
    })()`), "persisted widget conversation");
    await browser.page.navigate(host.url);
    await waitFor(() => browser.page.evaluate("window.__hubchatReady === true"), "widget ready after host reload");
    await waitFor(() => browser.page.evaluate(`Boolean(document.querySelector('#hubchat-widget')?.shadowRoot?.querySelector('[role="dialog"]'))`), "widget dialog after host reload");
    await waitFor(() => browser.page.evaluate(`Boolean(document.querySelector('#hubchat-widget')?.shadowRoot?.textContent?.includes(${JSON.stringify(persistenceBody)}))`), "restored widget conversation message");
    const persistenceState = await browser.page.evaluate(`(() => {
      const shadow = document.querySelector('#hubchat-widget')?.shadowRoot;
      const body = shadow?.textContent ?? '';
      return { hasMessage: body.includes(${JSON.stringify(persistenceBody)}), hasComposer: Boolean(shadow?.querySelector('textarea[aria-label="Message"]')) };
    })()`);
    assert(persistenceState.hasMessage && persistenceState.hasComposer, "widget did not restore the existing conversation after host reload");

    let portalFormsChecked = false;
    if (portalID && portalCookie) {
      const cookieResult = await browser.page.send("Network.setCookie", {
        name: "hubchat_portal_session",
        value: portalCookie,
        path: "/",
        url: new URL("/portal/", baseURL).href,
        httpOnly: true,
      });
      assert(cookieResult.success !== false, "Chrome refused the portal session cookie");
      await browser.page.navigate(new URL(`/portal/?portal=${encodeURIComponent(portalID)}`, baseURL).href);
      try {
        await waitFor(() => browser.page.evaluate("document.querySelector('#content') && document.body.innerText.includes('How can we help?')"), "portal content");
        const viewer = await browser.page.evaluate(`fetch("/api/v1/portal/me?portal=${encodeURIComponent(portalID)}", { credentials: "include" }).then((response) => response.json())`);
        assert(viewer?.viewer?.name === viewerName, "portal page did not retain the authenticated viewer");
      } catch (error) {
        const diagnostics = await browser.page.evaluate(`({
          location: location.href,
          root: Boolean(document.querySelector("#root")),
          content: Boolean(document.querySelector("#content")),
          body: document.body.innerText.slice(0, 500),
          resources: performance.getEntriesByType("resource").map((entry) => entry.name).filter((name) => name.includes("portal") || name.includes("api/v1/portal")),
        })`);
        const cookieDiagnostics = await browser.page.send("Network.getAllCookies");
        const portalMe = await browser.page.evaluate(`fetch("/api/v1/portal/me?portal=${encodeURIComponent(portalID)}", { credentials: "include" }).then(async (response) => ({ status: response.status, body: await response.text() })).catch((fetchError) => ({ error: String(fetchError) }))`);
        diagnostics.cookies = (cookieDiagnostics.cookies ?? []).filter((cookie) => cookie.name === "hubchat_portal_session").map((cookie) => ({ domain: cookie.domain, path: cookie.path, secure: cookie.secure, httpOnly: cookie.httpOnly, sameSite: cookie.sameSite }));
        diagnostics.portalMe = portalMe;
        throw new Error(`${error.message}; portal diagnostics: ${JSON.stringify(diagnostics)}`);
      }
      await browser.page.evaluate(`(() => {
        const link = [...document.querySelectorAll("a")].find((item) => item.getAttribute("href")?.includes("/tickets"));
        if (!link) throw new Error("portal tickets navigation link is missing");
        link.click();
      })()`);
      await waitFor(() => browser.page.evaluate("location.pathname.endsWith('/portal/tickets')"), "portal tickets navigation");
      const portalState = await browser.page.evaluate("({ main: Boolean(document.querySelector('#content')), body: document.body.innerText })");
      assert(portalState.main, "portal tickets route did not render its main content");
      assert(/request|ticket/i.test(portalState.body), "portal tickets route rendered no ticket/request content");

      if (portalFormSlug) {
        await browser.page.navigate(new URL(`/portal/forms?portal=${encodeURIComponent(portalID)}`, baseURL).href);
        await waitFor(() => browser.page.evaluate("location.pathname.endsWith('/portal/forms') && Boolean(document.querySelector('#content'))"), "portal forms navigation");
        await waitFor(() => browser.page.evaluate("document.body.innerText.includes('Production journey request form')"), "live portal forms directory");
        const formsState = await browser.page.evaluate("({ main: Boolean(document.querySelector('#content')), body: document.body.innerText })");
        assert(formsState.main, "portal forms route did not render its main content");
        assert(formsState.body.includes("Production journey request form"), "portal forms directory did not render the live journey form");

        const detailHref = `/forms/${encodeURIComponent(portalFormSlug)}`;
        await browser.page.evaluate(`(() => {
          const link = [...document.querySelectorAll("a")].find((item) => item.getAttribute("href")?.includes(${JSON.stringify(detailHref)}));
          if (!link) throw new Error("portal form detail link is missing");
          link.click();
        })()`);
        await waitFor(() => browser.page.evaluate(`location.pathname.endsWith(${JSON.stringify(`/portal${detailHref}`)}) && Boolean(document.querySelector('#content'))`), "portal form detail navigation");
        await waitFor(() => browser.page.evaluate("document.body.innerText.includes('Request type') && document.body.innerText.includes('Attachment')"), "live portal form detail");
        const formState = await browser.page.evaluate("({ body: document.body.innerText, hasAttachmentInput: Boolean(document.querySelector('input[type=file]')), hasSendButton: [...document.querySelectorAll('button')].some((item) => item.textContent?.includes('Send response')) })");
        assert(formState.body.includes("Request type") && formState.body.includes("Attachment"), "portal form detail did not render its live fields");
        assert(formState.hasAttachmentInput, "portal form detail did not render the attachment input");
        assert(formState.hasSendButton, "portal form detail did not render its submit action");
        portalFormsChecked = true;
      }
    }

    let dashboardChecked = false;
    if (dashboardCookie) {
      const dashboardCookieResult = await browser.page.send("Network.setCookie", {
        name: "hubchat_session",
        value: dashboardCookie,
        path: "/",
        url: new URL("/app/", baseURL).href,
        httpOnly: true,
      });
      assert(dashboardCookieResult.success !== false, "Chrome refused the dashboard session cookie");
      await browser.page.navigate(new URL("/app/", baseURL).href);
      await waitFor(
        () => browser.page.evaluate(`document.querySelector("#main") && document.body.innerText.includes(${JSON.stringify(`Good afternoon, ${workspaceName}`)})`),
        "authenticated dashboard overview",
      );

      const dashboardState = await browser.page.evaluate(`(() => {
        const skip = document.querySelector('a[href="#main"]');
        const main = document.querySelector("#main");
        const workspace = document.querySelector('button[aria-label^="Workspace:"]');
        const search = [...document.querySelectorAll("button")].find((item) => item.textContent?.includes("Search conversations"));
        skip?.focus();
        return {
          hasSkipLink: Boolean(skip),
          skipFocused: document.activeElement === skip,
          hasMain: Boolean(main),
          workspaceLabel: workspace?.getAttribute("aria-label") ?? "",
          hasSearch: Boolean(search),
        };
      })()`);
      assert(dashboardState.hasSkipLink && dashboardState.skipFocused, "dashboard skip link is not keyboard-focusable");
      assert(dashboardState.hasMain, "dashboard main landmark is missing");
      assert(dashboardState.workspaceLabel.includes(workspaceName), "dashboard workspace switcher did not identify the active tenant");
      assert(dashboardState.hasSearch, "dashboard global search control is missing");

      await browser.page.navigate(new URL("/app/inbox", baseURL).href);
      await waitFor(() => browser.page.evaluate("location.pathname.endsWith('/app/inbox') && Boolean(document.querySelector('#main'))"), "dashboard inbox navigation");
      dashboardChecked = true;
    }

    return { widgetState, dialogState, widgetPersistenceChecked: true, portalChecked: Boolean(portalID && portalCookie), portalFormsChecked: Boolean(portalID && portalCookie && portalFormSlug), dashboardChecked };
  } finally {
    await browser.close();
    await host.close();
  }
}

const isMain = process.argv[1] && fileURLToPath(import.meta.url) === resolve(process.argv[1]);
if (isMain) {
  try {
    const result = await runBrowserJourney({
      baseURL: process.env.HUBCHAT_BROWSER_BASE_URL,
      publicKey: process.env.HUBCHAT_BROWSER_PUBLIC_KEY,
      portalID: process.env.HUBCHAT_BROWSER_PORTAL_ID,
      portalCookie: process.env.HUBCHAT_BROWSER_PORTAL_COOKIE,
      portalFormSlug: process.env.HUBCHAT_BROWSER_PORTAL_FORM_SLUG,
      viewerName: process.env.HUBCHAT_BROWSER_VIEWER_NAME ?? "Journey Customer",
      dashboardCookie: process.env.HUBCHAT_BROWSER_DASHBOARD_COOKIE,
      workspaceName: process.env.HUBCHAT_BROWSER_WORKSPACE_NAME ?? "Production Journey Workspace",
    });
    console.log(`Browser journey OK (widget CSS isolation, accessible dialog, conversation persistence${result.portalChecked ? ", portal navigation" : ""}${result.dashboardChecked ? ", dashboard navigation" : ""})`);
  } catch (error) {
    console.error(`Browser journey failed: ${error instanceof Error ? error.message : String(error)}`);
    process.exitCode = 1;
  }
}
