/**
 * The public widget configuration.
 *
 * This is deliberately *not* the dashboard's `Widget` type. What the browser
 * receives is a narrowed projection: no inbox id, no workspace id, no domain
 * list, no rollout percentage, no internal counters. A visitor holding a public
 * key should learn nothing about the workspace beyond what they can already see
 * on screen (§11.5 — no privileged data in widget code).
 */
export type WidgetConfig = {
  enabled: boolean;
  online: boolean;
  modes: WidgetMode[];
  appearance: {
    accent: string;
    theme: "light" | "dark" | "auto";
    launcher_shape: "circle" | "rounded" | "pill";
    launcher_size: "sm" | "md" | "lg";
    launcher_label: string | null;
    position: "bottom-right" | "bottom-left";
    offset_x: number;
    offset_y: number;
    panel_width: number;
    panel_height: number;
    radius: number;
    header_style: "solid" | "gradient" | "minimal";
    bubble_style: "rounded" | "square";
    z_index: number;
    hide_branding: boolean;
  };
  content: {
    title: string;
    subtitle: string;
    welcome_message: string;
    input_placeholder: string;
    response_time_text: string;
    offline_message: string;
  };
  behavior: {
    trigger: "immediate" | "delay" | "scroll" | "event" | "manual";
    delay_seconds: number;
    scroll_percent: number;
    allow_anonymous: boolean;
  };
  /** A small pre-fetched set for instant search; full search hits the API. */
  articles: { slug: string; title: string; excerpt: string }[];
};

export type WidgetMode =
  | "chat"
  | "support_menu"
  | "ticket_form"
  | "knowledge_base"
  | "feedback"
  | "announcements"
  | "hub";

export type WidgetMessage = {
  id: string;
  from: "visitor" | "agent" | "system";
  author: string;
  body: string;
  at: string;
  delivery?: "sending" | "sent" | "failed";
};
