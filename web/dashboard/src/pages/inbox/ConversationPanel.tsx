import {
  api,
  ApiError,
  Avatar,
  Badge,
  Button,
  Callout,
  ConversationStateBadge,
  Dialog,
  DialogClose,
  DialogContent,
  EmptyState,
  Menu,
  MenuContent,
  MenuItem,
  MenuLabel,
  MenuSeparator,
  MenuSub,
  MenuTrigger,
  PriorityIndicator,
  TagChip,
  Tooltip,
  idempotencyKey,
  invalidate,
  useInfinite,
  useAllPages,
  useMutation,
  useQuery,
  type Conversation,
  type ConversationLink,
  type Customer,
  type FeedbackItem,
  type FeedbackLink,
  type Inbox,
  type Message,
  type Paginated,
} from "@hubchat/shared";
import {
  AlertOctagon,
  Ban,
  BellOff,
  BellRing,
  CheckCircle2,
  Clock,
  Combine,
  Download,
  Flag,
  Link2,
  Lightbulb,
  MoreHorizontal,
  PanelRightClose,
  Tag,
  TicketPlus,
  UserPlus,
} from "lucide-react";
import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";
import { Composer } from "./Composer";
import { MessageTimeline } from "./MessageTimeline";

/**
 * Centre pane: the conversation itself.
 *
 * Header carries state, not chrome. Every control in it maps to a real
 * backend action; the four an agent uses hourly (assign, priority, snooze,
 * resolve) are surfaced as buttons, the rest live behind the overflow menu.
 * Conversion to a ticket is a durable, permissioned action backed by the
 * ticket service. Related-conversation links are durable, workspace-scoped
 * records and are managed from the overflow menu below.
 */
