import {
  Avatar,
  Tooltip,
  cn,
  formatBytes,
  formatDateTime,
  formatTime,
  type Message,
} from "@hubchat/shared";
import {
  Check,
  CheckCheck,
  Download,
  Lock,
  Paperclip,
  Workflow,
} from "lucide-react";
import { Fragment } from "react";

/**
 * The conversation timeline.
 *
 * Four visual registers, and the distinction between them is load-bearing —
 * mistaking an internal note for a public reply is the single most damaging
 * error an agent can make in this product:
 *
 *   customer reply  — left, neutral surface, avatar
 *   agent reply     — right, accent-tinted, no avatar (it is always "us")
 *   internal note   — full width, amber, padlock, dashed edge
 *   system event    — centred hairline with inline text, never a bubble
 */
export function MessageTimeline({
  messages,
  typing,
}: {
  messages: Message[];
  typing?: { name: string } | null;
}) {
  let lastDay = "";

  return (
    <div className="flex flex-col gap-3 px-5 py-4">
      {messages.map((message) => {
        const day = message.created_at.slice(0, 10);
        const showDivider = day !== lastDay;
        lastDay = day;

        return (
          <Fragment key={message.id}>
            {showDivider && <DayDivider date={message.created_at} />}
            {message.kind === "event" ? (
              <SystemEvent message={message} />
            ) : message.kind === "note" ? (
              <InternalNote message={message} />
            ) : message.author_type === "customer" ? (
              <CustomerMessage message={message} />
            ) : (
              <AgentMessage message={message} />
            )}
          </Fragment>
        );
      })}

      {typing && <TypingIndicator name={typing.name} />}
    </div>
  );
}

function DayDivider({ date }: { date: string }) {
  return (
    <div className="my-2 flex items-center gap-3">
      <span className="h-px flex-1 bg-line" aria-hidden="true" />
      <span className="text-2xs font-medium uppercase tracking-caps text-fg-muted">
        {formatDateTime(date).split(",")[0]}
      </span>
      <span className="h-px flex-1 bg-line" aria-hidden="true" />
    </div>
  );
}

function CustomerMessage({ message }: { message: Message }) {
  return (
    <div className="group/msg flex max-w-[76%] items-end gap-2">
      <Avatar name={message.author_name} seed={message.author_id ?? message.id} size="sm" />
      <div className="min-w-0">
        <div className="rounded-lg rounded-bl-xs border border-line bg-surface px-3 py-2">
          <p className="whitespace-pre-wrap text-sm leading-normal text-fg">{message.body}</p>
          <Attachments message={message} />
        </div>
        <MessageMeta message={message} align="left" />
      </div>
    </div>
  );
}

function AgentMessage({ message }: { message: Message }) {
  return (
    <div className="group/msg flex max-w-[76%] items-end gap-2 self-end">
      <div className="min-w-0">
        <div
          className={cn(
            "rounded-lg rounded-br-xs border px-3 py-2",
            "border-accent-border bg-accent-subtle",
          )}
        >
          <p className="whitespace-pre-wrap text-sm leading-normal text-fg">{message.body}</p>
          <Attachments message={message} />
        </div>
        <MessageMeta message={message} align="right" />
      </div>
    </div>
  );
}

function InternalNote({ message }: { message: Message }) {
  return (
    <div className="group/msg w-full">
      <div
        className={cn(
          "rounded-lg border border-dashed border-warning-border bg-warning-subtle px-3 py-2",
        )}
      >
        <p className="mb-1 flex items-center gap-1.5 text-2xs font-semibold uppercase tracking-caps text-warning-text">
          <Lock aria-hidden="true" className="size-3" />
          Internal note · not visible to the customer
        </p>
        <p className="whitespace-pre-wrap text-sm leading-normal text-fg">{message.body}</p>
      </div>
      <MessageMeta message={message} align="left" />
    </div>
  );
}

function SystemEvent({ message }: { message: Message }) {
  const automated = message.author_type === "automation";
  const detail = describeEvent(message);

  return (
    <div className="my-1 flex items-center gap-3">
      <span className="h-px flex-1 bg-line-subtle" aria-hidden="true" />
      <span
        className={cn(
          "flex items-center gap-1.5 text-2xs",
          automated ? "text-system" : "text-fg-muted",
        )}
      >
        {automated && <Workflow aria-hidden="true" className="size-3" />}
        {detail}
        <span className="text-fg-disabled">· {formatTime(message.created_at)}</span>
      </span>
      <span className="h-px flex-1 bg-line-subtle" aria-hidden="true" />
    </div>
  );
}

function describeEvent(message: Message): string {
  const data = (message.event_data ?? {}) as Record<string, string>;
  switch (message.event_type) {
    case "conversation.assigned":
      return `Assigned to ${data.to}${data.by ? ` by ${data.by}` : ""}`;
    case "sla.approaching":
      return `${data.target} due in ${data.remaining}`;
    case "customer.event":
      return `Event · ${data.type}`;
    default:
      return message.event_type ?? "System event";
  }
}

function Attachments({ message }: { message: Message }) {
  if (message.attachments.length === 0) return null;

  return (
    <ul className="mt-2 flex flex-col gap-1">
      {message.attachments.map((attachment) => (
        <li key={attachment.id}>
          <a
            href={attachment.url}
            className="flex items-center gap-2 rounded-md border border-line bg-inset px-2 py-1.5 transition-colors hover:border-line-strong"
          >
            <Paperclip aria-hidden="true" className="size-3.5 shrink-0 text-fg-muted" />
            <span className="min-w-0 flex-1">
              <span className="block truncate text-xs text-fg">{attachment.name}</span>
              <span className="block text-2xs tabular text-fg-muted">
                {formatBytes(attachment.size_bytes)}
              </span>
            </span>
            <Download aria-hidden="true" className="size-3.5 shrink-0 text-fg-muted" />
          </a>
        </li>
      ))}
    </ul>
  );
}

function MessageMeta({ message, align }: { message: Message; align: "left" | "right" }) {
  return (
    <p
      className={cn(
        "mt-1 flex items-center gap-1.5 text-2xs text-fg-muted",
        align === "right" && "justify-end",
      )}
    >
      <Tooltip content={formatDateTime(message.created_at)}>
        <span className="tabular">{formatTime(message.created_at)}</span>
      </Tooltip>

      {message.author_type !== "customer" && <span>· {message.author_name}</span>}
      {message.edited_at && <span>· edited</span>}

      {message.author_type === "agent" && message.kind === "reply" && (
        <Tooltip content={message.delivery === "read" ? "Read by customer" : "Delivered"}>
          <span>
            {message.delivery === "read" ? (
              <CheckCheck aria-hidden="true" className="size-3 text-accent-text" />
            ) : (
              <Check aria-hidden="true" className="size-3" />
            )}
          </span>
        </Tooltip>
      )}
    </p>
  );
}

function TypingIndicator({ name }: { name: string }) {
  return (
    <div className="flex items-center gap-2" aria-live="polite">
      <Avatar name={name} size="sm" />
      <span className="flex items-center gap-1 rounded-lg rounded-bl-xs border border-line bg-surface px-3 py-2.5">
        {[0, 1, 2].map((index) => (
          <span
            key={index}
            className="size-1.5 rounded-full bg-fg-muted"
            style={{
              animation: "hc-typing-bounce 1.2s ease-in-out infinite",
              animationDelay: `${index * 0.16}s`,
            }}
          />
        ))}
        <span className="sr-only">{name} is typing</span>
      </span>
    </div>
  );
}
