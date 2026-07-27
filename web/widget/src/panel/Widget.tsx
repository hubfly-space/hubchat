import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowLeft,
  Book,
  ChevronRight,
  MessageSquare,
  MessageSquarePlus,
  Minus,
  Paperclip,
  Search,
  Send,
  Ticket,
  X,
} from "lucide-react";
import type { WidgetConfig, WidgetMessage } from "../types";

type Screen = "home" | "chat" | "articles" | "article" | "form";

/**
 * The widget interface.
 *
 * Written without the shared component library on purpose. Those components
 * assume the dashboard's base layer — global resets, focus rings, `@source`
 * scanning — none of which exist inside a shadow root, and importing them would
 * pull the whole design system into a bundle that must stay small. The token
 * layer is shared; the components are not.
 */
export function Widget({
  host,
  config,
  onEvent,
}: {
  host: string;
  config: WidgetConfig;
  onEvent: (name: string, payload?: unknown) => void;
}) {
  const [open, setOpen] = useState(false);
  const [screen, setScreen] = useState<Screen>("home");
  const [activeArticle, setActiveArticle] = useState<string | null>(null);
  const [unread, setUnread] = useState(0);
  const [messages, setMessages] = useState<WidgetMessage[]>([]);
  const [draft, setDraft] = useState("");
  const [query, setQuery] = useState("");
  const [agentTyping, setAgentTyping] = useState(false);

  const timelineRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  const { appearance, content } = config;
  const theme = useResolvedTheme(appearance.theme);

  const show = useCallback(() => {
    setOpen(true);
    setUnread(0);
    onEvent("open");
  }, [onEvent]);

  const hide = useCallback(() => {
    setOpen(false);
    onEvent("close");
  }, [onEvent]);

  /* ---------------------------------------------------------------- SDK API */

  useEffect(() => {
    const onCommand = (event: Event) => {
      const { method, payload } = (event as CustomEvent).detail as {
        method: string;
        payload?: Record<string, unknown>;
      };

      switch (method) {
        case "show":
          show();
          break;
        case "hide":
          hide();
          break;
        case "toggle":
          setOpen((current) => !current);
          break;
        case "startConversation":
          setScreen("chat");
          show();
          if (typeof payload?.message === "string") setDraft(payload.message);
          break;
        case "openArticle":
          setActiveArticle(String(payload?.slug ?? ""));
          setScreen("article");
          show();
          break;
        case "openForm":
          setScreen("form");
          show();
          break;
        case "reset":
          setMessages([]);
          setScreen("home");
          setOpen(false);
          break;
        default:
          // identify / update / track are network-only; the panel does not
          // re-render for them.
          break;
      }
    };

    window.addEventListener("hubchat:command", onCommand);
    return () => window.removeEventListener("hubchat:command", onCommand);
  }, [show, hide]);

  useEffect(() => {
    onEvent("ready");
  }, [onEvent]);

  useEffect(() => {
    onEvent("unread:changed", { count: unread });
  }, [unread, onEvent]);

  useEffect(() => {
    timelineRef.current?.scrollTo({ top: timelineRef.current.scrollHeight, behavior: "smooth" });
  }, [messages, agentTyping]);

  /* --------------------------------------------------------------- sending */

  const send = () => {
    const body = draft.trim();
    if (!body) return;

    const message: WidgetMessage = {
      // Client-generated so the send is idempotent if the socket retries (§9).
      id: `cli_${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`,
      from: "visitor",
      author: "You",
      body,
      at: new Date().toISOString(),
      delivery: "sending",
    };

    setMessages((current) => [...current, message]);
    setDraft("");
    onEvent("conversation:started", { id: message.id });

    window.setTimeout(() => {
      setMessages((current) =>
        current.map((item) => (item.id === message.id ? { ...item, delivery: "sent" } : item)),
      );
      setAgentTyping(true);
    }, 400);

    window.setTimeout(() => {
      setAgentTyping(false);
      setMessages((current) => [
        ...current,
        {
          id: `srv_${Date.now().toString(36)}`,
          from: "agent",
          author: "Ada",
          body: "Thanks — I can see your account. Give me a moment to check the delivery log.",
          at: new Date().toISOString(),
        },
      ]);
      if (!open) setUnread((count) => count + 1);
      onEvent("message:received", {});
    }, 2200);
  };

  const article = config.articles.find((item) => item.slug === activeArticle);
  const results = useMemo(
    () =>
      query
        ? config.articles.filter((item) =>
            `${item.title} ${item.excerpt}`.toLowerCase().includes(query.toLowerCase()),
          )
        : config.articles,
    [query, config.articles],
  );

  const right = appearance.position === "bottom-right";

  return (
    <div
      data-theme={theme}
      data-branded
      style={
        {
          "--hc-accent-brand": appearance.accent,
          position: "fixed",
          bottom: appearance.offset_y,
          [right ? "right" : "left"]: appearance.offset_x,
          display: "flex",
          flexDirection: "column",
          alignItems: right ? "flex-end" : "flex-start",
          gap: 12,
          pointerEvents: "none",
        } as React.CSSProperties
      }
    >
      {open && (
        <section
          role="dialog"
          aria-label={content.title}
          className="flex flex-col overflow-hidden border border-line bg-surface shadow-4"
          style={{
            width: `min(${appearance.panel_width}px, calc(100vw - 32px))`,
            height: `min(${appearance.panel_height}px, calc(100dvh - 120px))`,
            borderRadius: appearance.radius,
            pointerEvents: "auto",
            animation: "hc-widget-in 200ms cubic-bezier(0.16, 1, 0.3, 1) both",
          }}
        >
          <Header
            appearance={appearance}
            content={content}
            online={config.online}
            screen={screen}
            onBack={screen === "home" ? undefined : () => setScreen("home")}
            onClose={hide}
          />

          <div ref={timelineRef} className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
            {screen === "home" && (
              <HomeScreen
                config={config}
                onStartChat={() => {
                  setScreen("chat");
                  window.setTimeout(() => inputRef.current?.focus(), 60);
                }}
                onBrowse={() => setScreen("articles")}
                onForm={() => setScreen("form")}
                onArticle={(slug) => {
                  setActiveArticle(slug);
                  setScreen("article");
                }}
              />
            )}

            {screen === "chat" && (
              <ChatScreen
                config={config}
                messages={messages}
                typing={agentTyping}
              />
            )}

            {screen === "articles" && (
              <ArticlesScreen
                query={query}
                onQuery={setQuery}
                results={results}
                onOpen={(slug) => {
                  setActiveArticle(slug);
                  setScreen("article");
                }}
              />
            )}

            {screen === "article" && article && (
              <div className="p-4">
                <h2 className="text-md font-semibold tracking-tight text-fg">{article.title}</h2>
                <p className="mt-2 text-sm leading-relaxed text-fg-secondary">{article.excerpt}</p>
                <p className="mt-4 text-sm leading-relaxed text-fg-secondary">
                  Open the full guide in the help centre for the complete walkthrough, including
                  code samples.
                </p>
                <a
                  href={`${host}/portal/kb/article/${article.slug}`}
                  target="_blank"
                  rel="noreferrer"
                  className="mt-4 inline-flex items-center gap-1.5 text-sm text-accent"
                  style={{ pointerEvents: "auto" }}
                >
                  Read the full guide
                  <ChevronRight className="size-3.5" />
                </a>

                <div className="mt-6 rounded-lg border border-line bg-inset p-3">
                  <p className="text-xs text-fg">Did this answer your question?</p>
                  <div className="mt-2 flex gap-2">
                    <button
                      type="button"
                      className="rounded-md border border-line px-2.5 py-1 text-xs text-fg-secondary transition-colors hover:bg-fill"
                    >
                      Yes, thanks
                    </button>
                    <button
                      type="button"
                      onClick={() => setScreen("chat")}
                      className="rounded-md border border-line px-2.5 py-1 text-xs text-fg-secondary transition-colors hover:bg-fill"
                    >
                      No, I need help
                    </button>
                  </div>
                </div>
              </div>
            )}

            {screen === "form" && <FormScreen onDone={() => setScreen("home")} />}
          </div>

          {screen === "chat" && (
            <Composer
              ref={inputRef}
              value={draft}
              onChange={setDraft}
              onSend={send}
              placeholder={content.input_placeholder}
              disabled={!config.online}
              offlineMessage={content.offline_message}
              accent={appearance.accent}
            />
          )}

          {!appearance.hide_branding && (
            <p className="shrink-0 border-t border-line-subtle bg-surface py-1.5 text-center text-[10px] text-fg-disabled">
              Powered by Hubchat
            </p>
          )}
        </section>
      )}

      <Launcher
        appearance={appearance}
        open={open}
        unread={unread}
        onClick={() => (open ? hide() : show())}
      />
    </div>
  );
}

