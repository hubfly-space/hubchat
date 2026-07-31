import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
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
import {
  clearSession,
  createFeedbackItem,
  getArticle,
  identify as apiIdentify,
  issueVisitor,
  listFeedbackBoards,
  listFeedbackItems,
  listForms,
  listMessages,
  loadSession,
  postMessage,
  saveSession,
  searchArticles,
  startConversation,
  submitArticleFeedback,
  subscribeFeedbackItem,
  unsubscribeFeedbackItem,
  submitForm,
  track as apiTrack,
  uploadFile,
  voteFeedbackItem,
  type WidgetFeedbackBoard,
  type WidgetFeedbackItem,
  type WidgetArticle,
  type WidgetForm,
  type WireMessage,
} from "../lib/api";
import { VisitorSocket, type WireEvent } from "../lib/socket";
import type { WidgetConfig, WidgetMessage } from "../types";

type Screen = "home" | "chat" | "articles" | "article" | "form" | "feedback";

function fromWire(m: WireMessage): WidgetMessage {
  return {
    id: m.id,
    from: m.author_type === "agent" ? "agent" : m.author_type === "system" ? "system" : "visitor",
    author: m.author_name,
    body: m.body,
    at: m.created_at,
    delivery: "sent",
    attachments: m.attachments,
  };
}

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
  publicKey,
  config,
  onEvent,
}: {
  host: string;
  publicKey: string;
  config: WidgetConfig;
  onEvent: (name: string, payload?: unknown) => void;
}) {
  const [open, setOpen] = useState(false);
  const [screen, setScreen] = useState<Screen>("home");
  const [activeArticle, setActiveArticle] = useState<string | null>(null);
  const [unread, setUnread] = useState(0);
  const [messages, setMessages] = useState<WidgetMessage[]>([]);
  const [draft, setDraft] = useState("");
  const [attachments, setAttachments] = useState<File[]>([]);
  const [uploading, setUploading] = useState(false);
  const [attachmentError, setAttachmentError] = useState("");
  const [query, setQuery] = useState("");
  const [articleResults, setArticleResults] = useState<WidgetArticle[]>(config.articles);
  const [articleDetail, setArticleDetail] = useState<WidgetArticle | null>(null);
  const [articleLoading, setArticleLoading] = useState(false);
  const [articleFeedback, setArticleFeedback] = useState<"submitted" | "error" | null>(null);
  const [agentTyping, setAgentTyping] = useState(false);

  const timelineRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  // The visitor's own token and, once one exists, the conversation it owns —
  // both persisted so a page reload (or a return visit, when
  // behavior.persist_conversation is set) resumes the same thread instead of
  // starting a new one. Refs, not state: reading the current value inside
  // `send` must never race a stale render.
  const tokenRef = useRef<string | null>(null);
  const visitorTokenPromiseRef = useRef<Promise<string> | null>(null);
  const conversationRef = useRef<string | null>(null);
  const socketRef = useRef<VisitorSocket | null>(null);
  const viewedArticlesRef = useRef(new Set<string>());
  const impressionTrackedRef = useRef(false);

  const { appearance, content } = config;
  const theme = useResolvedTheme(appearance.theme);

  const ensureVisitorToken = useCallback(async () => {
    let token = tokenRef.current;
    if (token) return token;
    if (visitorTokenPromiseRef.current) return visitorTokenPromiseRef.current;

    const pending = issueVisitor(host, publicKey)
      .then((issued) => {
        tokenRef.current = issued.token;
        return issued.token;
      })
      .finally(() => {
        visitorTokenPromiseRef.current = null;
      });
    visitorTokenPromiseRef.current = pending;
    token = await pending;
    if (!token) {
      throw new Error("visitor token was empty");
    }
    return token;
  }, [host, publicKey]);

  const trackSurface = useCallback((type: string) => {
    void ensureVisitorToken()
      .then((token) => apiTrack(host, publicKey, token, type, {}))
      .catch(() => {});
  }, [ensureVisitorToken, host, publicKey]);

  const show = useCallback(() => {
    setOpen(true);
    setUnread(0);
    trackSurface("widget.opened");
    onEvent("open");
  }, [onEvent, trackSurface]);

  const hide = useCallback(() => {
    setOpen(false);
    onEvent("close");
  }, [onEvent]);

  /* ------------------------------------------------------------- realtime */

  // openSocket is called once a conversation exists — never before, because
  // the server computes a visitor's realtime grant from their conversations
  // at connect time (internal/realtime's Grant "only ever narrows", never
  // widens after the fact). A visitor with no conversation yet would open a
  // socket with an empty grant and never receive anything for one started
  // moments later.
  const openSocket = useCallback(
    (token: string) => {
      socketRef.current?.close();
      socketRef.current = new VisitorSocket({
        host,
        publicKey,
        token,
        onStatusChange: () => {},
        onEvent: (event: WireEvent) => {
          if (event.type === "presence.typing") {
            const typing = event.data as { actor_type: string; typing: boolean };
            if (typing.actor_type === "agent") setAgentTyping(typing.typing);
            return;
          }
          if (event.type !== "message.created") return;
          const data = event.data as { author_type: string; id: string; author_name: string; body: string; created_at: string };
          // The visitor's own message is already rendered optimistically the
          // moment they hit send — this channel delivering it back would
          // duplicate it. Internal notes never reach here at all (filtered
          // server-side), so nothing else needs excluding.
          if (data.author_type === "customer") return;

          setMessages((current) => {
            if (current.some((m) => m.id === data.id)) return current;
            return [...current, fromWire({ ...data, kind: "reply", sequence: event.sequence } as WireMessage)];
          });
          setAgentTyping(false);
          if (!openRef.current) setUnread((count) => count + 1);
          onEvent("message:received", { id: data.id, body: data.body });
        },
      });
    },
    [host, publicKey, onEvent],
  );

  // Mirrors `open` into a ref so the socket callback above (created once per
  // token, not per render) always reads the current value rather than the
  // one captured when openSocket last ran.
  const openRef = useRef(open);
  useEffect(() => {
    openRef.current = open;
  }, [open]);

  // Resume a persisted session on mount: same visitor, same conversation,
  // history reloaded and the socket reconnected — this is what makes a page
  // reload (or a return visit, when persist_conversation is set) pick up
  // exactly where the visitor left off rather than starting over.
  useEffect(() => {
    if (!config.behavior.persist_conversation) return;
    const session = loadSession(publicKey);
    if (!session?.token) return;
    tokenRef.current = session.token;

    if (session.conversationId) {
      conversationRef.current = session.conversationId;
      listMessages(host, publicKey, session.token, session.conversationId)
        .then((wire) => setMessages(wire.filter((m) => m.kind !== "note").map(fromWire)))
        .catch(() => {});
      openSocket(session.token);
    }

    return () => socketRef.current?.close();
    // Intentionally runs once per widget mount — a new publicKey means a
    // different widget instance entirely, which React already remounts.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  /* ---------------------------------------------------------------- SDK API */

  // A command dispatched before this listener is attached is lost —
  // `dispatchEvent` has no queue of its own, unlike v1.js's own `state.q`.
  // That case is not hypothetical: `boot` immediately followed by `show` (or
  // `startConversation`) in the same script block, or a "trigger: immediate"
  // widget, both fire a command the instant the interface finishes loading.
  // React does not guarantee an effect — layout or passive — has run by the
  // time `root.render()` returns, so main.tsx cannot simply call this
  // synchronously and assume it is safe to replay commands next.
  //
  // The fix is ordering, not timing: main.tsx attaches its own listener for
  // "hubchat:internal:mounted" *before* calling render() at all, and does
  // not resolve mount()'s promise — which is what unblocks v1.js's pending
  // command replay — until that fires. Dispatching it here, as the last
  // thing this effect does after its own listener is already attached,
  // guarantees no command can arrive before this component is ready for it,
  // regardless of which tick React chooses to run the effect in.
  useLayoutEffect(() => {
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
          setActiveArticle(typeof payload?.slug === "string" ? payload.slug : null);
          setScreen("form");
          show();
          break;
        case "openFeedback":
          setActiveArticle(typeof payload?.slug === "string" ? payload.slug : null);
          setScreen("feedback");
          show();
          break;
        case "identify": {
          const ensureTokenThenIdentify = async () => {
            const token = await ensureVisitorToken();
            await apiIdentify(host, publicKey, token, {
              name: typeof payload?.name === "string" ? payload.name : undefined,
              email: typeof payload?.email === "string" ? payload.email : undefined,
              external_id: typeof payload?.external_id === "string" ? payload.external_id : undefined,
              signed_token: typeof payload?.token === "string" ? payload.token : undefined,
            });
          };
          void ensureTokenThenIdentify().catch(() => {});
          break;
        }
        case "context":
        case "update": {
          if (!tokenRef.current) break;
          const context = (payload && typeof payload === "object" ? payload : {}) as Record<string, unknown>;
          void apiTrack(host, publicKey, tokenRef.current, "context.updated", context).catch(() => {});
          break;
        }
        case "track": {
          const type = typeof payload?.type === "string" ? payload.type : "";
          if (!type || !tokenRef.current) break;
          void apiTrack(host, publicKey, tokenRef.current, type, (payload?.payload as Record<string, unknown>) ?? {}).catch(
            () => {},
          );
          break;
        }
        case "reset":
          socketRef.current?.close();
          socketRef.current = null;
          tokenRef.current = null;
          conversationRef.current = null;
          clearSession(publicKey);
          setMessages([]);
          setScreen("home");
          setOpen(false);
          break;
        default:
          break;
      }
    };

    window.addEventListener("hubchat:command", onCommand);
    window.dispatchEvent(new CustomEvent("hubchat:internal:mounted"));
    return () => window.removeEventListener("hubchat:command", onCommand);
  }, [ensureVisitorToken, show, hide, host, publicKey]);

  useEffect(() => {
    onEvent("ready");
    if (!impressionTrackedRef.current) {
      impressionTrackedRef.current = true;
      trackSurface("widget.impression");
    }
  }, [onEvent, trackSurface]);

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

    const clientId = `cli_${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
    const message: WidgetMessage = {
      id: clientId,
      from: "visitor",
      author: "You",
      body,
      at: new Date().toISOString(),
      delivery: "sending",
    };

    setMessages((current) => [...current, message]);
    setDraft("");

    const markDelivery = (delivery: WidgetMessage["delivery"]) =>
      setMessages((current) => current.map((item) => (item.id === clientId ? { ...item, delivery } : item)));

    void (async () => {
      try {
        let token = await ensureVisitorToken();

        let sentMessageID = "";
        if (!conversationRef.current) {
          const result = await startConversation(host, publicKey, token, body);
          sentMessageID = result.message.id;
          conversationRef.current = result.conversation_id;
          // The server mints a fresh token only when the request arrived
          // without one; otherwise it echoes back an empty string and the
          // token this browser already holds keeps being the right one.
          if (result.token) token = result.token;
          tokenRef.current = token;
          saveSession(publicKey, { token, conversationId: result.conversation_id });
          openSocket(token);
          onEvent("conversation:started", { id: result.conversation_id });
        } else {
          const result = await postMessage(host, publicKey, token, conversationRef.current, body);
          sentMessageID = result.id;
          saveSession(publicKey, { token, conversationId: conversationRef.current });
        }

        markDelivery("sent");
        if (attachments.length > 0 && conversationRef.current && sentMessageID) {
          setUploading(true);
          try {
            const uploaded = [] as { id: string; name: string; url?: string }[];
            for (const file of attachments) uploaded.push(await uploadFile(host, publicKey, token, conversationRef.current, sentMessageID, file));
            setMessages((current) => current.map((item) => item.id === clientId ? {
              ...item,
              attachments: uploaded.filter((file): file is { id: string; name: string; url: string } => Boolean(file.url)).map((file) => ({ id: file.id, name: file.name, url: file.url })),
            } : item));
            setAttachments([]);
          } catch {
            setAttachmentError("Your message was sent, but an attachment could not be uploaded.");
          } finally {
            setUploading(false);
          }
        }
      } catch {
        markDelivery("failed");
      }
    })();
  };

  useEffect(() => {
    if (screen !== "articles" || !config.modes.includes("knowledge_base")) return;
    let cancelled = false;
    const timer = window.setTimeout(() => {
      void searchArticles(host, publicKey, query).then((items) => {
        if (!cancelled) setArticleResults(items);
      }).catch(() => {
        if (!cancelled && !query.trim()) setArticleResults(config.articles);
      });
    }, query.trim() ? 180 : 0);
    return () => { cancelled = true; window.clearTimeout(timer); };
  }, [config.articles, config.modes, host, publicKey, query, screen]);

  useEffect(() => {
    if (screen !== "article" || !activeArticle) return;
    setArticleLoading(true);
    setArticleDetail(null);
    setArticleFeedback(null);
    void getArticle(host, publicKey, activeArticle)
      .then((item) => {
        setArticleDetail(item);
        if (!viewedArticlesRef.current.has(activeArticle)) {
          viewedArticlesRef.current.add(activeArticle);
          trackSurface("widget.article_viewed");
        }
      })
      .catch(() => setArticleDetail(config.articles.find((item) => item.slug === activeArticle) ?? null))
      .finally(() => setArticleLoading(false));
  }, [activeArticle, config.articles, host, publicKey, screen, trackSurface]);

  const article: WidgetArticle | undefined = articleDetail ?? config.articles.find((item) => item.slug === activeArticle);

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
                onForm={() => { setActiveArticle(null); setScreen("form"); }}
                onFeedback={() => { setActiveArticle(null); setScreen("feedback"); }}
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
                host={host}
                publicKey={publicKey}
                token={tokenRef.current}
              />
            )}

            {screen === "articles" && (
              <ArticlesScreen
                query={query}
                onQuery={setQuery}
                results={articleResults}
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
                {articleLoading ? <p className="mt-4 text-xs text-fg-muted">Loading guide…</p> : article.body && <div className="mt-4 whitespace-pre-wrap text-sm leading-relaxed text-fg-secondary">{article.body}</div>}
                <a href={`${host}/portal/kb/article/${article.slug}`} target="_blank" rel="noreferrer" className="mt-4 inline-flex items-center gap-1.5 text-sm text-accent" style={{ pointerEvents: "auto" }}>
                  Open in help centre <ChevronRight className="size-3.5" />
                </a>

                <div className="mt-6 rounded-lg border border-line bg-inset p-3">
                  <p className="text-xs text-fg">Did this answer your question?</p>
                  <div className="mt-2 flex gap-2">
                    <button
                      type="button"
                      onClick={() => { void submitArticleFeedback(host, publicKey, article.slug, true, "").then(() => setArticleFeedback("submitted")).catch(() => setArticleFeedback("error")); }}
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
                  {articleFeedback === "submitted" && <p className="mt-2 text-xs text-fg-muted">Thanks — your feedback was recorded.</p>}
                  {articleFeedback === "error" && <p className="mt-2 text-xs text-danger">We could not record that feedback. Please try again.</p>}
                </div>
              </div>
            )}
            {screen === "article" && !article && (
              <div className="p-6 text-center text-xs text-fg-muted">{articleLoading ? "Loading guide…" : "This guide is no longer available."}</div>
            )}

            {screen === "form" && <FormScreen host={host} publicKey={publicKey} token={tokenRef.current} initialSlug={activeArticle} onToken={(token) => { tokenRef.current = token; saveSession(publicKey, { token, conversationId: conversationRef.current }); }} onDone={() => setScreen("home")} />}
            {screen === "feedback" && <FeedbackScreen host={host} publicKey={publicKey} token={tokenRef.current} initialSlug={activeArticle} onToken={(token) => { tokenRef.current = token; saveSession(publicKey, { token, conversationId: conversationRef.current }); }} onDone={() => setScreen("home")} />}
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
              files={attachments}
              uploading={uploading}
              error={attachmentError}
              onChooseFiles={(files) => { setAttachments(files); setAttachmentError(""); }}
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
  onFeedback,
  onArticle,
}: {
  config: WidgetConfig;
  onStartChat: () => void;
  onBrowse: () => void;
  onForm: () => void;
  onFeedback: () => void;
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

      {config.modes.includes("feedback") && (
        <button type="button" onClick={onFeedback} className="mt-3 flex w-full items-center gap-2 rounded-md border border-line px-3 py-2.5 text-left transition-colors hover:bg-fill">
          <MessageSquarePlus className="size-3.5 shrink-0 text-fg-muted" />
          <span className="flex-1 text-xs text-fg-secondary">Share feedback</span>
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
  host,
  publicKey,
  token,
}: {
  config: WidgetConfig;
  messages: WidgetMessage[];
  typing: boolean;
  host: string;
  publicKey: string;
  token: string | null;
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
                {message.attachments && message.attachments.length > 0 && (
                  <div className="mt-2 space-y-1 border-t border-white/20 pt-2">
                    {message.attachments.map((attachment) => (
                      <a
                        key={attachment.id}
                        href={`${host}${attachment.url}?${new URLSearchParams({ key: publicKey, url: window.location.href, token: token ?? "" }).toString()}`}
                        target="_blank"
                        rel="noreferrer"
                        className={mine ? "block truncate text-xs underline" : "block truncate text-xs text-accent underline"}
                      >
                        <Paperclip className="mr-1 inline size-3" aria-hidden="true" />{attachment.name}
                      </a>
                    ))}
                  </div>
                )}
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

function FeedbackScreen({ host, publicKey, token, initialSlug, onToken, onDone }: {
  host: string;
  publicKey: string;
  token: string | null;
  initialSlug: string | null;
  onToken: (token: string) => void;
  onDone: () => void;
}) {
  const [boards, setBoards] = useState<WidgetFeedbackBoard[]>([]);
  const [board, setBoard] = useState<WidgetFeedbackBoard | null>(null);
  const [items, setItems] = useState<WidgetFeedbackItem[]>([]);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [error, setError] = useState("");
  const [sending, setSending] = useState(false);
  useEffect(() => {
    let cancelled = false;
    void listFeedbackBoards(host, publicKey).then((next) => {
      if (cancelled) return;
      setBoards(next);
      setBoard(next.find((item) => item.slug === initialSlug) ?? next[0] ?? null);
    }).catch((reason: unknown) => { if (!cancelled) setError(reason instanceof Error ? reason.message : "Feedback is unavailable."); });
    return () => { cancelled = true; };
  }, [host, publicKey, initialSlug]);
  useEffect(() => {
    if (!board) return;
    void listFeedbackItems(host, publicKey, token, board.slug).then(setItems).catch(() => setError("Could not load feedback items."));
  }, [host, publicKey, token, board]);
  const submit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!board || !title.trim() || !description.trim()) return;
    setSending(true); setError("");
    try {
      const result = await createFeedbackItem(host, publicKey, token, board.slug, title.trim(), description.trim());
      if (result.token) onToken(result.token);
      setItems((current) => [result.item, ...current]); setTitle(""); setDescription("");
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Could not submit feedback."); }
    finally { setSending(false); }
  };
  const vote = async (item: WidgetFeedbackItem) => {
    if (!token) { setError("Identify yourself before voting."); return; }
    try { await voteFeedbackItem(host, publicKey, token, item.id); setItems((current) => current.map((entry) => entry.id === item.id ? { ...entry, vote_count: entry.vote_count + 1, viewer_has_voted: true } : entry)); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "Could not record vote."); }
  };
  const follow = async (item: WidgetFeedbackItem) => {
    if (!token) { setError("Identify yourself before following feedback."); return; }
    try {
      if (item.viewer_subscribed) await unsubscribeFeedbackItem(host, publicKey, token, item.id);
      else await subscribeFeedbackItem(host, publicKey, token, item.id);
      setItems((current) => current.map((entry) => entry.id === item.id ? { ...entry, viewer_subscribed: !item.viewer_subscribed } : entry));
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Could not update feedback subscription."); }
  };
  if (!board) return <div className="p-6 text-center"><p className="text-sm font-medium text-fg">No public feedback boards</p>{error && <p className="mt-2 text-xs text-danger">{error}</p>}<button type="button" onClick={onDone} className="mt-4 rounded-md border border-line px-3 py-1.5 text-xs text-fg-secondary">Back</button></div>;
  return <div className="p-3"><div className="mb-3 flex items-center gap-2">{boards.length > 1 ? <select value={board.slug} onChange={(event) => setBoard(boards.find((item) => item.slug === event.target.value) ?? board)} className="min-w-0 flex-1 rounded-md border border-line bg-inset px-2.5 py-2 text-sm text-fg">{boards.map((item) => <option key={item.id} value={item.slug}>{item.name}</option>)}</select> : <p className="text-sm font-medium text-fg">{board.name}</p>}</div>{board.description && <p className="mb-3 text-xs text-fg-muted">{board.description}</p>}<form className="space-y-2 border-b border-line pb-3" onSubmit={(event) => void submit(event)}><input required value={title} onChange={(event) => setTitle(event.target.value)} placeholder="What should we improve?" className="w-full rounded-md border border-line bg-inset px-2.5 py-2 text-sm text-fg outline-none focus:border-accent" /><textarea required rows={3} value={description} onChange={(event) => setDescription(event.target.value)} placeholder="Tell us why this matters…" className="w-full resize-none rounded-md border border-line bg-inset px-2.5 py-2 text-sm text-fg outline-none focus:border-accent" /><button type="submit" disabled={sending} className="w-full rounded-md bg-accent px-3 py-2 text-sm font-medium text-accent-fg disabled:opacity-50">{sending ? "Sending…" : "Submit feedback"}</button></form>{error && <p className="mt-2 text-xs text-danger">{error}</p>}<div className="mt-3 space-y-2">{items.length === 0 ? <p className="py-4 text-center text-xs text-fg-muted">No feedback here yet.</p> : items.map((item) => <div key={item.id} className="rounded-md border border-line bg-inset p-2.5"><p className="text-xs font-medium text-fg">{item.title}</p><p className="mt-1 line-clamp-2 text-[11px] text-fg-muted">{item.description}</p><div className="mt-2 flex items-center justify-between gap-2"><span className="text-[11px] text-fg-disabled">{item.status.replaceAll("_", " ")}</span><div className="flex items-center gap-1.5"><button type="button" disabled={item.viewer_has_voted} onClick={() => void vote(item)} className="rounded border border-line px-2 py-1 text-[11px] text-fg-secondary disabled:opacity-50">{item.viewer_has_voted ? "Voted" : `Vote · ${item.vote_count}`}</button><button type="button" onClick={() => void follow(item)} className="rounded border border-line px-2 py-1 text-[11px] text-fg-secondary">{item.viewer_subscribed ? "Following" : "Follow"}</button></div></div></div>)}</div></div>;
}

function FormScreen({ host, publicKey, token, initialSlug, onToken, onDone }: {
  host: string;
  publicKey: string;
  token: string | null;
  initialSlug: string | null;
  onToken: (token: string) => void;
  onDone: () => void;
}) {
  const [forms, setForms] = useState<WidgetForm[]>([]);
  const [form, setForm] = useState<WidgetForm | null>(null);
  const [values, setValues] = useState<Record<string, unknown>>({});
  const [loading, setLoading] = useState(true);
  const [sending, setSending] = useState(false);
  const [sent, setSent] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    void listForms(host, publicKey).then((items) => {
      if (cancelled) return;
      setForms(items);
      setForm(items.find((item) => item.slug === initialSlug) ?? items[0] ?? null);
    }).catch((reason: unknown) => {
      if (!cancelled) setError(reason instanceof Error ? reason.message : "Forms are unavailable.");
    }).finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [host, publicKey, initialSlug]);

  const visible = (field: WidgetForm["fields"][number]) => {
    const condition = field.condition ?? {};
    const key = typeof condition.field === "string" ? condition.field : typeof condition.key === "string" ? condition.key : "";
    if (!key) return true;
    if ("equals" in condition) return values[key] === condition.equals;
    if ("not_equals" in condition) return values[key] !== condition.not_equals;
    return true;
  };
  const setValue = (key: string, value: unknown) => setValues((current) => ({ ...current, [key]: value }));
  const submit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!form) return;
    setSending(true); setError("");
    try {
      const result = await submitForm(host, publicKey, token, form.slug, values);
      if (result.token) onToken(result.token);
      setSent(true);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "The form could not be sent.");
    } finally { setSending(false); }
  };

  if (loading) return <p className="p-6 text-center text-xs text-fg-muted">Loading request forms…</p>;
  if (sent) return <div className="p-6 text-center"><p className="text-sm font-medium text-fg">Request received</p><p className="mt-1.5 text-xs leading-normal text-fg-muted">Your submission was sent to the support team.</p><button type="button" onClick={onDone} className="mt-4 rounded-md border border-line px-3 py-1.5 text-xs text-fg-secondary transition-colors hover:bg-fill">Back</button></div>;
  if (!form) return <div className="p-6 text-center"><p className="text-sm font-medium text-fg">No request forms are available</p><p className="mt-1.5 text-xs text-fg-muted">Start a conversation instead and a person will help you there.</p>{error && <p className="mt-2 text-xs text-danger">{error}</p>}<button type="button" onClick={onDone} className="mt-4 rounded-md border border-line px-3 py-1.5 text-xs text-fg-secondary">Back</button></div>;

  return <div className="p-3"><div className="mb-3 flex items-center gap-2">{forms.length > 1 && <select value={form.slug} onChange={(event) => { setForm(forms.find((item) => item.slug === event.target.value) ?? form); setValues({}); }} className="min-w-0 flex-1 rounded-md border border-line bg-inset px-2.5 py-2 text-sm text-fg"><option value={form.slug}>{form.name}</option>{forms.filter((item) => item.slug !== form.slug).map((item) => <option key={item.slug} value={item.slug}>{item.name}</option>)}</select>}{forms.length === 1 && <p className="text-sm font-medium text-fg">{form.name}</p>}</div>{form.description && <p className="mb-3 text-xs leading-normal text-fg-muted">{form.description}</p>}<form className="flex flex-col gap-3" onSubmit={(event) => void submit(event)}>{form.fields.filter(visible).map((field) => <label key={field.key} className="flex flex-col gap-1"><span className="text-xs font-medium text-fg-secondary">{field.label}{field.required && <span className="text-danger"> *</span>}</span>{field.description && <span className="text-[11px] text-fg-muted">{field.description}</span>}{field.type === "text" ? <textarea required={field.required} rows={4} value={String(values[field.key] ?? "")} placeholder={field.placeholder ?? ""} onChange={(event) => setValue(field.key, event.target.value)} className="resize-none rounded-md border border-line bg-inset px-2.5 py-2 text-sm text-fg outline-none focus:border-accent" /> : field.type === "boolean" ? <input type="checkbox" checked={Boolean(values[field.key])} onChange={(event) => setValue(field.key, event.target.checked)} className="size-4 accent-accent" /> : field.type === "enum" ? <select required={field.required} value={String(values[field.key] ?? "")} onChange={(event) => setValue(field.key, event.target.value)} className="rounded-md border border-line bg-inset px-2.5 py-2 text-sm text-fg outline-none focus:border-accent"><option value="">Choose…</option>{(field.options ?? []).map((option) => <option key={option} value={option}>{option}</option>)}</select> : <input required={field.required} type={field.type === "email" ? "email" : field.type === "integer" || field.type === "decimal" ? "number" : "text"} value={String(values[field.key] ?? "")} placeholder={field.placeholder ?? ""} onChange={(event) => setValue(field.key, field.type === "integer" || field.type === "decimal" ? Number(event.target.value) : event.target.value)} className="rounded-md border border-line bg-inset px-2.5 py-2 text-sm text-fg outline-none focus:border-accent" />}</label>)}{error && <p className="text-xs text-danger">{error}</p>}<button type="submit" disabled={sending} className="rounded-md bg-accent px-3 py-2 text-sm font-medium text-accent-fg disabled:opacity-50">{sending ? "Sending…" : "Send request"}</button></form></div>;
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
  files,
  uploading,
  error,
  onChooseFiles,
}: {
  ref?: React.Ref<HTMLTextAreaElement>;
  value: string;
  onChange: (value: string) => void;
  onSend: () => void;
  placeholder: string;
  disabled: boolean;
  offlineMessage: string;
  accent: string;
  files: File[];
  uploading: boolean;
  error: string;
  onChooseFiles: (files: File[]) => void;
}) {
  return (
    <div className="shrink-0 border-t border-line bg-surface p-2">
      {disabled && (
        <p className="mb-2 rounded-md bg-fill px-2.5 py-1.5 text-[11px] leading-normal text-fg-muted">
          {offlineMessage}
        </p>
      )}
      {files.length > 0 && <p className="mb-2 truncate rounded-md bg-fill px-2.5 py-1.5 text-[11px] text-fg-muted">{uploading ? "Uploading…" : files.map((file) => file.name).join(", ")}</p>}
      {error && <p className="mb-2 rounded-md bg-danger-subtle px-2.5 py-1.5 text-[11px] text-danger-text">{error}</p>}

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

        <label aria-label="Attach a file" className="cursor-pointer pb-1.5 text-fg-muted">
          <Paperclip className="size-4" />
          <input type="file" multiple className="sr-only" onChange={(event) => { onChooseFiles(Array.from(event.target.files ?? [])); event.target.value = ""; }} />
        </label>

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
