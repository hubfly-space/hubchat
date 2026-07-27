import {
  Avatar,
  Badge,
  Button,
  Callout,
  ConversationStateBadge,
  Menu,
  MenuContent,
  MenuItem,
  MenuLabel,
  MenuSeparator,
  MenuSub,
  MenuTrigger,
  PriorityIndicator,
  SlaBadge,
  TagChip,
  Tooltip,
  formatDuration,
  type Conversation,
} from "@hubchat/shared";
import {
  AlertOctagon,
  Ban,
  BellOff,
  CheckCircle2,
  Clock,
  Combine,
  Download,
  Flag,
  Lightbulb,
  Link2,
  MoreHorizontal,
  PanelRightClose,
  Split,
  Tag,
  TicketPlus,
  Trash2,
  UserPlus,
} from "lucide-react";
import { Link } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";
import { messagesByConversation, tickets } from "../../data/fixtures";
import { Composer } from "./Composer";
import { MessageTimeline } from "./MessageTimeline";

/**
 * Centre pane: the conversation itself.
 *
 * Header carries state, not chrome. Every control in it maps to a §6.2
 * conversation action, and the four an agent uses hourly (assign, priority,
 * snooze, resolve) are surfaced as buttons; the rest live behind the overflow
 * menu rather than competing for the same row.
 */