/* -------------------------------------------------------------------------- */

function Launcher({
  appearance,
  open,
  unread,
  onClick,
}: {
  appearance: WidgetConfig["appearance"];
  open: boolean;
  unread: number;
  onClick: () => void;
}) {
  const size = appearance.launcher_size === "sm" ? 44 : appearance.launcher_size === "lg" ? 60 : 52;

  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={open ? "Close support" : "Open support"}
      aria-expanded={open}
      className="relative flex items-center justify-center gap-2 text-white shadow-3 transition-transform hover:scale-105 active:scale-95"
      style={{
        backgroundColor: appearance.accent,
        width: appearance.launcher_label ? undefined : size,
        height: size,
        paddingInline: appearance.launcher_label ? 18 : undefined,
        borderRadius:
          appearance.launcher_shape === "circle"
            ? 999
            : appearance.launcher_shape === "pill"
              ? 999
              : 14,
        pointerEvents: "auto",
      }}
    >
      {open ? <X className="size-5" /> : <MessageSquare className="size-5" />}
      {appearance.launcher_label && !open && (
        <span className="text-sm font-medium">{appearance.launcher_label}</span>
      )}

      {unread > 0 && !open && (
        <span
          className="absolute -right-0.5 -top-0.5 grid min-w-5 place-items-center rounded-full px-1 text-[10px] font-semibold text-white"
          style={{ backgroundColor: "var(--hc-danger)" }}
        >
          {unread}
        </span>
      )}
    </button>
  );
}

