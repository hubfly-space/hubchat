import { StrictMode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { Widget } from "./panel/Widget";
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

export function mount({
  host,
  config,
}: {
  host: string;
  config: { key: string; remote?: WidgetConfig };
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

  // Adopted stylesheets avoid a <style> node and are cheaper to apply.
  if ("adoptedStyleSheets" in Document.prototype) {
    const sheet = new CSSStyleSheet();
    sheet.replaceSync(styles);
    shadow.adoptedStyleSheets = [sheet];
  } else {
    const style = document.createElement("style");
    style.textContent = styles;
    shadow.appendChild(style);
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
          config={config.remote ?? FALLBACK_CONFIG}
          onEvent={(name, payload) => emit(name, payload)}
        />
      </StrictMode>,
    );
  });
}

const commands = new Set([
  "show",
  "hide",
  "toggle",
  "identify",
  "update",
  "track",
  "startConversation",
  "openArticle",
  "openForm",
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

/** Used only when the configuration request failed but the host called show(). */
const FALLBACK_CONFIG: WidgetConfig = {
  enabled: true,
  appearance: {
    accent: "#3B6EF6",
    theme: "auto",
    launcher_shape: "circle",
    launcher_size: "md",
    launcher_label: null,
    position: "bottom-right",
    offset_x: 20,
    offset_y: 20,
    panel_width: 384,
    panel_height: 620,
    radius: 14,
    header_style: "solid",
    bubble_style: "rounded",
    z_index: 2_147_483_000,
    hide_branding: false,
  },
  content: {
    title: "Support",
    subtitle: "We usually reply in a few minutes",
    welcome_message: "Hi there 👋 How can we help?",
    input_placeholder: "Write a message…",
    response_time_text: "",
    offline_message: "We're offline — leave a message and we'll reply by email.",
  },
  behavior: { trigger: "manual", delay_seconds: 0, scroll_percent: 0, allow_anonymous: true, persist_conversation: false },
  modes: ["chat"],
  online: true,
  articles: [],
};