export function ConversationPanel({
  conversation,
  onToggleContext,
  onResolved,
  markReadOnOpen,
}: {
  conversation: Conversation;
  onToggleContext: () => void;
  onResolved: () => void;
  markReadOnOpen: boolean;
}) {
  const navigate = useNavigate();
  const { memberById, members, tagById, viewer, can, workspace } = useWorkspace();
  const [managingTags, setManagingTags] = useState(false);
  const [merging, setMerging] = useState(false);
  const [linking, setLinking] = useState(false);
  const [linkingFeedback, setLinkingFeedback] = useState(false);
  const [blocking, setBlocking] = useState(false);

  const assignee = memberById(conversation.assignee_id);
  const viewers = conversation.viewers.map(memberById).filter(Boolean);

  const messages = useInfinite<Message>(
    ["conversation-messages", conversation.id],
    (cursor, signal) => api.get<Paginated<Message>>(`/conversations/${conversation.id}/messages?limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }),
  );
  const customer = useQuery<Customer>(
    conversation.customer_id ? ["customer", conversation.customer_id] : null,
    (signal) => api.get(`/customers/${conversation.customer_id}`, { signal }),
  );
  const linkedTicket = useQuery<{ number: number; prefix: string }>(
    conversation.ticket_id ? ["ticket", workspace.id, conversation.ticket_id] : null,
    (signal) => api.get(`/tickets/${conversation.ticket_id}`, { signal, workspaceId: workspace.id }),
    { enabled: Boolean(conversation.ticket_id) },
  );
  const links = useQuery<{ data: ConversationLink[] }>(
    ["conversation-links", conversation.id],
    (signal) => api.get(`/conversations/${conversation.id}/links`, { signal }),
  );
  const feedbackLinks = useQuery<Paginated<FeedbackItem>>(
    ["conversation-feedback-links", conversation.id],
    (signal) => api.get(`/feedback/items?conversation_id=${encodeURIComponent(conversation.id)}&link_state=linked&sort=recent&limit=25`, { signal }),
    { enabled: can("feedback.moderate") },
  );

  const markRead = useMutation<void, unknown>(() => api.post(`/conversations/${conversation.id}/read`));
  const markReadConversation = markRead.mutate;
  const isFollowing = viewers.some((v) => v!.id === viewer.id) || false;

  const setAssignee = useMutation<string | null, unknown>(
    (assigneeId) => api.patch(`/conversations/${conversation.id}/assignee`, { assignee_id: assigneeId }),
    { invalidates: [["conversations"], ["conversation", conversation.id]] },
  );
  const setPriority = useMutation<string, unknown>(
    (priority) => api.patch(`/conversations/${conversation.id}/priority`, { priority }),
    { invalidates: [["conversations"], ["conversation", conversation.id]] },
  );
  const setState = useMutation<string, unknown>(
    (state) => api.patch(`/conversations/${conversation.id}/state`, { state }),
    { invalidates: [["conversations"], ["conversation", conversation.id]] },
  );
  const snooze = useMutation<string, unknown>(
    (until) => api.post(`/conversations/${conversation.id}/snooze`, { until }),
    { invalidates: [["conversations"], ["conversation", conversation.id]] },
  );
  const setInbox = useMutation<string, unknown>(
    (inboxId) => api.patch(`/conversations/${conversation.id}/inbox`, { inbox_id: inboxId }),
    { invalidates: [["conversations"], ["conversation", conversation.id]] },
  );
  const toggleFollow = useMutation<void, unknown>(
    () =>
      isFollowing
        ? api.delete(`/conversations/${conversation.id}/followers/me`)
        : api.put(`/conversations/${conversation.id}/followers/me`),
    { invalidates: [["conversations"], ["conversation", conversation.id]] },
  );
  const convertToTicket = useMutation<void, { id: string }>(
    () => api.post(`/conversations/${conversation.id}/ticket`, {}, { idempotencyKey: idempotencyKey() }),
    {
      invalidates: [["conversations"], ["conversation", conversation.id]],
      onSuccess: (ticket) => navigate(`/tickets/${ticket.id}`),
    },
  );

  useEffect(() => {
    if (!markReadOnOpen || !conversation.unread) return;
    void markReadConversation().catch(() => {});
  }, [conversation.id, conversation.unread, markReadConversation, markReadOnOpen]);

  const sendMessage = async (body: string, kind: "reply" | "note", fileIDs: string[], mentionedMemberIDs: string[]) => {
    await api.post(`/conversations/${conversation.id}/messages`, { body, kind, author_name: viewer.name, file_ids: fileIDs, mentioned_member_ids: mentionedMemberIDs }, { idempotencyKey: idempotencyKey() });
    invalidate(["conversation-messages", conversation.id]);
    invalidate(["conversations"]);
  };

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col bg-canvas">
      {/* Header ------------------------------------------------------------ */}
      <header className="shrink-0 border-b border-line bg-surface">
        <div className="flex items-start justify-between gap-4 px-4 py-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <h1 className="truncate text-sm font-semibold text-fg">
                {conversation.subject ?? `Conversation with ${customer.data?.name ?? "a visitor"}`}
              </h1>
              <ConversationStateBadge state={conversation.state} />
            </div>

            <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-fg-muted">
              <span className="capitalize">{conversation.channel}</span>
              <span aria-hidden="true">·</span>
              <span>{conversation.message_count} messages</span>

              {conversation.tag_ids.map((tagId) => {
                const tag = tagById(tagId);
                return tag ? <TagChip key={tagId} label={tag.name} color={tag.color} /> : null;
              })}
            </div>
          </div>

          <div className="flex shrink-0 items-center gap-1">
            <Tooltip content="Hide context panel">
              <Button
                variant="ghost"
                size="sm"
                iconOnly
                aria-label="Hide context panel"
                leading={<PanelRightClose />}
                onClick={onToggleContext}
                className="hidden xl:inline-flex"
              />
            </Tooltip>
          </div>
        </div>

        {/* Action bar ------------------------------------------------------ */}
        <div className="flex items-center gap-1 border-t border-line-subtle px-3 py-1.5">
          <Menu>
            <MenuTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                loading={setAssignee.isPending}
                leading={
                  assignee ? (
                    <Avatar name={assignee.name} seed={assignee.id} size="2xs" />
                  ) : (
                    <UserPlus />
                  )
                }
              >
                {assignee?.name ?? "Unassigned"}
              </Button>
            </MenuTrigger>
            <MenuContent className="w-56">
              <MenuLabel>Assign to</MenuLabel>
              <AssigneeItems
                onPick={(id) => void setAssignee.mutate(id).catch(() => {})}
                currentAssigneeId={conversation.assignee_id}
              />
            </MenuContent>
          </Menu>

          <Menu>
            <MenuTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                loading={setPriority.isPending}
                leading={<PriorityIndicator priority={conversation.priority} />}
              >
                <span className="capitalize">{conversation.priority}</span>
              </Button>
            </MenuTrigger>
            <MenuContent>
              <MenuLabel>Priority</MenuLabel>
              {(["urgent", "high", "normal", "low"] as const).map((priority) => (
                <MenuItem
                  key={priority}
                  icon={<PriorityIndicator priority={priority} />}
                  onSelect={() => void setPriority.mutate(priority).catch(() => {})}
                >
                  <span className="capitalize">{priority}</span>
                </MenuItem>
              ))}
            </MenuContent>
          </Menu>

          <Menu>
            <MenuTrigger asChild>
              <Button variant="ghost" size="sm" loading={snooze.isPending} leading={<Clock />}>
                Snooze
              </Button>
            </MenuTrigger>
            <MenuContent>
              <MenuLabel>Snooze until</MenuLabel>
              {snoozePresets().map((preset) => (
                <MenuItem key={preset.label} onSelect={() => void snooze.mutate(preset.at.toISOString()).catch(() => {})}>
                  {preset.label}
                </MenuItem>
              ))}
            </MenuContent>
          </Menu>

          <Tooltip content="Manage tags" shortcut="l">
            <Button
              variant="ghost"
              size="sm"
              iconOnly
              aria-label="Manage tags"
              leading={<Tag />}
              onClick={() => setManagingTags(true)}
            />
          </Tooltip>

          <span className="mx-1 h-4 w-px bg-line" aria-hidden="true" />

          <Button
            variant="secondary"
            size="sm"
            leading={<CheckCircle2 />}
            loading={setState.isPending}
            disabled={conversation.state === "resolved"}
            onClick={() => void setState.mutate("resolved").then(onResolved).catch(() => {})}
          >
            Resolve
          </Button>

          <Menu>
            <MenuTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                iconOnly
                aria-label="More actions"
                leading={<MoreHorizontal />}
                className="ml-auto"
              />
            </MenuTrigger>
            <MenuContent align="end" className="w-64">
              <MenuLabel>Organise</MenuLabel>
              <MenuItem icon={<Combine />} onSelect={() => setMerging(true)}>
                Merge with another conversation…
              </MenuItem>
              <MenuItem icon={<Link2 />} disabled={!conversation.customer_id} onSelect={() => setLinking(true)}>
                Link a related conversation…
              </MenuItem>
              {can("feedback.moderate") ? (
                <MenuItem icon={<Lightbulb />} onSelect={() => setLinkingFeedback(true)}>
                  Link to feedback…
                </MenuItem>
              ) : null}
              <MoveToInboxMenu
                currentInboxId={conversation.inbox_id}
                onPick={(id) => void setInbox.mutate(id).catch(() => {})}
              />
              {conversation.ticket_id ? (
                <MenuItem icon={<TicketPlus />} onSelect={() => navigate(`/tickets/${conversation.ticket_id}`)}>
                  Open linked ticket
                </MenuItem>
              ) : can("ticket.manage") ? (
                <MenuItem icon={<TicketPlus />} disabled={convertToTicket.isPending} onSelect={() => void convertToTicket.mutate().catch(() => {})}>
                  Convert to ticket
                </MenuItem>
              ) : null}

              <MenuSeparator />
              <MenuItem
                icon={isFollowing ? <BellOff /> : <BellRing />}
                onSelect={() => void toggleFollow.mutate().catch(() => {})}
              >
                {isFollowing ? "Unfollow" : "Follow"}
              </MenuItem>
              <MenuItem icon={<Download />} onSelect={() => window.open(`/api/v1/conversations/${conversation.id}/transcript`, "_blank")}>
                Export transcript
              </MenuItem>

              <MenuSeparator />
              <MenuItem
                icon={<AlertOctagon />}
                onSelect={() => void setState.mutate("spam").catch(() => {})}
              >
                Mark as spam
              </MenuItem>
              {customer.data && (
                <MenuItem icon={<Ban />} destructive onSelect={() => setBlocking(true)}>
                  Block this visitor
                </MenuItem>
              )}
            </MenuContent>
          </Menu>
        </div>

        {/* §6.12 — warn, never silently overwrite. */}
        {viewers.length > 0 && (
          <Callout
            tone="warning"
            className="rounded-none border-x-0 border-b-0 px-4 py-2"
            icon={<Avatar name={viewers[0]!.name} seed={viewers[0]!.id} size="xs" />}
          >
            {viewers.map((v) => v!.name).join(", ")} {viewers.length === 1 ? "is" : "are"} also viewing
            this conversation.
          </Callout>
        )}
        {links.data?.data.length ? (
          <div className="flex flex-wrap items-center gap-2 border-t border-line-subtle px-4 py-2 text-xs">
            <span className="text-fg-muted">Related</span>
            {links.data.data.map((link) => {
              const otherID = link.source_id === conversation.id ? link.target_id : link.source_id;
              return (
                <Link key={link.id} to={`/inbox/all/${otherID}`} className="rounded-md bg-inset px-2 py-1 text-accent-text hover:underline">
                  {link.relation.replaceAll("_", " ")} · {otherID}
                </Link>
              );
            })}
          </div>
        ) : null}
        {feedbackLinks.data?.data.length ? (
          <div className="flex flex-wrap items-center gap-2 border-t border-line-subtle px-4 py-2 text-xs">
            <span className="text-fg-muted">Feedback</span>
            {feedbackLinks.data.data.map((item) => (
              <Link key={item.id} to={`/feedback/items/${item.id}`} className="max-w-full truncate rounded-md bg-inset px-2 py-1 text-accent-text hover:underline">
                {item.title}
              </Link>
            ))}
          </div>
        ) : null}
      </header>

      {/* Timeline ---------------------------------------------------------- */}
      <div className="min-h-0 flex-1 overflow-y-auto" onClick={markReadOnOpen ? undefined : () => void markReadConversation().catch(() => {})}>
        {messages.isLoading ? <p className="p-8 text-center text-xs text-fg-muted">Loading messages…</p> : messages.error ? <p className="p-8 text-center text-xs text-danger">Could not load messages.</p> : messages.items.length > 0 ? <><div className="flex justify-center px-5 pt-4">{messages.hasMore && <Button variant="ghost" size="xs" loading={messages.isFetching} onClick={() => void messages.fetchNext()}>Load older messages</Button>}</div><MessageTimeline messages={messages.items} /></> : <div className="flex h-full items-center justify-center p-8 text-center text-xs text-fg-muted">No messages yet.</div>}
      </div>

      <Composer
        workspaceId={workspace.id}
        canUseMacros={can("automation.manage")}
        conversationId={conversation.id}
        customerName={customer.data?.name ?? "the visitor"}
        ticketNumber={linkedTicket.data ? `${linkedTicket.data.prefix}-${linkedTicket.data.number}` : undefined}
        mentionMembers={members.filter((member) => member.id !== viewer.id && member.accepting_conversations)}
        onSend={sendMessage}
      />

      {managingTags && (
        <ManageTagsDialog conversation={conversation} onClose={() => setManagingTags(false)} />
      )}
      {merging && conversation.customer_id && (
        <MergeDialog conversation={conversation} customerId={conversation.customer_id} onClose={() => setMerging(false)} />
      )}
      {linking && conversation.customer_id && (
        <LinkDialog conversation={conversation} customerId={conversation.customer_id} existing={links.data?.data ?? []} onClose={() => setLinking(false)} />
      )}
      {linkingFeedback && (
        <FeedbackLinkDialog conversation={conversation} onClose={() => setLinkingFeedback(false)} />
      )}
      {blocking && customer.data && (
        <BlockVisitorDialog customer={customer.data} onClose={() => setBlocking(false)} />
      )}
    </div>
  );
}

function FeedbackLinkDialog({ conversation, onClose }: { conversation: Conversation; onClose: () => void }) {
  const [query, setQuery] = useState("");
  const items = useInfinite<FeedbackItem>(["feedback-link-items", query], (cursor, signal) => {
    const params = new URLSearchParams({ sort: "recent", q: query, conversation_id: conversation.id, link_state: "available", limit: "50" });
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<FeedbackItem>>(`/feedback/items?${params.toString()}`, { signal });
  });
  const link = useMutation<{ feedback_item_id: string; conversation_id: string; ticket_id: string }, FeedbackLink>(
    (input) => api.post(`/feedback/items/${input.feedback_item_id}/links`, { conversation_id: conversation.id, ticket_id: "" }, { idempotencyKey: idempotencyKey() }),
    {
      onSuccess: () => {
        invalidate(["conversation-feedback-links", conversation.id]);
        onClose();
      },
    },
  );
  const normalized = query.trim().toLowerCase();
  const candidates = items.items.filter((item) => !normalized || `${item.title} ${item.description}`.toLowerCase().includes(normalized));

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent title="Link to feedback" description="Connect this conversation to a customer request so its support history stays together.">
        <input aria-label="Search feedback items" className="h-9 w-full rounded-md border border-line bg-surface px-2 text-sm text-fg" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search feedback items…" autoFocus />
        {items.isLoading ? <p className="mt-3 text-sm text-fg-muted">Loading feedback…</p> : items.error ? <p className="mt-3 text-sm text-danger">Could not load feedback items.</p> : candidates.length === 0 ? <EmptyState size="sm" title="No matching feedback items" description="Try another search or create feedback from the customer request." /> : <ul className="mt-3 flex max-h-64 flex-col gap-1 overflow-y-auto">{candidates.map((item) => <li key={item.id}><button type="button" disabled={link.isPending} className="w-full rounded-md px-2 py-2 text-left hover:bg-inset" onClick={() => void link.mutate({ feedback_item_id: item.id, conversation_id: "", ticket_id: "" }).catch(() => {})}><span className="block truncate text-sm text-fg">{item.title}</span><span className="block truncate text-2xs text-fg-muted">{item.status.replaceAll("_", " ")} · {item.vote_count} votes</span></button></li>)}</ul>}
        {items.hasMore && <Button variant="ghost" size="sm" loading={items.isFetching} onClick={() => void items.fetchNext()}>Load more feedback</Button>}
        {link.error ? <Callout tone="danger" className="mt-3">Could not link this feedback item.</Callout> : null}
      </DialogContent>
    </Dialog>
  );
}

function snoozePresets() {
  const now = new Date();
  const later = new Date(now);
  later.setHours(17, 0, 0, 0);
  if (later <= now) later.setDate(later.getDate() + 1);

  const tomorrow = new Date(now);
  tomorrow.setDate(tomorrow.getDate() + 1);
  tomorrow.setHours(9, 0, 0, 0);

  const nextMonday = new Date(now);
  const daysUntilMonday = ((8 - nextMonday.getDay()) % 7) || 7;
  nextMonday.setDate(nextMonday.getDate() + daysUntilMonday);
  nextMonday.setHours(9, 0, 0, 0);

  return [
    { label: "Later today · 17:00", at: later },
    { label: "Tomorrow · 09:00", at: tomorrow },
    { label: "Next week · Monday 09:00", at: nextMonday },
  ];
}

function AssigneeItems({
  onPick,
  currentAssigneeId,
}: {
  onPick: (id: string | null) => void;
  currentAssigneeId: string | null;
}) {
  const { members, viewer } = useWorkspace();

  return (
    <>
      <MenuItem icon={<Avatar name={viewer.name} seed={viewer.id} size="2xs" />} onSelect={() => onPick(viewer.id)}>
        Assign to me
      </MenuItem>
      <MenuSeparator />
      <MenuLabel>People</MenuLabel>
      {members
        .filter((member) => member.accepting_conversations)
        .map((member) => (
          <MenuItem
            key={member.id}
            icon={<Avatar name={member.name} seed={member.id} size="2xs" status={member.presence} />}
            onSelect={() => onPick(member.id)}
          >
            {member.name}
          </MenuItem>
        ))}
      <MenuSeparator />
      <MenuItem disabled={currentAssigneeId === null} onSelect={() => onPick(null)}>
        <span className="flex items-center gap-2">
          <Badge tone="neutral">Unassign</Badge>
        </span>
      </MenuItem>
    </>
  );
}

function MoveToInboxMenu({ currentInboxId, onPick }: { currentInboxId: string; onPick: (id: string) => void }) {
  const inboxes = useAllPages<Inbox>(["inboxes", "lookup"], (cursor, signal) => api.get<Paginated<Inbox>>(`/inboxes?limit=200${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }));

  return (
    <MenuSub label="Move to inbox" icon={<Flag />}>
      {inboxes.items
        .filter((inbox) => inbox.id !== currentInboxId)
        .map((inbox) => (
          <MenuItem key={inbox.id} onSelect={() => onPick(inbox.id)}>
            {inbox.name}
          </MenuItem>
        ))}
    </MenuSub>
  );
}