export function ConversationPanel({
  conversation,
  onToggleContext,
}: {
  conversation: Conversation;
  onToggleContext: () => void;
}) {
  const { customerById, memberById, tagById } = useWorkspace();

  const customer = customerById(conversation.customer_id);
  const assignee = memberById(conversation.assignee_id);
  const viewers = conversation.viewers.map(memberById).filter(Boolean);
  const messages = messagesByConversation[conversation.id] ?? [];
  const ticket = tickets.find((item) => item.id === conversation.ticket_id);

  const slaRemaining =
    conversation.sla?.next_response_remaining ?? conversation.sla?.first_response_remaining ?? null;

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col bg-canvas">
      {/* Header ------------------------------------------------------------ */}
      <header className="shrink-0 border-b border-line bg-surface">
        <div className="flex items-start justify-between gap-4 px-4 py-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <h1 className="truncate text-sm font-semibold text-fg">
                {conversation.subject ?? `Conversation with ${customer?.name ?? "a visitor"}`}
              </h1>
              <ConversationStateBadge state={conversation.state} />
            </div>

            <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-fg-muted">
              <span className="capitalize">{conversation.channel}</span>
              <span aria-hidden="true">·</span>
              <span>{conversation.message_count} messages</span>

              {ticket && (
                <>
                  <span aria-hidden="true">·</span>
                  <Link
                    to={`/tickets/${ticket.id}`}
                    className="font-mono text-accent-text transition-colors hover:underline"
                  >
                    {ticket.prefix}-{ticket.number}
                  </Link>
                </>
              )}

              {conversation.tag_ids.map((tagId) => {
                const tag = tagById(tagId);
                return tag ? <TagChip key={tagId} label={tag.name} color={tag.color} /> : null;
              })}
            </div>
          </div>

          <div className="flex shrink-0 items-center gap-1">
            {conversation.sla && (
              <SlaBadge
                state={conversation.sla.state}
                remaining={slaRemaining != null ? formatDuration(slaRemaining) : undefined}
              />
            )}
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
              <AssigneeItems />
            </MenuContent>
          </Menu>

          <Menu>
            <MenuTrigger asChild>
              <Button variant="ghost" size="sm" leading={<PriorityIndicator priority={conversation.priority} />}>
                <span className="capitalize">{conversation.priority}</span>
              </Button>
            </MenuTrigger>
            <MenuContent>
              <MenuLabel>Priority</MenuLabel>
              {(["urgent", "high", "normal", "low"] as const).map((priority) => (
                <MenuItem key={priority} icon={<PriorityIndicator priority={priority} />}>
                  <span className="capitalize">{priority}</span>
                </MenuItem>
              ))}
            </MenuContent>
          </Menu>

          <Menu>
            <MenuTrigger asChild>
              <Button variant="ghost" size="sm" leading={<Clock />}>
                Snooze
              </Button>
            </MenuTrigger>
            <MenuContent>
              <MenuLabel>Snooze until</MenuLabel>
              <MenuItem>Later today · 17:00</MenuItem>
              <MenuItem>Tomorrow · 09:00</MenuItem>
              <MenuItem>Next week · Monday 09:00</MenuItem>
              <MenuSeparator />
              <MenuItem>Until the customer replies</MenuItem>
              <MenuItem>Pick a date and time…</MenuItem>
            </MenuContent>
          </Menu>

          <Tooltip content="Add tags" shortcut="l">
            <Button variant="ghost" size="sm" iconOnly aria-label="Add tags" leading={<Tag />} />
          </Tooltip>

          <span className="mx-1 h-4 w-px bg-line" aria-hidden="true" />

          <Button variant="secondary" size="sm" leading={<CheckCircle2 />}>
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
            <MenuContent align="end" className="w-60">
              <MenuLabel>Convert</MenuLabel>
              <MenuItem icon={<TicketPlus />}>Convert to ticket</MenuItem>
              <MenuItem icon={<Lightbulb />}>Create feedback item</MenuItem>

              <MenuSeparator />
              <MenuLabel>Organise</MenuLabel>
              <MenuItem icon={<Combine />}>Merge with another conversation…</MenuItem>
              <MenuItem icon={<Split />}>Split messages into a new ticket…</MenuItem>
              <MenuItem icon={<Link2 />}>Link related conversation…</MenuItem>
              <MenuSub label="Move to inbox" icon={<Flag />}>
                <MenuItem>Support</MenuItem>
                <MenuItem>Billing</MenuItem>
                <MenuItem>API &amp; Integrations</MenuItem>
              </MenuSub>

              <MenuSeparator />
              <MenuItem icon={<BellOff />}>Unfollow</MenuItem>
              <MenuItem icon={<Download />}>Export transcript</MenuItem>

              <MenuSeparator />
              <MenuItem icon={<AlertOctagon />}>Mark as spam</MenuItem>
              <MenuItem icon={<Ban />} destructive>
                Block this visitor
              </MenuItem>
              <MenuItem icon={<Trash2 />} destructive>
                Delete conversation
              </MenuItem>
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
            {viewers.map((viewer) => viewer!.name).join(", ")}{" "}
            {viewers.length === 1 ? "is" : "are"} also viewing this conversation.
          </Callout>
        )}

        {conversation.sla?.state === "breached" && (
          <Callout tone="danger" className="rounded-none border-x-0 border-b-0 px-4 py-2">
            This conversation breached its next-response target{" "}
            {slaRemaining != null ? formatDuration(Math.abs(slaRemaining)) : ""} ago.
          </Callout>
        )}

        {conversation.sla?.paused_reason && (
          <Callout tone="info" className="rounded-none border-x-0 border-b-0 px-4 py-2">
            SLA timers are paused — {conversation.sla.paused_reason.toLowerCase()}.
          </Callout>
        )}
      </header>

      {/* Timeline ---------------------------------------------------------- */}
      <div className="min-h-0 flex-1 overflow-y-auto">
        {messages.length > 0 ? (
          <MessageTimeline
            messages={messages}
            typing={customer?.presence === "online" ? { name: customer.name ?? "Visitor" } : null}
          />
        ) : (
          <div className="flex h-full items-center justify-center p-8 text-center text-xs text-fg-muted">
            No messages loaded for this conversation in the fixture dataset.
          </div>
        )}
      </div>

      <Composer customerName={customer?.name ?? "the visitor"} />
    </div>
  );
}

function AssigneeItems() {
  const { members, teams, viewer } = useWorkspace();

  return (
    <>
      <MenuItem icon={<Avatar name={viewer.name} seed={viewer.id} size="2xs" />} shortcut="a">
        Assign to me
      </MenuItem>
      <MenuSeparator />
      <MenuLabel>Teams</MenuLabel>
      {teams.map((team) => (
        <MenuItem key={team.id} icon={<Flag />}>
          {team.name}
        </MenuItem>
      ))}
      <MenuLabel>People</MenuLabel>
      {members
        .filter((member) => member.accepting_conversations)
        .map((member) => (
          <MenuItem
            key={member.id}
            icon={<Avatar name={member.name} seed={member.id} size="2xs" status={member.presence} />}
          >
            {member.name}
          </MenuItem>
        ))}
      <MenuSeparator />
      <MenuItem>
        <span className="flex items-center gap-2">
          <Badge tone="neutral">Unassign</Badge>
        </span>
      </MenuItem>
    </>
  );
}
