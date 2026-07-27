import { EmptyState, cn, useHotkey } from "@hubchat/shared";
import { MessagesSquare } from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";
import { conversations, inboxes, savedViews } from "../../data/fixtures";
import { ConversationList } from "./ConversationList";
import { ConversationPanel } from "./ConversationPanel";
import { CustomerContextPanel } from "./CustomerContextPanel";

/**
 * The inbox (§6.2) — three panes plus the shell's view sidebar.
 *
 *   views (shell)  ·  conversation list  ·  conversation  ·  customer context
 *
 * Filtering runs client-side against fixtures here. Against the real API the
 * view id becomes a server-side filter so the client never holds conversations
 * the viewer's inbox access excludes (§11.3).
 */
export default function InboxPage() {
  const { viewId = "all", conversationId } = useParams();
  const navigate = useNavigate();
  const { viewer } = useWorkspace();
  const [showContext, setShowContext] = useState(true);

  const visible = useMemo(
    () => filterByView(conversations, viewId, viewer.id),
    [viewId, viewer.id],
  );

  const active = conversationId
    ? conversations.find((conversation) => conversation.id === conversationId)
    : undefined;

  const activeIndex = active ? visible.findIndex((item) => item.id === active.id) : -1;

  const open = (id: string) => navigate(`/inbox/${viewId}/${id}`);

  // j/k move through the list without leaving the keyboard (§6.2).
  useHotkey("j", () => {
    const next = visible[Math.min(activeIndex + 1, visible.length - 1)];
    if (next) open(next.id);
  });
  useHotkey("k", () => {
    const previous = visible[Math.max(activeIndex - 1, 0)];
    if (previous) open(previous.id);
  });

  return (
    <div className="flex h-full min-h-0">
      <ConversationList
        conversations={visible}
        activeId={active?.id ?? null}
        onSelect={open}
        viewName={viewLabel(viewId)}
      />

      {active ? (
        <ConversationPanel
          conversation={active}
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

      {active && (
        <aside
          className={cn(
            "w-context shrink-0 overflow-y-auto border-l border-line bg-surface",
            showContext ? "hidden xl:block" : "hidden",
          )}
          aria-label="Customer context"
        >
          <CustomerContextPanel customerId={active.customer_id} />
        </aside>
      )}
    </div>
  );
}

function filterByView(
  all: typeof conversations,
  viewId: string,
  viewerId: string,
): typeof conversations {
  switch (viewId) {
    case "all":
      return all.filter((item) => !["closed", "spam"].includes(item.state));
    case "unassigned":
      return all.filter((item) => item.assignee_id === null && item.state !== "closed");
    case "mine":
      return all.filter((item) => item.assignee_id === viewerId);
    case "breached":
      return all.filter((item) => item.sla?.state === "breached");
    case "approaching":
      return all.filter((item) => item.sla?.state === "approaching");
    case "waiting-support":
      return all.filter((item) => item.state === "waiting_for_support");
    case "waiting-customer":
      return all.filter((item) => ["waiting_for_customer", "pending"].includes(item.state));
    case "snoozed":
      return all.filter((item) => item.state === "snoozed");
    case "resolved":
      return all.filter((item) => item.state === "resolved");
    case "spam":
      return all.filter((item) => item.state === "spam");
    case "mentions":
    case "following":
      return all.slice(0, 3);
    default: {
      const inbox = inboxes.find((item) => item.slug === viewId);
      if (inbox) return all.filter((item) => item.inbox_id === inbox.id);
      // Saved views carry a real filter expression; the fixture build shows the
      // whole set rather than re-implementing the evaluator client-side.
      return all;
    }
  }
}

function viewLabel(viewId: string): string {
  const labels: Record<string, string> = {
    all: "All active",
    unassigned: "Unassigned",
    mine: "Assigned to me",
    mentions: "Mentions",
    following: "Following",
    breached: "Breached SLA",
    approaching: "Approaching SLA",
    "waiting-support": "Waiting on us",
    "waiting-customer": "Waiting on customer",
    snoozed: "Snoozed",
    resolved: "Resolved",
    spam: "Spam",
  };

  return (
    labels[viewId] ??
    inboxes.find((inbox) => inbox.slug === viewId)?.name ??
    savedViews.find((view) => view.id === viewId)?.name ??
    "Inbox"
  );
}
