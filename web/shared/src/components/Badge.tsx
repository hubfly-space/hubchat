import { forwardRef, type HTMLAttributes, type ReactNode } from "react";
import { cn } from "../lib/cn";
import type { ConversationState, Priority, SlaState, TicketStatus } from "../types";

export type BadgeTone =
  | "neutral"
  | "accent"
  | "success"
  | "warning"
  | "danger"
  | "info"
  | "system";

export type BadgeVariant = "subtle" | "outline" | "solid";

const TONES: Record<BadgeTone, Record<BadgeVariant, string>> = {
  neutral: {
    subtle: "bg-fill text-fg-secondary",
    outline: "border border-line text-fg-secondary",
    solid: "bg-fg-muted text-fg-inverse",
  },
  accent: {
    subtle: "bg-accent-subtle text-accent-text",
    outline: "border border-accent-border text-accent-text",
    solid: "bg-accent text-accent-fg",
  },
  success: {
    subtle: "bg-success-subtle text-success-text",
    outline: "border border-success-border text-success-text",
    solid: "bg-success text-fg-inverse",
  },
  warning: {
    subtle: "bg-warning-subtle text-warning-text",
    outline: "border border-warning-border text-warning-text",
    solid: "bg-warning text-fg-inverse",
  },
  danger: {
    subtle: "bg-danger-subtle text-danger-text",
    outline: "border border-danger-border text-danger-text",
    solid: "bg-danger text-danger-fg",
  },
  info: {
    subtle: "bg-info-subtle text-info-text",
    outline: "border border-info-border text-info-text",
    solid: "bg-info text-accent-fg",
  },
  system: {
    subtle: "bg-system-subtle text-system",
    outline: "border border-system-border text-system",
    solid: "bg-system text-fg-inverse",
  },
};

export type BadgeProps = HTMLAttributes<HTMLSpanElement> & {
  tone?: BadgeTone;
  variant?: BadgeVariant;
  size?: "sm" | "md";
  /** A leading dot instead of an icon. Cheaper visually in dense rows. */
  dot?: boolean;
  leading?: ReactNode;
};

export const Badge = forwardRef<HTMLSpanElement, BadgeProps>(function Badge(
  { tone = "neutral", variant = "subtle", size = "sm", dot, leading, className, children, ...props },
  ref,
) {
  return (
    <span
      ref={ref}
      className={cn(
        "inline-flex shrink-0 select-none items-center gap-1 whitespace-nowrap rounded-sm font-medium",
        size === "sm" ? "h-[18px] px-1.5 text-2xs" : "h-5 px-2 text-xs",
        "[&_svg]:size-3 [&_svg]:shrink-0",
        TONES[tone][variant],
        className,
      )}
      {...props}
    >
      {dot && <span className="size-1.5 shrink-0 rounded-full bg-current" aria-hidden="true" />}
      {leading}
      {children}
    </span>
  );
});

/* -------------------------------------------------------------------------- */
/*  Domain badges                                                              */
/*  Colour mapping lives here and only here. Every surface — inbox, ticket      */
/*  table, portal, report — reads the same mapping, so a "pending" thread is    */
/*  the same colour everywhere an agent or a customer can see it.               */
/* -------------------------------------------------------------------------- */

const CONVERSATION_STATE: Record<ConversationState, { label: string; tone: BadgeTone }> = {
  new: { label: "New", tone: "accent" },
  open: { label: "Open", tone: "accent" },
  pending: { label: "Pending", tone: "warning" },
  waiting_for_customer: { label: "Waiting on customer", tone: "neutral" },
  waiting_for_support: { label: "Waiting on us", tone: "warning" },
  snoozed: { label: "Snoozed", tone: "neutral" },
  resolved: { label: "Resolved", tone: "success" },
  closed: { label: "Closed", tone: "neutral" },
  spam: { label: "Spam", tone: "danger" },
};

export function ConversationStateBadge({
  state,
  variant = "subtle",
  className,
}: {
  state: ConversationState;
  variant?: BadgeVariant;
  className?: string;
}) {
  const config = CONVERSATION_STATE[state];
  return (
    <Badge tone={config.tone} variant={variant} dot className={className}>
      {config.label}
    </Badge>
  );
}

const TICKET_STATUS: Record<TicketStatus, { label: string; tone: BadgeTone }> = {
  new: { label: "New", tone: "accent" },
  open: { label: "Open", tone: "accent" },
  pending: { label: "Pending", tone: "warning" },
  on_hold: { label: "On hold", tone: "neutral" },
  resolved: { label: "Resolved", tone: "success" },
  closed: { label: "Closed", tone: "neutral" },
};

export function TicketStatusBadge({
  status,
  variant = "subtle",
  className,
}: {
  status: TicketStatus;
  variant?: BadgeVariant;
  className?: string;
}) {
  const config = TICKET_STATUS[status];
  return (
    <Badge tone={config.tone} variant={variant} dot className={className}>
      {config.label}
    </Badge>
  );
}

/**
 * Priority is encoded by *bar count* first and colour second, so it survives
 * greyscale, colour-blindness, and a glance at 200ms (§21 contrast).
 */