function ManageTagsDialog({ conversation, onClose }: { conversation: Conversation; onClose: () => void }) {
  const { tags } = useWorkspace();

  const toggle = useMutation<{ tagId: string; add: boolean }, unknown>(
    ({ tagId, add }) =>
      add
        ? api.post(`/conversations/${conversation.id}/tags`, { tag_id: tagId })
        : api.delete(`/conversations/${conversation.id}/tags/${tagId}`),
    { invalidates: [["conversations"], ["conversation", conversation.id]] },
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title="Tags"
        footer={
          <Button variant="primary" size="sm" onClick={onClose}>
            Done
          </Button>
        }
      >
        <div className="flex flex-col gap-1">
          {tags.length === 0 ? (
            <EmptyState size="sm" title="No tags yet" description="Create tags under Settings → Tags." />
          ) : (
            tags.map((tag) => {
              const active = conversation.tag_ids.includes(tag.id);
              return (
                <button
                  key={tag.id}
                  type="button"
                  onClick={() => void toggle.mutate({ tagId: tag.id, add: !active }).catch(() => {})}
                  className="flex items-center justify-between gap-3 rounded-md px-2 py-2 text-left hover:bg-inset"
                >
                  <TagChip label={tag.name} color={tag.color} />
                  {active && <Badge tone="accent">Applied</Badge>}
                </button>
              );
            })
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function MergeDialog({
  conversation,
  customerId,
  onClose,
}: {
  conversation: Conversation;
  customerId: string;
  onClose: () => void;
}) {
  const candidates = useQuery<{ data: Conversation[] }>(
    ["conversations", "merge-candidates", customerId],
    (signal) => api.get(`/conversations?customer_id=${customerId}&state=new,open,pending,waiting_for_customer,waiting_for_support`, { signal }),
  );
  const others = (candidates.data?.data ?? []).filter((c) => c.id !== conversation.id);

  const merge = useMutation<string, unknown>(
    (targetId) => api.post(`/conversations/${conversation.id}/merge`, { target_id: targetId }),
    {
      invalidates: [["conversations"]],
      onSuccess: onClose,
    },
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title="Merge into another conversation"
        description="Every message here moves to the conversation you choose, and this one closes."
      >
        {merge.error ? (
          <Callout tone="danger" className="mb-3">
            {merge.error instanceof ApiError ? merge.error.message : "Could not merge these conversations."}
          </Callout>
        ) : null}
        {others.length === 0 ? (
          <EmptyState size="sm" title="No other open conversations from this visitor" />
        ) : (
          <ul className="flex flex-col gap-1">
            {others.map((other) => (
              <li key={other.id}>
                <button
                  type="button"
                  onClick={() => void merge.mutate(other.id).catch(() => {})}
                  className="w-full rounded-md px-2 py-2 text-left text-sm hover:bg-inset"
                >
                  <span className="block truncate text-fg">{other.subject ?? other.last_message_preview}</span>
                  <span className="block text-xs text-fg-muted capitalize">{other.state.replace(/_/g, " ")}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  );
}

function LinkDialog({
  conversation,
  customerId,
  existing,
  onClose,
}: {
  conversation: Conversation;
  customerId: string;
  existing: ConversationLink[];
  onClose: () => void;
}) {
  const [relation, setRelation] = useState<ConversationLink["relation"]>("related");
  const candidates = useQuery<{ data: Conversation[] }>(
    ["conversations", "link-candidates", customerId],
    (signal) => api.get(`/conversations?customer_id=${encodeURIComponent(customerId)}&limit=50&state=new,open,pending,waiting_for_customer,waiting_for_support,resolved,closed`, { signal }),
  );
  const linkedIDs = new Set(existing.flatMap((link) => [link.source_id, link.target_id]));
  const others = (candidates.data?.data ?? []).filter((item) => item.id !== conversation.id && !linkedIDs.has(item.id));
  const link = useMutation<{ target_id: string; relation: ConversationLink["relation"] }, ConversationLink>(
    (input) => api.post(`/conversations/${conversation.id}/links`, input, { idempotencyKey: idempotencyKey() }),
    {
      invalidates: [["conversation-links", conversation.id]],
      onSuccess: onClose,
    },
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent title="Link a conversation" description="Keep related support threads visible without merging their message histories.">
        <label className="mb-3 block text-xs text-fg-muted">
          Relationship
          <select className="mt-1 h-9 w-full rounded-md border border-line bg-surface px-2 text-sm text-fg" value={relation} onChange={(event) => setRelation(event.target.value as ConversationLink["relation"])}>
            <option value="related">Related</option>
            <option value="duplicate_of">Duplicate of</option>
            <option value="follow_up">Follow-up</option>
          </select>
        </label>
        {link.error ? <Callout tone="danger" className="mb-3">Could not link that conversation.</Callout> : null}
        {candidates.isLoading ? (
          <p className="text-sm text-fg-muted">Loading other conversations…</p>
        ) : candidates.error ? (
          <p className="text-sm text-danger">Could not load link candidates.</p>
        ) : others.length === 0 ? (
          <EmptyState size="sm" title="No other conversations to link" description="This customer has no additional unlinked conversations." />
        ) : (
          <ul className="flex max-h-64 flex-col gap-1 overflow-y-auto">
            {others.map((other) => (
              <li key={other.id}>
                <button
                  type="button"
                  disabled={link.isPending}
                  onClick={() => void link.mutate({ target_id: other.id, relation }).catch(() => {})}
                  className="w-full rounded-md px-2 py-2 text-left text-sm hover:bg-inset disabled:opacity-50"
                >
                  <span className="block truncate text-fg">{other.subject ?? other.last_message_preview}</span>
                  <span className="block text-xs text-fg-muted">{other.state.replace(/_/g, " ")} · {other.id}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  );
}

function BlockVisitorDialog({ customer, onClose }: { customer: Customer; onClose: () => void }) {
  const block = useMutation<void, unknown>(
    () => api.post("/blocked-contacts", { kind: "customer", value: customer.id, reason: "Blocked from conversation" }),
    { onSuccess: onClose },
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={`Block ${customer.name ?? "this visitor"}?`}
        footer={
          <>
            <DialogClose asChild>
              <Button variant="ghost" size="sm">
                Cancel
              </Button>
            </DialogClose>
            <Button variant="danger" size="sm" loading={block.isPending} onClick={() => void block.mutate().catch(() => {})}>
              Block
            </Button>
          </>
        }
      >
        <p className="text-sm text-fg-muted">
          Future conversations from this customer will be refused. This does not affect conversations
          already open.
        </p>
        {block.error ? (
          <Callout tone="danger" className="mt-3">
            {block.error instanceof ApiError ? block.error.message : "Could not block this visitor."}
          </Callout>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
