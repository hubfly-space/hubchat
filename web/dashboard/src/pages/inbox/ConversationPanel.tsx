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
  QueryBoundary,
  TagChip,
  Tooltip,
  idempotencyKey,
  invalidate,
  useMutation,
  useQuery,
  type Conversation,
  type Customer,
  type Inbox,
  type Message,
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
  MoreHorizontal,
  PanelRightClose,
  Tag,
  UserPlus,
} from "lucide-react";
import { useState } from "react";
import { useWorkspace } from "../../app/workspace-context";
import { Composer } from "./Composer";
import { MessageTimeline } from "./MessageTimeline";

/**
 * Centre pane: the conversation itself.
 *
 * Header carries state, not chrome. Every control in it maps to a real
 * backend action; the four an agent uses hourly (assign, priority, snooze,
 * resolve) are surfaced as buttons, the rest live behind the overflow menu.
 * A few actions from the original design — converting to a ticket, splitting
 * into one, linking a related conversation — have no backend yet (tickets
 * are a later stage) and are left out entirely rather than wired to nothing.
 */
export function ConversationPanel({
  conversation,
  onToggleContext,
}: {
  conversation: Conversation;
  onToggleContext: () => void;
}) {
  const { memberById, tagById, viewer } = useWorkspace();
  const [managingTags, setManagingTags] = useState(false);
  const [merging, setMerging] = useState(false);
  const [blocking, setBlocking] = useState(false);

  const assignee = memberById(conversation.assignee_id);
  const viewers = conversation.viewers.map(memberById).filter(Boolean);

  const messages = useQuery<{ data: Message[] }>(
    ["conversation-messages", conversation.id],
    (signal) => api.get(`/conversations/${conversation.id}/messages`, { signal }),
  );
  const customer = useQuery<Customer>(
    conversation.customer_id ? ["customer", conversation.customer_id] : null,
    (signal) => api.get(`/customers/${conversation.customer_id}`, { signal }),
  );

  const markRead = useMutation<void, unknown>(() => api.post(`/conversations/${conversation.id}/read`));
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

  const sendMessage = async (body: string, kind: "reply" | "note", fileIDs: string[]) => {
    await api.post(`/conversations/${conversation.id}/messages`, { body, kind, author_name: viewer.name, file_ids: fileIDs }, { idempotencyKey: idempotencyKey() });
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
            onClick={() => void setState.mutate("resolved").catch(() => {})}
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
              <MoveToInboxMenu
                currentInboxId={conversation.inbox_id}
                onPick={(id) => void setInbox.mutate(id).catch(() => {})}
              />

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
      </header>

      {/* Timeline ---------------------------------------------------------- */}
      <div className="min-h-0 flex-1 overflow-y-auto" onClick={() => void markRead.mutate().catch(() => {})}>
        <QueryBoundary query={messages}>
          {({ data }) =>
            data.length > 0 ? (
              <MessageTimeline messages={data} />
            ) : (
              <div className="flex h-full items-center justify-center p-8 text-center text-xs text-fg-muted">
                No messages yet.
              </div>
            )
          }
        </QueryBoundary>
      </div>

      <Composer
        conversationId={conversation.id}
        customerName={customer.data?.name ?? "the visitor"}
        onSend={sendMessage}
      />

      {managingTags && (
        <ManageTagsDialog conversation={conversation} onClose={() => setManagingTags(false)} />
      )}
      {merging && conversation.customer_id && (
        <MergeDialog conversation={conversation} customerId={conversation.customer_id} onClose={() => setMerging(false)} />
      )}
      {blocking && customer.data && (
        <BlockVisitorDialog customer={customer.data} onClose={() => setBlocking(false)} />
      )}
    </div>
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
  const inboxes = useQuery<{ data: Inbox[] }>(["inboxes"], (signal) => api.get("/inboxes", { signal }));

  return (
    <MenuSub label="Move to inbox" icon={<Flag />}>
      {(inboxes.data?.data ?? [])
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
