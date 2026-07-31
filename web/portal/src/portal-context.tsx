import { ApiError, api, useQuery } from "@hubchat/shared";
import { createContext, useContext, type ReactNode } from "react";

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