function Header({
  appearance,
  content,
  online,
  screen,
  onBack,
  onClose,
}: {
  appearance: WidgetConfig["appearance"];
  content: WidgetConfig["content"];
  online: boolean;
  screen: Screen;
  onBack?: () => void;
  onClose: () => void;
}) {
  const minimal = appearance.header_style === "minimal";

  return (
    <header
      className={minimal ? "shrink-0 border-b border-line bg-surface px-4 py-3" : "shrink-0 px-4 py-3.5"}
      style={
        minimal
          ? undefined
          : appearance.header_style === "gradient"
            ? {
                background: `linear-gradient(135deg, ${appearance.accent}, color-mix(in oklab, ${appearance.accent} 62%, #000))`,
                color: "#fff",
              }
            : { backgroundColor: appearance.accent, color: "#fff" }
      }
    >
      <div className="flex items-start gap-2">
        {onBack && (
          <button
            type="button"
            onClick={onBack}
            aria-label="Back"
            className={minimal ? "mt-0.5 text-fg-muted" : "mt-0.5 opacity-80"}
          >
            <ArrowLeft className="size-4" />
          </button>
        )}

        <div className="min-w-0 flex-1">
          <p className={minimal ? "truncate text-sm font-semibold text-fg" : "truncate text-sm font-semibold"}>
            {screen === "articles" ? "Help articles" : content.title}
          </p>
          <p
            className={
              minimal
                ? "mt-0.5 flex items-center gap-1.5 truncate text-xs text-fg-muted"
                : "mt-0.5 flex items-center gap-1.5 truncate text-xs opacity-80"
            }
          >
            <span
              aria-hidden="true"
              className="size-1.5 rounded-full"
              style={{ backgroundColor: online ? "var(--hc-success)" : "var(--hc-text-disabled)" }}
            />
            {online ? content.subtitle : "Offline"}
          </p>
        </div>

        <button
          type="button"
          onClick={onClose}
          aria-label="Minimise"
          className={minimal ? "mt-0.5 text-fg-muted" : "mt-0.5 opacity-70 hover:opacity-100"}
        >
          <Minus className="size-4" />
        </button>
      </div>
    </header>
  );
}

