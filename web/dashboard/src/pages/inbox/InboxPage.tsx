import {
  api,
  EmptyState,
  cn,
  invalidate,
  useHotkey,
  useInfinite,
  useQuery,
  type Conversation,
  type Customer,
  type Inbox,
  type Paginated,
} from "@hubchat/shared";
import { MessagesSquare } from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
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
  const navigate = useNavigate();
  const { viewer } = useWorkspace();
  const [showContext, setShowContext] = useState(true);

  const inboxes = useQuery<{ data: Inbox[] }>(["inboxes"], (signal) => api.get("/inboxes", { signal }));
  const filterParams = useMemo(
    () => viewFilterParams(viewId, viewer.id, inboxes.data?.data ?? []),
    [viewId, viewer.id, inboxes.data],
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

  const customerIds = useMemo(
    () => [...new Set(list.items.map((item) => item.customer_id).filter((id): id is string => !!id))],
    [list.items],
  );
  const customers = useQuery<{ data: Customer[] }>(
    customerIds.length > 0 ? ["customers", "by-ids", customerIds.join(",")] : null,
    (signal) => api.get(`/customers?ids=${customerIds.join(",")}`, { signal }),
  );
  const customersById = useMemo(
    () => new Map((customers.data?.data ?? []).map((c) => [c.id, c])),
    [customers.data],
  );

  const activeIndex = conversation ? list.items.findIndex((item) => item.id === conversation.id) : -1;
  const open = (id: string) => navigate(`/inbox/${viewId}/${id}`);

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
  const bulkAssignToMe = async (ids: string[]) => {
    setBulkPending(true);
    try {
      await Promise.all(ids.map((id) => api.patch(`/conversations/${id}/assignee`, { assignee_id: viewer.id })));
    } finally {
      invalidate(["conversations"]);
      setBulkPending(false);
    }
  };
  const bulkResolve = async (ids: string[]) => {
    setBulkPending(true);
    try {
      await Promise.all(ids.map((id) => api.patch(`/conversations/${id}/state`, { state: "resolved" })));
    } finally {
      invalidate(["conversations"]);
      setBulkPending(false);
    }
  };

  return (
    <div className="flex h-full min-h-0">
      <ConversationList
        conversations={list.items}
        customersById={customersById}
        activeId={conversation?.id ?? null}
        onSelect={open}
        viewName={viewLabel(viewId, inboxes.data?.data ?? [])}
        onBulkAssignToMe={(ids) => void bulkAssignToMe(ids)}
        onBulkResolve={(ids) => void bulkResolve(ids)}
        bulkPending={bulkPending}
        hasMore={list.hasMore}
        onLoadMore={() => void list.fetchNext()}
        loadingMore={list.isFetching}
      />

      {conversation ? (
        <ConversationPanel
          key={conversation.id}
          conversation={conversation}
          onToggleContext={() => setShowContext((current) => !current)}
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
function viewFilterParams(viewId: string, viewerId: string, inboxes: Inbox[]): string {
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
      if (inbox) params.set("inbox_id", inbox.id);
      break;
    }
  }

  return params.toString();
}

function viewLabel(viewId: string, inboxes: Inbox[]): string {
  const labels: Record<string, string> = {
    all: "All active",
    unassigned: "Unassigned",
    mine: "Assigned to me",
    following: "Following",
    "waiting-support": "Waiting on us",
    "waiting-customer": "Waiting on customer",
    snoozed: "Snoozed",
    resolved: "Resolved",
    spam: "Spam",
  };

  return labels[viewId] ?? inboxes.find((inbox) => inbox.slug === viewId)?.name ?? "Inbox";
}
