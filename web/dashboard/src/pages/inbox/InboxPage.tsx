import {
	ApiError,
	api,
  EmptyState,
  cn,
	invalidate,
	idempotencyKey,
	useDashboardPreferences,
	useHotkey,
  useAllPages,
  useInfinite,
  useQuery,
  type Conversation,
  type Customer,
  type Inbox,
  type Paginated,
  type SavedView,
} from "@hubchat/shared";
import { MessagesSquare } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useLocation, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";
import { ConversationList } from "./ConversationList";
import { ConversationPanel } from "./ConversationPanel";
import { CustomerContextPanel } from "./CustomerContextPanel";

/**
 * The inbox (§6.2) — three panes plus the shell's view sidebar.
 *
 *   views (shell)  ·  conversation list  ·  conversation  ·  customer context
 *
 * The view id becomes a server-side filter (conversation.ListFilter), so the
 * client never holds conversations the viewer's inbox access would exclude
 * (§11.3) and a workspace with a hundred thousand conversations costs the
 * same as one with ten.
 */
export default function InboxPage() {
  const { viewId = "all", conversationId } = useParams();
  const location = useLocation();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const { viewer } = useWorkspace();
  const { preferences, setPreference } = useDashboardPreferences();
  const [showContext, setShowContext] = useState(preferences.showCustomerContext);

  useEffect(() => {
    setShowContext(preferences.showCustomerContext);
  }, [preferences.showCustomerContext]);

  const inboxes = useAllPages<Inbox>(["inboxes", "lookup"], (cursor, signal) => api.get<Paginated<Inbox>>(`/inboxes?limit=200${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }));
  const savedViews = useInfinite<SavedView>(["saved-views", "conversation"], (cursor, signal) => {
    const params = new URLSearchParams({ entity_type: "conversation", limit: "50" });
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<SavedView>>(`/saved-views?${params.toString()}`, { signal });
  });
  const filterParams = useMemo(
    () => viewFilterParams(viewId, viewer.id, inboxes.items, savedViews.items),
    [viewId, viewer.id, inboxes.items, savedViews.items],
  );

  const list = useInfinite<Conversation>(
    inboxes.isLoading ? null : ["conversations", filterParams],
    (cursor, signal) => {
      const params = new URLSearchParams(filterParams);
      if (cursor) params.set("cursor", cursor);
      return api.get<Paginated<Conversation>>(`/conversations?${params.toString()}`, { signal });
    },
  );

  const active = list.items.find((item) => item.id === conversationId);
  const activeDetail = useQuery<Conversation>(
    conversationId && !active ? ["conversation", conversationId] : null,
    (signal) => api.get(`/conversations/${conversationId}`, { signal }),
  );
  const conversation = active ?? activeDetail.data;

  const sortParam = searchParams.get("sort");
  const sort = sortParam === "oldest" || sortParam === "priority" ? sortParam : "recent";
  const setSort = (next: "recent" | "oldest" | "priority") => {
    const nextParams = new URLSearchParams(searchParams);
    if (next === "recent") nextParams.delete("sort");
    else nextParams.set("sort", next);
    setSearchParams(nextParams, { replace: true });
  };

  const customerIds = useMemo(
    () => [...new Set(list.items.map((item) => item.customer_id).filter((id): id is string => !!id))],
    [list.items],
  );
  const customers = useQuery<Paginated<Customer>>(
    customerIds.length > 0 ? ["customers", "by-ids", customerIds.join(",")] : null,
    (signal) => api.get(`/customers?ids=${customerIds.join(",")}`, { signal }),
  );
  const customersById = useMemo(
    () => new Map((customers.data?.data ?? []).map((c) => [c.id, c])),
    [customers.data],
  );

  const activeIndex = conversation ? list.items.findIndex((item) => item.id === conversation.id) : -1;
  const open = (id: string) => navigate({ pathname: `/inbox/${viewId}/${id}`, search: location.search });
  const afterResolved = (id: string) => {
    if (preferences.afterResolve === "stay") return;
    if (preferences.afterResolve === "next") {
      const currentIndex = list.items.findIndex((item) => item.id === id);
      const next = currentIndex >= 0 ? list.items[currentIndex + 1] : undefined;
      if (next) {
        open(next.id);
        return;
      }
    }
    navigate({ pathname: `/inbox/${viewId}`, search: location.search });
  };
  const toggleContext = () => {
    setShowContext((current) => {
      const next = !current;
      setPreference("showCustomerContext", next);
      return next;
    });
  };

  // j/k move through the list without leaving the keyboard (§6.2).
  useHotkey("j", () => {
    const next = list.items[Math.min(activeIndex + 1, list.items.length - 1)];
    if (next) open(next.id);
  });
  useHotkey("k", () => {
    const previous = list.items[Math.max(activeIndex - 1, 0)];
    if (previous) open(previous.id);
  });

	const [bulkPending, setBulkPending] = useState(false);
	const [bulkError, setBulkError] = useState<string | null>(null);
	const runBulk = async (ids: string[], action: "assign" | "state", body: Record<string, unknown>): Promise<boolean> => {
		setBulkPending(true);
		setBulkError(null);
		try {
			await api.post("/conversations/bulk", { ids, action, ...body }, { idempotencyKey: idempotencyKey() });
			return true;
		} catch (error) {
			setBulkError(error instanceof ApiError ? error.message : "The bulk update could not be completed. No conversations were changed.");
			return false;
		} finally {
			invalidate(["conversations"]);
			setBulkPending(false);
		}
	};
	const bulkAssignToMe = (ids: string[]) => runBulk(ids, "assign", { assignee_id: viewer.id });
	const bulkResolve = (ids: string[]) => runBulk(ids, "state", { state: "resolved" });

  return (
    <div className="flex h-full min-h-0">
      <ConversationList
        conversations={list.items}
        customersById={customersById}
        activeId={conversation?.id ?? null}
        onSelect={open}
        viewName={viewLabel(viewId, inboxes.items, savedViews.items)}
		onBulkAssignToMe={(ids) => bulkAssignToMe(ids)}
		onBulkResolve={(ids) => bulkResolve(ids)}
		bulkPending={bulkPending}
		bulkError={bulkError}
        sort={sort}
        onSortChange={setSort}
        hasMore={list.hasMore}
        onLoadMore={() => void list.fetchNext()}
        loadingMore={list.isFetching}
      />

      {conversation ? (
        <ConversationPanel
          key={conversation.id}
          conversation={conversation}
          onToggleContext={toggleContext}
          onResolved={() => afterResolved(conversation.id)}
          markReadOnOpen={preferences.markReadOnOpen}
        />
      ) : (
        <div className="flex flex-1 items-center justify-center bg-canvas">
          <EmptyState
            icon={MessagesSquare}
            title="Select a conversation"
            description="Choose a thread from the list, or press j and k to move through it without leaving the keyboard."
          />
        </div>
      )}

      {conversation && (
        <aside
          className={cn(
            "w-context shrink-0 overflow-y-auto border-l border-line bg-surface",
            showContext ? "hidden xl:block" : "hidden",
          )}
          aria-label="Customer context"
        >
          <CustomerContextPanel customerId={conversation.customer_id} />
        </aside>
      )}
    </div>
  );
}

/** Builds the conversations query string a sidebar view id maps to. */
function viewFilterParams(viewId: string, viewerId: string, inboxes: Inbox[], savedViews: SavedView[]): string {
  const params = new URLSearchParams();

  switch (viewId) {
    case "all":
      // The backend's own default (no `state` param) excludes only closed
      // and spam, matching the conversations_active_queue index verbatim.
      // "All active" as a product concept is narrower than that — resolved
      // means done, not active — so this view asks for the narrower set
      // explicitly rather than relying on the broader default.
      params.set("state", "new,open,pending,waiting_for_customer,waiting_for_support,snoozed");
      break;
    case "unassigned":
      params.set("assignee_id", "unassigned");
      break;
    case "mine":
      params.set("assignee_id", viewerId);
      break;
    case "following":
      params.set("follower_id", viewerId);
      break;
    case "mentioned":
      params.set("mentioned", "true");
      params.set("state", "new,open,pending,waiting_for_customer,waiting_for_support,snoozed");
      break;
    case "sla-approaching":
      params.set("sla", "approaching");
      params.set("state", "new,open,pending,waiting_for_customer,waiting_for_support,snoozed");
      break;
    case "sla-breached":
      params.set("sla", "breached");
      params.set("state", "new,open,pending,waiting_for_customer,waiting_for_support,snoozed");
      break;
    case "waiting-support":
      params.set("state", "waiting_for_support");
      break;
    case "waiting-customer":
      params.set("state", "waiting_for_customer,pending");
      break;
    case "snoozed":
      params.set("state", "snoozed");
      break;
    case "resolved":
      params.set("state", "resolved");
      break;
    case "spam":
      params.set("state", "spam");
      break;
    default: {
      const inbox = inboxes.find((item) => item.slug === viewId);
      if (inbox) {
        params.set("inbox_id", inbox.id);
        break;
      }
      const savedView = savedViews.find((item) => item.id === viewId);
      if (savedView) applySavedView(params, savedView);
      break;
    }
  }

  return params.toString();
}

function viewLabel(viewId: string, inboxes: Inbox[], savedViews: SavedView[]): string {
  const labels: Record<string, string> = {
    all: "All active",
    unassigned: "Unassigned",
    mine: "Assigned to me",
    following: "Following",
    mentioned: "Mentioned",
    "waiting-support": "Waiting on us",
    "waiting-customer": "Waiting on customer",
    snoozed: "Snoozed",
    resolved: "Resolved",
    "sla-approaching": "Approaching SLA",
    "sla-breached": "Breached SLA",
    spam: "Spam",
  };

  return labels[viewId] ?? inboxes.find((inbox) => inbox.slug === viewId)?.name ?? savedViews.find((view) => view.id === viewId)?.name ?? "Inbox";
}

function applySavedView(params: URLSearchParams, view: SavedView) {
  const conditions = Array.isArray(view.filters?.conditions) ? view.filters.conditions : [];
  const states: string[] = [];
  for (const condition of conditions) {
    if (condition.operator !== "is" && condition.operator !== "in") continue;
    const values = Array.isArray(condition.value) ? condition.value.map(String) : [String(condition.value)];
    switch (condition.field) {
      case "state":
        states.push(...values);
        break;
      case "assignee_id":
        if (values[0]) params.set("assignee_id", values[0]);
        break;
      case "team_id":
        if (values[0]) params.set("team_id", values[0]);
        break;
      case "inbox_id":
        if (values[0]) params.set("inbox_id", values[0]);
        break;
      case "priority":
        if (values[0]) params.set("priority", values[0]);
        break;
      case "tag_id":
        if (values[0]) params.set("tag_id", values[0]);
        break;
      case "follower_id":
        if (values[0]) params.set("follower_id", values[0]);
        break;
    }
  }
  if (states.length > 0) params.set("state", states.join(","));
}
