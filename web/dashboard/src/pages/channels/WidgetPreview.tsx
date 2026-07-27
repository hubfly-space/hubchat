import { Avatar, cn, type Widget } from "@hubchat/shared";
import { Book, MessageSquare, Paperclip, Search, Send, Smile, Ticket, X } from "lucide-react";

/**
 * Live widget preview used by the builder.
 *
 * It renders the real component vocabulary against the draft configuration, so
 * what the operator sees is what the visitor gets. Theming happens through
 * `[data-branded]` and `--hc-accent-brand`, which is the same mechanism the
 * shipped widget uses — no preview-only styling exists.
 */
export function WidgetPreview({
  widget,
  device,
  theme,
  state,
}: {
  widget: Widget;
  device: "desktop" | "mobile";
  theme: "light" | "dark";
  state: "launcher" | "home" | "chat";
}) {
  const { appearance, content } = widget;

  return (
    <div
      data-theme={theme}
      data-branded
      style={{ ["--hc-accent-brand" as string]: appearance.accent }}
      className={cn(
        "relative flex h-full w-full items-end overflow-hidden rounded-xl border border-line bg-canvas",
        appearance.position === "bottom-left" ? "justify-start" : "justify-end",
      )}
    >
      {/* Host page stand-in. Deliberately abstract — the preview is about the
          widget, and a realistic fake site would pull attention. */}
      <div aria-hidden="true" className="absolute inset-0 p-6 opacity-40">
        <div className="mb-4 h-6 w-32 rounded bg-fill" />
        <div className="mb-2 h-3 w-full max-w-md rounded bg-fill" />
        <div className="mb-2 h-3 w-4/5 max-w-md rounded bg-fill" />
        <div className="h-3 w-2/3 max-w-md rounded bg-fill" />
      </div>

      <div
        className="relative flex flex-col items-end gap-3 p-5"
        style={{
          paddingInline: appearance.offset_x,
          paddingBlockEnd: appearance.offset_y,
        }}
      >
        {state !== "launcher" && (
          <div
            className="flex flex-col overflow-hidden border border-line bg-surface shadow-4"
            style={{
              width: device === "mobile" ? 300 : Math.min(appearance.panel_width, 360),
              height: device === "mobile" ? 460 : Math.min(appearance.panel_height, 480),
              borderRadius: appearance.radius,
            }}
          >
            {/* Header ------------------------------------------------------ */}
            <header
              className={cn(
                "shrink-0 px-4 py-3.5",
                appearance.header_style === "minimal"
                  ? "border-b border-line bg-surface"
                  : "text-white",
              )}
              style={
                appearance.header_style === "minimal"
                  ? undefined
                  : appearance.header_style === "gradient"
                    ? {
                        background: `linear-gradient(135deg, ${appearance.accent}, color-mix(in oklab, ${appearance.accent} 62%, #000))`,
                      }
                    : { backgroundColor: appearance.accent }
              }
            >
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <p
                    className={cn(
                      "truncate text-sm font-semibold",
                      appearance.header_style === "minimal" && "text-fg",
                    )}
                  >
                    {content.title}
                  </p>
                  <p
                    className={cn(
                      "mt-0.5 truncate text-xs",
                      appearance.header_style === "minimal" ? "text-fg-muted" : "opacity-80",
                    )}
                  >
                    {content.subtitle}
                  </p>
                </div>
                <X
                  aria-hidden="true"
                  className={cn(
                    "size-4 shrink-0",
                    appearance.header_style === "minimal" ? "text-fg-muted" : "opacity-70",
                  )}
                />
              </div>
            </header>

            {/* Body -------------------------------------------------------- */}
            {state === "home" ? (
              <div className="min-h-0 flex-1 overflow-y-auto p-3">
                <div className="rounded-lg border border-line bg-inset p-3">
                  <p className="text-sm text-fg">{content.welcome_message}</p>
                  <p className="mt-1 text-2xs text-fg-muted">{content.response_time_text}</p>
                  <button
                    type="button"
                    className="mt-2.5 flex w-full items-center justify-center gap-1.5 rounded-md px-3 py-2 text-xs font-medium text-white"
                    style={{ backgroundColor: appearance.accent }}
                  >
                    <MessageSquare className="size-3.5" />
                    Start a conversation
                  </button>
                </div>

                {widget.modes.includes("knowledge_base") && (
                  <div className="mt-3">
                    <div className="flex items-center gap-2 rounded-md border border-line bg-inset px-2.5 py-2">
                      <Search aria-hidden="true" className="size-3.5 text-fg-muted" />
                      <span className="text-xs text-fg-disabled">Search for help</span>
                    </div>
                    <ul className="mt-2 space-y-1">
                      {["Installing the widget", "Verifying webhook signatures", "Signed identity tokens"].map(
                        (title) => (
                          <li key={title}>
                            <span className="flex items-center gap-2 rounded-md px-2 py-1.5 text-xs text-fg-secondary">
                              <Book aria-hidden="true" className="size-3 text-fg-muted" />
                              {title}
                            </span>
                          </li>
                        ),
                      )}
                    </ul>
                  </div>
                )}

                {widget.modes.includes("ticket_form") && (
                  <button
                    type="button"
                    className="mt-3 flex w-full items-center gap-2 rounded-md border border-line px-2.5 py-2 text-xs text-fg-secondary"
                  >
                    <Ticket aria-hidden="true" className="size-3.5 text-fg-muted" />
                    Submit a request
                  </button>
                )}
              </div>
            ) : (
              <div className="min-h-0 flex-1 space-y-2.5 overflow-y-auto p-3">
                <div className="flex items-end gap-1.5">
                  <Avatar name="Ada Mwangi" size="xs" />
                  <div
                    className="max-w-[80%] border border-line bg-inset px-2.5 py-1.5"
                    style={{
                      borderRadius: appearance.bubble_style === "square" ? 4 : 12,
                    }}
                  >
                    <p className="text-xs leading-normal text-fg">{content.welcome_message}</p>
                  </div>
                </div>

                <div className="flex justify-end">
                  <div
                    className="max-w-[80%] px-2.5 py-1.5 text-white"
                    style={{
                      backgroundColor: appearance.accent,
                      borderRadius: appearance.bubble_style === "square" ? 4 : 12,
                    }}
                  >
                    <p className="text-xs leading-normal">
                      Hi — our webhooks stopped delivering after the upgrade.
                    </p>
                  </div>
                </div>

                <div className="flex items-end gap-1.5">
                  <Avatar name="Ada Mwangi" size="xs" />
                  <div
                    className="border border-line bg-inset px-3 py-2"
                    style={{ borderRadius: appearance.bubble_style === "square" ? 4 : 12 }}
                  >
                    <span className="flex items-center gap-1">
                      {[0, 1, 2].map((index) => (
                        <span
                          key={index}
                          className="size-1 rounded-full bg-fg-muted"
                          style={{
                            animation: "hc-typing-bounce 1.2s ease-in-out infinite",
                            animationDelay: `${index * 0.16}s`,
                          }}
                        />
                      ))}
                    </span>
                  </div>
                </div>
              </div>
            )}

            {/* Composer ---------------------------------------------------- */}
            <div className="shrink-0 border-t border-line p-2">
              <div className="flex items-center gap-2 rounded-md border border-line bg-inset px-2.5 py-2">
                <span className="flex-1 truncate text-xs text-fg-disabled">
                  {content.input_placeholder}
                </span>
                <Paperclip aria-hidden="true" className="size-3.5 text-fg-muted" />
                <Smile aria-hidden="true" className="size-3.5 text-fg-muted" />
                <Send aria-hidden="true" className="size-3.5" style={{ color: appearance.accent }} />
              </div>
              {!appearance.hide_branding && (
                <p className="mt-1.5 text-center text-[9px] text-fg-disabled">Powered by Hubchat</p>
              )}
            </div>
          </div>
        )}

        {/* Launcher ------------------------------------------------------- */}
        <button
          type="button"
          className={cn(
            "flex items-center justify-center gap-2 text-white shadow-3",
            appearance.launcher_shape === "circle" && "rounded-full",
            appearance.launcher_shape === "rounded" && "rounded-xl",
            appearance.launcher_shape === "pill" && "rounded-full px-4",
          )}
          style={{
            backgroundColor: appearance.accent,
            width: appearance.launcher_label
              ? undefined
              : appearance.launcher_size === "sm"
                ? 44
                : appearance.launcher_size === "lg"
                  ? 60
                  : 52,
            height: appearance.launcher_size === "sm" ? 44 : appearance.launcher_size === "lg" ? 60 : 52,
          }}
        >
          <MessageSquare aria-hidden="true" className="size-5" />
          {appearance.launcher_label && (
            <span className="text-sm font-medium">{appearance.launcher_label}</span>
          )}
        </button>
      </div>
    </div>
  );
}
