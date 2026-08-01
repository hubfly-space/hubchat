import {
  api,
  ApiError,
  invalidate,
  QueryError,
  useLocalStorage,
  useQuery,
  useRealtimeConnection,
  useRealtimeEvents,
  type Capability,
  type Company,
  type Customer,
  type Inbox,
  type Member,
  type Tag,
  type Team,
  type Workspace,
} from "@hubchat/shared";
import { createContext, useCallback, useContext, useMemo, type ReactNode } from "react";
import { Navigate } from "react-router-dom";

/**
 * Workspace scope for the whole dashboard.
 *
 * Three jobs:
 *   1. hold the active workspace and viewer, so nothing has to thread them
 *      through props;
 *   2. expose `can()` — the client-side capability check;
 *   3. keep the realtime socket open and turn incoming events into cache
 *      invalidation, which is what makes every screen update without polling.
 *
 * `can()` is a *UI affordance* only. It decides whether to render a button, not
 * whether an action is permitted. §11.3 puts real authorization in the Go
 * service layer, and every mutation is re-checked there. Hiding a control the
 * server would reject is courtesy; relying on that hiding would be a defect.
 */

type WorkspaceEntry = { id: string; name: string; slug: string; role: string };

type Bootstrap = {
  workspace: Workspace;
  workspaces: WorkspaceEntry[];
  viewer: Member;
  members: Member[];
  teams: Team[];
  tags: Tag[];
  inboxes: Inbox[];
};

type WorkspaceContextValue = {
  workspace: Workspace;
  workspaces: WorkspaceEntry[];
  viewer: Member;
  members: Member[];
  teams: Team[];
  inboxes: Inbox[];
  can: (capability: Capability) => boolean;
  switchWorkspace: (id: string) => void;
  /** Whether the realtime socket is currently connected. */
  isLive: boolean;

  memberById: (id: string | null) => Member | undefined;
  customerById: (id: string | null) => Customer | undefined;
  companyById: (id: string | null) => Company | undefined;
  tagById: (id: string) => Tag | undefined;
  tags: Tag[];
};

const WorkspaceContext = createContext<WorkspaceContextValue | null>(null);

/** The key the bootstrap payload is cached under. Invalidated on membership changes. */
export const BOOTSTRAP_KEY = ["bootstrap"] as const;

export function WorkspaceProvider({ children }: { children: ReactNode }) {
  // Persisted so a reload returns to the workspace the agent was working in
  // rather than their first membership. Empty means "let the server choose",
  // which is right immediately after signup.
  const [activeId, setActiveId] = useLocalStorage<string>("hubchat.workspace", "");

  const bootstrap = useQuery<Bootstrap>(
    ["bootstrap", activeId],
    (signal) => api.get<Bootstrap>("/bootstrap", { workspaceId: activeId || undefined, signal }),
    // The bootstrap payload changes rarely, and the pieces that do change
    // (presence, tags) arrive over the socket. A long stale time here avoids
    // refetching the whole workspace every time the tab regains focus.
    { staleTime: 5 * 60_000 },
  );

  const connection = useRealtimeConnection(bootstrap.data?.workspace.id);
  useWorkspaceInvalidation();

  const can = useCallback(
    (capability: Capability) => bootstrap.data?.viewer.capabilities.includes(capability) ?? false,
    [bootstrap.data],
  );

  const value = useMemo<WorkspaceContextValue | null>(() => {
    const data = bootstrap.data;
    if (!data) return null;

    // Indexed once per payload rather than a linear scan per lookup. The
    // conversation list calls memberById once per visible row, and the inbox
    // is the screen most agents leave open all day.
    const memberIndex = new Map(data.members.map((member) => [member.id, member]));
    const tagIndex = new Map(data.tags.map((tag) => [tag.id, tag]));

    return {
      workspace: data.workspace,
      workspaces: data.workspaces,
      viewer: data.viewer,
      members: data.members,
      teams: data.teams,
      inboxes: data.inboxes,
      tags: data.tags,
      can,
      switchWorkspace: setActiveId,
      isLive: connection === "open",
      memberById: (id) => (id ? memberIndex.get(id) : undefined),
      // Customers and companies are deliberately not in the bootstrap payload:
      // a workspace has unboundedly many, so the screens that need one fetch
      // it. These stay on the interface so callers do not have to branch.
      customerById: () => undefined,
      companyById: () => undefined,
      tagById: (id) => tagIndex.get(id),
    };
  }, [bootstrap.data, can, setActiveId, connection]);

  if (bootstrap.isLoading) return <BootstrapSkeleton />;

  if (bootstrap.error !== undefined || !value) {
    // Not signed in, or the session expired while the tab was open. Being
    // signed out is not an error the user can act on from here, so send them
    // somewhere they can — carrying the current path so they land back where
    // they were rather than on the overview.
    if (bootstrap.error instanceof ApiError && bootstrap.error.isUnauthorized) {
      const next = encodeURIComponent(window.location.pathname + window.location.search);
      return <Navigate to={`/login?next=${next}`} replace />;
    }

    // A member with no workspace has not finished setting up. The onboarding
    // route is the only thing that can move them forward.
    if (bootstrap.error instanceof ApiError && bootstrap.error.isForbidden) {
      return <Navigate to="/onboarding" replace />;
    }

    return (
      <div className="mx-auto max-w-md p-8">
        <QueryError error={bootstrap.error} retry={bootstrap.refetch} />
      </div>
    );
  }

  return <WorkspaceContext.Provider value={value}>{children}</WorkspaceContext.Provider>;
}