export function PriorityIndicator({
  priority,
  showLabel = false,
  className,
}: {
  priority: Priority;
  showLabel?: boolean;
  className?: string;
}) {
  const level = { low: 1, normal: 2, high: 3, urgent: 4 }[priority];
  const tone = {
    low: "bg-fg-disabled",
    normal: "bg-fg-muted",
    high: "bg-warning",
    urgent: "bg-danger",
  }[priority];

  return (
    <span
      className={cn("inline-flex items-center gap-1.5", className)}
      title={`Priority: ${priority}`}
    >
      <span className="flex items-end gap-px" aria-hidden="true">
        {[1, 2, 3, 4].map((step) => (
          <span
            key={step}
            className={cn(
              "w-[3px] rounded-[1px] transition-colors",
              step === 1 && "h-1.5",
              step === 2 && "h-2",
              step === 3 && "h-2.5",
              step === 4 && "h-3",
              step <= level ? tone : "bg-line-strong",
            )}
          />
        ))}
      </span>
      {showLabel && (
        <span className="text-xs capitalize text-fg-secondary">{priority}</span>
      )}
      <span className="sr-only">Priority {priority}</span>
    </span>
  );
}

const SLA_TONE: Record<SlaState, BadgeTone> = {
  none: "neutral",
  active: "neutral",
  paused: "neutral",
  approaching: "warning",
  breached: "danger",
  met: "success",
};

/**
 * SLA is the single most time-sensitive signal in the inbox, so it is the one
 * place a badge is allowed to animate — and only when actually breaching.
 */
export function SlaBadge({
  state,
  remaining,
  className,
}: {
  state: SlaState;
  /** Formatted countdown, e.g. "2h 14m". Omitted for paused/none. */
  remaining?: string;
  className?: string;
}) {
  if (state === "none") return null;

  const label =
    state === "breached"
      ? remaining
        ? `Breached ${remaining}`
        : "Breached"
      : state === "paused"
        ? "SLA paused"
        : state === "met"
          ? "SLA met"
          : remaining ?? "SLA";

  return (
    <Badge
      tone={SLA_TONE[state]}
      variant={state === "breached" ? "solid" : "subtle"}
      className={cn("tabular", state === "breached" && "animate-pulse-ring", className)}
      title={`SLA ${state}`}
    >
      {label}
    </Badge>
  );
}

/* -------------------------------------------------------------------------- */
/*  Dots & counters                                                            */
/* -------------------------------------------------------------------------- */

export type StatusDotProps = {
  status: "online" | "away" | "busy" | "offline" | "live";
  size?: "sm" | "md";
  /** Adds an expanding ring. Reserved for genuinely live realtime presence. */
  pulse?: boolean;
  className?: string;
};

export function StatusDot({ status, size = "sm", pulse, className }: StatusDotProps) {
  const tone = {
    online: "bg-success",
    away: "bg-warning",
    busy: "bg-danger",
    offline: "bg-fg-disabled",
    live: "bg-live",
  }[status];

  return (
    <span
      role="img"
      aria-label={status}
      className={cn(
        "inline-block shrink-0 rounded-full",
        size === "sm" ? "size-2" : "size-2.5",
        tone,
        pulse && "animate-pulse-ring",
        className,
      )}
    />
  );
}

/** Unread / queue counter. Caps at 99+ so it never widens a nav row. */
export function CountBadge({
  count,
  tone = "neutral",
  className,
}: {
  count: number;
  tone?: BadgeTone;
  className?: string;
}) {
  if (count <= 0) return null;

  return (
    <span
      className={cn(
        "inline-flex h-4 min-w-4 items-center justify-center rounded-full px-1 text-2xs font-semibold tabular",
        tone === "accent" ? "bg-accent text-accent-fg" : "bg-fill text-fg-secondary",
        className,
      )}
    >
      {count > 99 ? "99+" : count}
    </span>
  );
}

/** Workspace tag chip. Colour comes from the six-slot chart palette only. */
export function TagChip({
  label,
  color = 3,
  onRemove,
  className,
}: {
  label: string;
  color?: 1 | 2 | 3 | 4 | 5 | 6;
  onRemove?: () => void;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex h-[18px] shrink-0 items-center gap-1 rounded-sm bg-fill pl-1.5 text-2xs text-fg-secondary",
        onRemove ? "pr-0.5" : "pr-1.5",
        className,
      )}
    >
      <span
        aria-hidden="true"
        className="size-1.5 shrink-0 rounded-[2px]"
        style={{ backgroundColor: `var(--hc-chart-${color})` }}
      />
      {label}
      {onRemove && (
        <button
          type="button"
          onClick={onRemove}
          aria-label={`Remove tag ${label}`}
          className="grid size-3.5 place-items-center rounded-[3px] text-fg-muted transition-colors hover:bg-fill-hover hover:text-fg"
        >
          <svg viewBox="0 0 8 8" className="size-2 stroke-current" strokeWidth="1.5" fill="none">
            <path d="M1 1l6 6M7 1L1 7" strokeLinecap="round" />
          </svg>
        </button>
      )}
    </span>
  );
}
