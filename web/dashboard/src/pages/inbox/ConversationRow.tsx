import {
  Avatar,
  AvatarGroup,
  Checkbox,
  PriorityIndicator,
  SlaBadge,
  TagChip,
  Tooltip,
  cn,
  formatDuration,
  formatRelativeShort,
  type Conversation,
} from "@hubchat/shared";
import { AtSign, Globe, Mail, MessageSquare, Radio, Terminal, UserCog } from "lucide-react";
import { NOW } from "../../data/fixtures";
import { useWorkspace } from "../../app/workspace-context";

const CHANNEL_ICON = {
  widget: MessageSquare,
  portal: Globe,
  email: Mail,
  form: AtSign,
  api: Terminal,
  manual: UserCog,
} as const;

/**
 * One row of the conversation list.
 *
 * The information budget here is brutal: roughly 372×64px must carry who, what,
 * when, how urgent, whether anyone owns it, and whether it is about to breach.
 * The ordering below is the priority order an agent scans in — identity, then
 * recency, then the SLA state, then everything else.
 */
export function ConversationRow({
  conversation,
  selected,
  active,
  onSelect,
  onToggleSelect,
  showSelection,
}: {
  conversation: Conversation;
  selected: boolean;
  active: boolean;
  onSelect: () => void;
  onToggleSelect: () => void;
  showSelection: boolean;
}) {
  const { customerById, memberById, tagById } = useWorkspace();

  const customer = customerById(conversation.customer_id);
  const assignee = memberById(conversation.assignee_id);
  const ChannelIcon = CHANNEL_ICON[conversation.channel];
  const viewers = conversation.viewers.map(memberById).filter(Boolean);

  const slaRemaining =
    conversation.sla?.next_response_remaining ?? conversation.sla?.first_response_remaining ?? null;

  return (
    <div
      role="option"
      aria-selected={active}
      tabIndex={0}
      onClick={onSelect}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onSelect();
        }
      }}
      data-unread={conversation.unread}
      className={cn(
        "group relative flex cursor-pointer gap-2.5 border-b border-line-subtle px-3",
        "py-[var(--hc-list-item-py)]",
        "transition-colors duration-fast",
        active ? "bg-accent-subtle" : "hover:bg-surface-hover",
        selected && !active && "bg-fill",
      )}
    >
      {/* Unread marker on the leading edge — a dot inside the row would compete
          with the avatar's presence dot. */}
      <span
        aria-hidden="true"
        className={cn(
          "absolute inset-y-0 left-0 w-0.5",
          conversation.unread ? "bg-accent" : "bg-transparent",
        )}
      />

      <div className="relative shrink-0 pt-0.5">
        {showSelection ? (
          <span onClick={(event) => event.stopPropagation()} className="grid size-8 place-items-center">
            <Checkbox
              checked={selected}
              onCheckedChange={onToggleSelect}
              aria-label={`Select conversation from ${customer?.name ?? "anonymous visitor"}`}
            />
          </span>
        ) : (
          <Avatar
            name={customer?.name}
            seed={customer?.id ?? conversation.id}
            size="md"
            status={customer?.presence === "online" ? "online" : null}
          />
        )}
      </div>

      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <span
            className={cn(
              "min-w-0 flex-1 truncate text-sm",
              conversation.unread ? "font-semibold text-fg" : "text-fg-secondary",
            )}
          >
            {customer?.name ?? "Anonymous visitor"}
          </span>

          <Tooltip content={`Via ${conversation.channel}`}>
            <ChannelIcon aria-hidden="true" className="size-3 shrink-0 text-fg-disabled" />
          </Tooltip>

          <span className="shrink-0 text-2xs tabular text-fg-muted">
            {formatRelativeShort(conversation.last_message_at, NOW)}
          </span>
        </div>

        {conversation.subject && (
          <p
            className={cn(
              "mt-0.5 truncate text-xs",
              conversation.unread ? "text-fg" : "text-fg-secondary",
            )}
          >
            {conversation.subject}
          </p>
        )}

        <p className="mt-0.5 line-clamp-2 text-xs leading-snug text-fg-muted">
          {conversation.last_message_preview}
        </p>

        <div className="mt-1.5 flex items-center gap-1.5">
          <PriorityIndicator priority={conversation.priority} />

          {conversation.sla && conversation.sla.state !== "none" && (
            <SlaBadge
              state={conversation.sla.state}
              remaining={slaRemaining != null ? formatDuration(slaRemaining) : undefined}
            />
          )}

          {conversation.tag_ids.slice(0, 2).map((tagId) => {
            const tag = tagById(tagId);
            return tag ? <TagChip key={tagId} label={tag.name} color={tag.color} /> : null;
          })}

          {conversation.tag_ids.length > 2 && (
            <span className="text-2xs text-fg-disabled">+{conversation.tag_ids.length - 2}</span>
          )}

          <span className="ml-auto flex shrink-0 items-center gap-1">
            {/* §6.12 collision prevention — someone else has this open. */}
            {viewers.length > 0 && (
              <Tooltip content={`${viewers.map((v) => v!.name).join(", ")} viewing`}>
                <span className="flex items-center">
                  <Radio aria-hidden="true" className="mr-0.5 size-3 animate-pulse text-warning" />
                  <AvatarGroup
                    size="2xs"
                    max={2}
                    people={viewers.map((viewer) => ({ id: viewer!.id, name: viewer!.name }))}
                  />
                </span>
              </Tooltip>
            )}

            {assignee ? (
              <Tooltip content={`Assigned to ${assignee.name}`}>
                <span>
                  <Avatar name={assignee.name} seed={assignee.id} size="2xs" />
                </span>
              </Tooltip>
            ) : (
              <Tooltip content="Unassigned">
                <span className="size-4 rounded-full border border-dashed border-line-loud" />
              </Tooltip>
            )}
          </span>
        </div>
      </div>
    </div>
  );
}
