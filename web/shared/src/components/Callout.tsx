import { AlertTriangle, CheckCircle2, Info, ShieldAlert, Sparkles } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../lib/cn";

export type CalloutTone = "info" | "success" | "warning" | "danger" | "system";

const TONES: Record<CalloutTone, { className: string; icon: typeof Info }> = {
  info: { className: "border-info-border bg-info-subtle text-info-text", icon: Info },
  success: {
    className: "border-success-border bg-success-subtle text-success-text",
    icon: CheckCircle2,
  },
  warning: {
    className: "border-warning-border bg-warning-subtle text-warning-text",
    icon: AlertTriangle,
  },
  danger: {
    className: "border-danger-border bg-danger-subtle text-danger-text",
    icon: ShieldAlert,
  },
  system: { className: "border-system-border bg-system-subtle text-system", icon: Sparkles },
};

export type CalloutProps = {
  tone?: CalloutTone;
  title?: ReactNode;
  children?: ReactNode;
  actions?: ReactNode;
  icon?: ReactNode;
  className?: string;
};

/**
 * Inline explanatory block. Distinct from Toast (transient, global) and from
 * Field error text (bound to one control) — a Callout explains a *situation*
 * that persists on the page: a disabled webhook, a paused SLA, a pending
 * domain verification.
 */
export function Callout({ tone = "info", title, children, actions, icon, className }: CalloutProps) {
  const config = TONES[tone];
  const Icon = config.icon;

  return (
    <div
      role={tone === "danger" ? "alert" : "note"}
      className={cn(
        "flex items-start gap-2.5 rounded-md border px-3 py-2.5",
        config.className,
        className,
      )}
    >
      <span className="mt-px shrink-0 [&_svg]:size-4">
        {icon ?? <Icon aria-hidden="true" />}
      </span>

      <div className="min-w-0 flex-1">
        {title && <p className="text-sm font-medium">{title}</p>}
        {children && (
          <div className={cn("text-xs leading-normal opacity-90", title && "mt-1")}>{children}</div>
        )}
      </div>

      {actions && <div className="flex shrink-0 items-center gap-1.5">{actions}</div>}
    </div>
  );
}
