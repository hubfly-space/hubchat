import { StrictMode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { Widget } from "./panel/Widget";
import "./styles.css";
import styles from "./styles.css?inline";
import type { WidgetConfig } from "./types";

/**
 * Widget entry point.
 *
 * Mounted into a closed-ish shadow root attached to a single container element.
 * That gives us three things the host page cannot take away:
 *
 *   · our styles do not leak out, and theirs do not leak in
 *   · `!important` rules on their `div` selectors cannot reach us
 *   · we occupy exactly one node in their DOM, which is easy to reason about
 *
 * The one thing a shadow root does not isolate is stacking context, hence the
 * explicit z-index from the workspace configuration.
 */

export type WidgetHandle = {
  handle: (method: string, payload?: unknown) => void;
  destroy: () => void;
};

let root: Root | null = null;
let container: HTMLElement | null = null;

const listeners = new Map<string, ((payload: unknown) => void)[]>();

// Keep the widget usable when a host CSP blocks the inline stylesheet, a
// browser cannot adopt a constructable sheet, or a legacy CSS parser drops
// part of the generated utility layer. The compiled sheet remains the normal
// design system; this is intentionally limited to the reset and geometry that
// prevent the unstyled serif/default-controls failure mode.
const criticalStyles = `
:host {
  all: initial;
  font-family: Inter, ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
  font-size: 13.5px;
  line-height: 1.55;
}
[data-hubchat-widget-root], [data-hubchat-widget-root] * { box-sizing: border-box; }
[data-hubchat-widget-root] {
  color: #0a0b0d;
  font-family: Inter, ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
  font-size: 13.5px;
  line-height: 1.55;
}
[data-hubchat-widget-root][data-theme="dark"] { color: #f7f8fa; background: #0f1114; }
[data-hubchat-widget-root] button,
[data-hubchat-widget-root] input,
[data-hubchat-widget-root] textarea,
[data-hubchat-widget-root] select { font: inherit; }
[data-hubchat-widget-root] button { cursor: pointer; }
[data-hubchat-widget-panel] {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: #fff;
  border: 1px solid rgba(10, 11, 13, .12);
  box-shadow: 0 8px 24px rgba(16, 20, 28, .18);
}
[data-hubchat-widget-root][data-theme="dark"] [data-hubchat-widget-panel] {
  background: #171a20;
  border-color: rgba(255, 255, 255, .12);
  box-shadow: 0 12px 32px rgba(0, 0, 0, .5);
}
[data-hubchat-widget-header] { flex: 0 0 auto; }
[data-hubchat-widget-timeline] { min-height: 0; flex: 1 1 auto; overflow-y: auto; }
[data-hubchat-widget-composer] { flex: 0 0 auto; }
[data-hubchat-widget-launcher] { display: flex; align-items: center; justify-content: center; }
[data-hubchat-widget-composer] textarea { min-width: 0; resize: none; border: 0; outline: 0; }
`;

export function mount({
  host,
  config,
}: {
  host: string;
  config: { key: string; remote: WidgetConfig };
}): Promise<WidgetHandle> {
  if (container) return Promise.resolve(handle);

  container = document.createElement("div");
  container.id = "hubchat-widget";
  container.style.position = "fixed";
  container.style.zIndex = String(config.remote?.appearance.z_index ?? 2_147_483_000);
  container.style.inset = "0";
  // The container spans the viewport so the panel can position itself, but it
  // must not swallow clicks meant for the host page. Interactive descendants
  // re-enable pointer events themselves.
  container.style.pointerEvents = "none";
  document.body.appendChild(container);

  const shadow = container.attachShadow({ mode: "open" });

  // The inline sheet is the fastest path, but some host CSPs reject style
  // elements and older embedded browsers do not implement constructable
  // stylesheets. The library build emits the same compiled CSS as
  // `/widget/app.css`; a stylesheet link gives those browsers a standards-
  // based fallback without allowing host-page CSS to cross the shadow root.
  const externalStyles = document.createElement("link");
  externalStyles.rel = "stylesheet";
  // The production binary serves the compiled stable asset. Vite's dev
  // harness serves source modules instead; asking it for /widget/app.css
  // falls through to index.html, which the browser rejects as a stylesheet
  // and makes CSP/older-browser fallback testing look like an unstyled widget.
  const stylesheetPath = import.meta.env.DEV ? "/widget/src/styles.css?direct" : "/widget/app.css";
  externalStyles.href = new URL(stylesheetPath, host).href;
  externalStyles.setAttribute("data-hubchat-widget-external-styles", "true");
  shadow.appendChild(externalStyles);

  // Keep the style tag as the portable path. Some embedded browsers expose
  // constructable stylesheets but do not reliably apply them to a shadow root
  // (especially after navigation or when the widget is injected late). A
  // failed stylesheet must never turn into an unstyled widget.
  const style = document.createElement("style");
  style.setAttribute("data-hubchat-widget-styles", "true");
  style.textContent = `${styles}\n${criticalStyles}`;
  shadow.appendChild(style);

  // A host page with `style-src 'self'` can block the inline style element even
  // though the widget script itself is allowed to run. Browsers can still
  // expose a blocked sheet and its rules, so checking `style.sheet` is not a
  // reliable signal that the rules are active. Constructable sheets do not use
  // an inline style element; install one whenever supported so a strict host
  // CSP cannot silently produce the unstyled panel shown by the host page.
  if (
    "adoptedStyleSheets" in shadow &&
    typeof CSSStyleSheet !== "undefined" &&
    "replaceSync" in CSSStyleSheet.prototype
  ) {
    try {
      const sheet = new CSSStyleSheet();
      sheet.replaceSync(`${styles}\n${criticalStyles}`);
      shadow.adoptedStyleSheets = [...shadow.adoptedStyleSheets, sheet];
    } catch {
      // The style tag remains the only portable option on older browsers.
    }
  }

  const mountPoint = document.createElement("div");
  mountPoint.setAttribute("data-density", "comfortable");
  shadow.appendChild(mountPoint);

  // v1.js replays any commands queued before the interface finished loading
  // (`boot` immediately followed by `show`, a "trigger: immediate" widget)
  // the moment this promise resolves. React gives no guarantee that Widget's
  // effects — where its own "hubchat:command" listener gets attached — have
  // run by the time `root.render()` returns, so waiting for that call to
  // return is not enough: a command dispatched before the listener exists is
  // simply lost. Listening for Widget's own "mounted" signal *before*
  // rendering at all, and resolving only once it fires, closes that window
  // regardless of which tick React chooses to run the effect in.
  return new Promise((resolve) => {
    window.addEventListener(
      "hubchat:internal:mounted",
      () => resolve(handle),
      { once: true },
    );

    root = createRoot(mountPoint);
    root.render(
      <StrictMode>
        <Widget
          host={host}
          publicKey={config.key}
          config={config.remote}
          onEvent={(name, payload) => emit(name, payload)}
        />
      </StrictMode>,
    );
  });
}

const commands = new Set([
	"show",
	"open",
	"hide",
	"close",
  "toggle",
  "identify",
  "context",
  "update",
  "track",
  "startConversation",
  "openArticle",
  "openForm",
  "openTicketForm",
  "openFeedback",
  "openFeedbackForm",
  "reset",
  "on",
]);

const handle: WidgetHandle = {
  handle(method, payload) {
    if (!commands.has(method)) {
      console.warn(`[hubchat] unknown method "${method}"`);
      return;
    }

    if (method === "on") {
      const { event, handler } = payload as { event: string; handler: (p: unknown) => void };
      listeners.set(event, [...(listeners.get(event) ?? []), handler]);
      return;
    }

    // Commands are dispatched through a DOM event rather than a module-level
    // callback so the React tree owns its own state and this file stays a thin
    // adapter between the loader's imperative API and the component.
    window.dispatchEvent(new CustomEvent("hubchat:command", { detail: { method, payload } }));
  },

  destroy() {
    root?.unmount();
    container?.remove();
    root = null;
    container = null;
    listeners.clear();
  },
};

function emit(name: string, payload: unknown) {
  for (const listener of listeners.get(name) ?? []) listener(payload);
}