/**
 * Turns realtime events into cache invalidation.
 *
 * This is the whole reason the dashboard feels live. A screen never subscribes
 * to events itself; it reads a query, and when the server says the underlying
 * data changed, that query refetches. Adding a screen costs nothing here.
 *
 * Mapping is by entity type rather than by event type, because the question a
 * cache asks is "what changed", not "what happened to it" — a conversation
 * being assigned, resolved, or replied to all invalidate the same keys.
 */
function useWorkspaceInvalidation() {
  useRealtimeEvents((event) => {
    // The server could not replay everything we missed. Nothing on screen can
    // be trusted, so drop it all rather than leaving stale rows that look
    // current.
    if (event.type === "hub.desynchronised") {
      invalidate(["conversations"]);
      invalidate(["tickets"]);
      invalidate(["customers"]);
      invalidate(["companies"]);
      invalidate(["bootstrap"]);
      invalidateNotifications();
      return;
    }

    if (notificationEventTypes.has(event.type)) {
      invalidateNotifications();
    }

    switch (event.entity_type) {
      case "conversation":
        invalidate(["conversations"]);
        if (event.entity_id) invalidate(["conversation", event.entity_id]);
        break;
      case "ticket":
        invalidate(["tickets"]);
        if (event.entity_id) invalidate(["ticket", event.entity_id]);
        break;
      case "customer":
        invalidate(["customers"]);
        if (event.entity_id) invalidate(["customer", event.entity_id]);
        // event.received carries application events (§6.10), not a profile
        // change — the timeline and developer event stream key off it
        // separately from the general customer-record invalidation above.
        if (event.type === "event.received") {
          invalidate(["events"]);
          if (event.entity_id) invalidate(["customer-timeline", event.entity_id]);
        }
        break;
      case "company":
        invalidate(["companies"]);
        if (event.entity_id) invalidate(["company", event.entity_id]);
        break;
      case "member":
      case "team":
        invalidate(["bootstrap"]);
        break;
      case "saved_view":
        invalidate(["saved-views", "conversation"]);
        break;
      case "article":
        invalidate(["articles"]);
        if (event.entity_id) invalidate(["article", event.entity_id]);
        invalidate(["knowledge-bases"]);
        break;
    }
  });
}

// These source events can create durable agent notifications in the Go
// notification consumer. Keeping this mapping beside the other realtime
// invalidation rules means the bell converges from the same event stream as
// the inbox, rather than waiting for focus or a stale-time refresh.
const notificationEventTypes = new Set([
  "conversation.assigned",
  "message.created",
  "ticket.updated",
  "sla.approaching",
  "sla.breached",
]);

function invalidateNotifications() {
  invalidate(["notifications"]);
  invalidate(["notifications-count"]);
}

function BootstrapSkeleton() {
  return (
    <div className="flex h-dvh items-center justify-center">
      <div className="flex flex-col items-center gap-3">
        <div className="size-8 animate-spin rounded-full border-2 border-[var(--border-strong)] border-t-transparent" />
        <p className="text-sm text-[var(--text-muted)]">Loading your workspace…</p>
      </div>
    </div>
  );
}

export function useWorkspace(): WorkspaceContextValue {
  const context = useContext(WorkspaceContext);
  if (!context) throw new Error("useWorkspace must be used within a <WorkspaceProvider>");
  return context;
}