function HomeScreen({
  config,
  onStartChat,
  onBrowse,
  onForm,
  onArticle,
}: {
  config: WidgetConfig;
  onStartChat: () => void;
  onBrowse: () => void;
  onForm: () => void;
  onArticle: (slug: string) => void;
}) {
  return (
    <div className="p-3">
      <div className="rounded-lg border border-line bg-inset p-3.5">
        <p className="text-sm leading-normal text-fg">{config.content.welcome_message}</p>
        {config.content.response_time_text && (
          <p className="mt-1 text-xs text-fg-muted">{config.content.response_time_text}</p>
        )}

        <button
          type="button"
          onClick={onStartChat}
          className="mt-3 flex w-full items-center justify-center gap-1.5 rounded-md px-3 py-2.5 text-sm font-medium text-white transition-transform active:scale-[0.99]"
          style={{ backgroundColor: config.appearance.accent }}
        >
          <MessageSquarePlus className="size-4" />
          {config.online ? "Start a conversation" : "Leave a message"}
        </button>
      </div>

      {config.modes.includes("knowledge_base") && config.articles.length > 0 && (
        <section className="mt-4">
          <button
            type="button"
            onClick={onBrowse}
            className="flex w-full items-center gap-2 rounded-md border border-line bg-inset px-3 py-2.5 text-left"
          >
            <Search className="size-3.5 shrink-0 text-fg-muted" />
            <span className="flex-1 text-xs text-fg-disabled">Search for help</span>
          </button>

          <ul className="mt-2">
            {config.articles.slice(0, 4).map((item) => (
              <li key={item.slug}>
                <button
                  type="button"
                  onClick={() => onArticle(item.slug)}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-2 text-left transition-colors hover:bg-fill"
                >
                  <Book className="size-3.5 shrink-0 text-fg-muted" />
                  <span className="min-w-0 flex-1 truncate text-xs text-fg-secondary">
                    {item.title}
                  </span>
                  <ChevronRight className="size-3 shrink-0 text-fg-disabled" />
                </button>
              </li>
            ))}
          </ul>
        </section>
      )}

      {config.modes.includes("ticket_form") && (
        <button
          type="button"
          onClick={onForm}
          className="mt-3 flex w-full items-center gap-2 rounded-md border border-line px-3 py-2.5 text-left transition-colors hover:bg-fill"
        >
          <Ticket className="size-3.5 shrink-0 text-fg-muted" />
          <span className="flex-1 text-xs text-fg-secondary">Submit a detailed request</span>
          <ChevronRight className="size-3 shrink-0 text-fg-disabled" />
        </button>
      )}
    </div>
  );
}

function ChatScreen({
  config,
  messages,
  typing,
}: {
  config: WidgetConfig;
  messages: WidgetMessage[];
  typing: boolean;
}) {
  const square = config.appearance.bubble_style === "square";

  return (
    <div className="flex flex-col gap-2.5 p-3">
      <div className="flex justify-start">
        <div
          className="max-w-[85%] border border-line bg-inset px-3 py-2"
          style={{ borderRadius: square ? 4 : 12 }}
        >
          <p className="text-sm leading-normal text-fg">{config.content.welcome_message}</p>
        </div>
      </div>

      {messages.map((message) => {
        const mine = message.from === "visitor";
        return (
          <div key={message.id} className={mine ? "flex justify-end" : "flex justify-start"}>
            <div className="max-w-[85%]">
              <div
                className={mine ? "px-3 py-2 text-white" : "border border-line bg-inset px-3 py-2"}
                style={{
                  borderRadius: square ? 4 : 12,
                  backgroundColor: mine ? config.appearance.accent : undefined,
                }}
              >
                <p className={mine ? "text-sm leading-normal" : "text-sm leading-normal text-fg"}>
                  {message.body}
                </p>
              </div>

              {mine && (
                <p className="mt-0.5 text-right text-[10px] text-fg-disabled">
                  {message.delivery === "sending"
                    ? "Sending…"
                    : message.delivery === "failed"
                      ? "Failed — tap to retry"
                      : "Sent"}
                </p>
              )}
            </div>
          </div>
        );
      })}

      {typing && (
        <div className="flex justify-start" aria-live="polite">
          <div
            className="flex items-center gap-1 border border-line bg-inset px-3 py-2.5"
            style={{ borderRadius: square ? 4 : 12 }}
          >
            {[0, 1, 2].map((index) => (
              <span
                key={index}
                className="size-1.5 rounded-full bg-fg-muted"
                style={{
                  animation: "hc-typing-bounce 1.2s ease-in-out infinite",
                  animationDelay: `${index * 0.16}s`,
                }}
              />
            ))}
            <span className="sr-only">Agent is typing</span>
          </div>
        </div>
      )}
    </div>
  );
}

