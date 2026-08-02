import { ApiError, api, useQuery } from "@hubchat/shared";
import { createContext, useContext, type ReactNode } from "react";
import { Navigate } from "react-router-dom";

export type PortalConfig = {
  id: string;
  workspace_id: string;
  name: string;
  subdomain: string;
  theme: Record<string, unknown>;
  features: Record<string, unknown>;
  auth_methods: string[];
  permissions: Record<string, unknown>;
  default_language: string;
  navigation: Array<{ id: string; label: string; href: string; external: boolean }>;
  enabled: boolean;
};

export type PortalFeature = "tickets" | "knowledge_base" | "feedback" | "changelog" | "forms" | "announcements";

export type PortalViewer = {
  id: string;
  name: string;
  email: string;
  company?: string;
  language?: string;
  timezone?: string;
};

export type PortalNotificationPreferences = {
  ticket_status: boolean;
  feedback_updates: boolean;
  changelog: boolean;
  surveys: boolean;
};

export type PortalBootstrap = {
  portal: PortalConfig;
  viewer: PortalViewer | null;
  preferences?: PortalNotificationPreferences;
  session?: { portal_id: string; expires_at: string; auth_method: string };
};

type PortalContextValue = {
  data: PortalBootstrap | undefined;
  isLoading: boolean;
  error: unknown;
  refetch: () => void;
};

const Context = createContext<PortalContextValue | null>(null);

function portalQuery() {
  const value = new URLSearchParams(window.location.search).get("portal");
  return value ? `?portal=${encodeURIComponent(value)}` : "";
}

export function PortalProvider({ children }: { children: ReactNode }) {
  const query = useQuery<PortalBootstrap>(
    ["portal", "bootstrap", portalQuery()],
    (signal) => api.get(`/portal/bootstrap${portalQuery()}`, { signal }),
    { staleTime: 30_000 },
  );

  return <Context.Provider value={query}>{children}</Context.Provider>;
}

export function usePortal() {
  const value = useContext(Context);
  if (!value) throw new Error("usePortal must be used inside PortalProvider");
  return value;
}

export function portalAccent(config: PortalConfig | undefined) {
  const value = config?.theme?.accent;
  return typeof value === "string" && value ? value : "#3B6EF6";
}

/**
 * Feature flags are a server-owned visibility contract, not just a UI hint.
 * Core sections default on for older portals created before the flag was
 * introduced; announcements are opt-in because they have no content by
 * default.
 */
export function portalFeatureEnabled(config: PortalConfig | undefined, feature: PortalFeature) {
  if (!config) return false;
  const value = config.features?.[feature];
  return feature === "announcements" ? value === true : value !== false;
}

export function portalThemeText(config: PortalConfig | undefined, key: "headline" | "subheadline", fallback: string) {
  const value = config?.theme?.[key];
  return typeof value === "string" && value.trim() ? value : fallback;
}

/** Language used for knowledge-base variants on public portal requests. */
export function portalLanguage(data: PortalBootstrap | undefined) {
  const configured = data?.viewer?.language || data?.portal.default_language || "en";
  return configured.trim().toLowerCase().replaceAll("_", "-");
}

const RTL_LANGUAGE_PREFIXES = new Set(["ar", "dv", "fa", "ha", "he", "ks", "ku", "ps", "ur", "yi"]);

/** Languages whose normal reading direction is right-to-left. */
export function portalIsRTL(language: string) {
  const base = language.trim().toLowerCase().split("-", 1)[0] ?? "";
  return RTL_LANGUAGE_PREFIXES.has(base);
}

export function portalDirection(language: string): "ltr" | "rtl" {
  return portalIsRTL(language) ? "rtl" : "ltr";
}

export function portalAssetURL(value: unknown) {
  if (typeof value !== "string" || !value.trim()) return null;
  try {
    const parsed = new URL(value, window.location.origin);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return null;
    return parsed.href;
  } catch {
    return null;
  }
}

export function portalNavigationFeature(href: string): PortalFeature | null {
  const pathname = href.split(/[?#]/, 1)[0] ?? "";
  if (pathname === "/tickets" || pathname.startsWith("/tickets/")) return "tickets";
  if (pathname === "/kb" || pathname.startsWith("/kb/")) return "knowledge_base";
  if (pathname === "/feedback" || pathname.startsWith("/feedback/")) return "feedback";
  if (pathname === "/changelog" || pathname.startsWith("/changelog/")) return "changelog";
  if (pathname === "/forms" || pathname.startsWith("/forms/")) return "forms";
  return null;
}

export function portalNavigationItems(config: PortalConfig | undefined) {
  if (!config) return [];
  return config.navigation.filter((item) => {
    if (item.external || /^https?:\/\//i.test(item.href)) return portalAssetURL(item.href) !== null;
    if (!item.href.startsWith("/") || item.href.startsWith("//") || item.href.includes("\\")) return false;
    const feature = portalNavigationFeature(item.href);
    return !feature || portalFeatureEnabled(config, feature);
  });
}

export function PortalFeatureGate({ feature, children }: { feature: PortalFeature; children: ReactNode }) {
  const { data } = usePortal();
  if (!data || portalFeatureEnabled(data.portal, feature)) return <>{children}</>;
  return <Navigate to="/" replace />;
}

export function portalErrorMessage(error: unknown) {
  if (error instanceof ApiError) return `${error.message}${error.requestId ? ` (Request ${error.requestId})` : ""}`;
  return "The portal could not be loaded. Check your connection and try again.";
}

/** Keep authentication handoffs inside the mounted portal application. */
export function safePortalNext(value: string | null | undefined, portalID?: string | null) {
  if (!value || !value.startsWith("/") || value.startsWith("//") || value.includes("\\")) return "/";
  try {
    const parsed = new URL(value, window.location.origin);
    if (parsed.origin !== window.location.origin) return "/";
    const pathname = parsed.pathname.replace(/^\/portal(?=\/|$)/, "") || "/";
    if (portalID) parsed.searchParams.set("portal", portalID);
    return `${pathname}${parsed.search}${parsed.hash}`;
  } catch {
    return "/";
  }
}