function ArticlesScreen({
  query,
  onQuery,
  results,
  onOpen,
}: {
  query: string;
  onQuery: (value: string) => void;
  results: { slug: string; title: string; excerpt: string }[];
  onOpen: (slug: string) => void;
}) {
  return (
    <div className="p-3">
      <div className="flex items-center gap-2 rounded-md border border-line bg-inset px-2.5 py-2">
        <Search className="size-3.5 shrink-0 text-fg-muted" />
        <input
          value={query}
          onChange={(event) => onQuery(event.target.value)}
          placeholder="Search articles"
          aria-label="Search articles"
          className="min-w-0 flex-1 text-xs text-fg outline-none"
        />
      </div>

      {results.length === 0 ? (
        <p className="px-2 py-8 text-center text-xs text-fg-muted">
          Nothing matched. Try fewer words, or start a conversation.
        </p>
      ) : (
        <ul className="mt-2">
          {results.map((item) => (
            <li key={item.slug}>
              <button
                type="button"
                onClick={() => onOpen(item.slug)}
                className="w-full rounded-md px-2 py-2 text-left transition-colors hover:bg-fill"
              >
                <p className="text-xs font-medium text-fg">{item.title}</p>
                <p className="mt-0.5 line-clamp-2 text-[11px] leading-normal text-fg-muted">
                  {item.excerpt}
                </p>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function FormScreen({ onDone }: { onDone: () => void }) {
  const [sent, setSent] = useState(false);

  if (sent) {
    return (
      <div className="p-6 text-center">
        <p className="text-sm font-medium text-fg">Request received</p>
        <p className="mt-1.5 text-xs leading-normal text-fg-muted">
          We have emailed you a copy. Replies arrive in your inbox and here.
        </p>
        <button
          type="button"
          onClick={onDone}
          className="mt-4 rounded-md border border-line px-3 py-1.5 text-xs text-fg-secondary transition-colors hover:bg-fill"
        >
          Back
        </button>
      </div>
    );
  }

  return (
    <form
      className="flex flex-col gap-3 p-3"
      onSubmit={(event) => {
        event.preventDefault();
        setSent(true);
      }}
    >
      <label className="flex flex-col gap-1">
        <span className="text-xs font-medium text-fg-secondary">Your email</span>
        <input
          type="email"
          required
          className="rounded-md border border-line bg-inset px-2.5 py-2 text-sm text-fg outline-none focus:border-accent"
        />
      </label>

      <label className="flex flex-col gap-1">
        <span className="text-xs font-medium text-fg-secondary">Subject</span>
        <input
          required
          className="rounded-md border border-line bg-inset px-2.5 py-2 text-sm text-fg outline-none focus:border-accent"
        />
      </label>

      <label className="flex flex-col gap-1">
        <span className="text-xs font-medium text-fg-secondary">How can we help?</span>
        <textarea
          required
          rows={5}
          className="resize-none rounded-md border border-line bg-inset px-2.5 py-2 text-sm text-fg outline-none focus:border-accent"
        />
      </label>

      <button
        type="submit"
        className="rounded-md bg-accent px-3 py-2 text-sm font-medium text-accent-fg"
      >
        Send request
      </button>
    </form>
  );
}

/** React 19 passes `ref` as an ordinary prop, so no forwardRef wrapper. */
function Composer({
  ref,
  value,
  onChange,
  onSend,
  placeholder,
  disabled,
  offlineMessage,
  accent,
}: {
  ref?: React.Ref<HTMLTextAreaElement>;
  value: string;
  onChange: (value: string) => void;
  onSend: () => void;
  placeholder: string;
  disabled: boolean;
  offlineMessage: string;
  accent: string;
}) {
  return (
    <div className="shrink-0 border-t border-line bg-surface p-2">
      {disabled && (
        <p className="mb-2 rounded-md bg-fill px-2.5 py-1.5 text-[11px] leading-normal text-fg-muted">
          {offlineMessage}
        </p>
      )}

      <div className="flex items-end gap-2 rounded-md border border-line bg-inset px-2.5 py-1.5 focus-within:border-accent">
        <textarea
          ref={ref}
          rows={1}
          value={value}
          onChange={(event) => {
            onChange(event.target.value);
            const el = event.currentTarget;
            el.style.height = "auto";
            el.style.height = `${Math.min(el.scrollHeight, 96)}px`;
          }}
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.shiftKey) {
              event.preventDefault();
              onSend();
            }
          }}
          placeholder={placeholder}
          aria-label="Message"
          className="max-h-24 min-w-0 flex-1 resize-none bg-transparent py-1 text-sm leading-normal text-fg outline-none"
        />

        <button type="button" aria-label="Attach a file" className="pb-1.5 text-fg-muted">
          <Paperclip className="size-4" />
        </button>

        <button
          type="button"
          onClick={onSend}
          disabled={!value.trim()}
          aria-label="Send"
          className="pb-1.5 transition-opacity disabled:opacity-30"
          style={{ color: accent }}
        >
          <Send className="size-4" />
        </button>
      </div>
    </div>
  );
}

/** `auto` follows the host page's preference, not the workspace's. */
function useResolvedTheme(mode: "light" | "dark" | "auto"): "light" | "dark" {
  const [system, setSystem] = useState<"light" | "dark">("light");

  useEffect(() => {
    if (mode !== "auto") return;
    const query = window.matchMedia("(prefers-color-scheme: dark)");
    const apply = () => setSystem(query.matches ? "dark" : "light");
    apply();
    query.addEventListener("change", apply);
    return () => query.removeEventListener("change", apply);
  }, [mode]);

  return mode === "auto" ? system : mode;
}
