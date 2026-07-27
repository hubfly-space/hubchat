import { cn } from "../lib/cn";

export type ProgressProps = {
  /** 0–1. Omit for an indeterminate bar. */
  value?: number;
  tone?: "accent" | "success" | "warning" | "danger";
  size?: "xs" | "sm" | "md";
  label?: string;
  className?: string;
};

export function Progress({ value, tone = "accent", size = "sm", label, className }: ProgressProps) {
  const indeterminate = value == null;
  const percent = Math.min(Math.max((value ?? 0) * 100, 0), 100);

  const fill = {
    accent: "bg-accent",
    success: "bg-success",
    warning: "bg-warning",
    danger: "bg-danger",
  }[tone];

  return (
    <div
      role="progressbar"
      aria-valuenow={indeterminate ? undefined : Math.round(percent)}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-label={label}
      className={cn(
        "w-full overflow-hidden rounded-full bg-chart-track",
        size === "xs" && "h-1",
        size === "sm" && "h-1.5",
        size === "md" && "h-2",
        className,
      )}
    >
      <div
        className={cn(
          "h-full rounded-full",
          fill,
          indeterminate ? "w-1/3 animate-indeterminate" : "transition-[width] duration-slow ease-out",
        )}
        style={indeterminate ? undefined : { width: `${percent}%` }}
      />
    </div>
  );
}

/**
 * Usage meter for workspace limits (§23). Crosses into warning at 80% and
 * danger at 95% automatically, so no call site has to remember the thresholds.
 */
export function UsageMeter({
  used,
  limit,
  label,
  unit,
  className,
}: {
  used: number;
  limit: number | null;
  label: string;
  unit?: string;
  className?: string;
}) {
  const ratio = limit ? used / limit : 0;
  const tone = ratio >= 0.95 ? "danger" : ratio >= 0.8 ? "warning" : "accent";

  return (
    <div className={cn("min-w-0", className)}>
      <div className="mb-1.5 flex items-baseline justify-between gap-3 text-xs">
        <span className="truncate text-fg-secondary">{label}</span>
        <span className="shrink-0 tabular text-fg-muted">
          {used.toLocaleString()}
          {limit != null ? ` / ${limit.toLocaleString()}` : ""}
          {unit ? ` ${unit}` : ""}
        </span>
      </div>
      <Progress value={limit ? ratio : 0} tone={tone} size="xs" label={label} />
    </div>
  );
}

/**
 * Circular countdown for SLA timers. Depletes clockwise and switches tone at
 * the policy's warning threshold.
 */
export function RingProgress({
  value,
  size = 28,
  thickness = 3,
  tone = "accent",
  children,
  className,
}: {
  value: number;
  size?: number;
  thickness?: number;
  tone?: "accent" | "success" | "warning" | "danger";
  children?: React.ReactNode;
  className?: string;
}) {
  const radius = (size - thickness) / 2;
  const circumference = 2 * Math.PI * radius;
  const clamped = Math.min(Math.max(value, 0), 1);

  const stroke = {
    accent: "var(--hc-accent)",
    success: "var(--hc-success)",
    warning: "var(--hc-warning)",
    danger: "var(--hc-danger)",
  }[tone];

  return (
    <span className={cn("relative inline-grid place-items-center", className)} style={{ width: size, height: size }}>
      <svg viewBox={`0 0 ${size} ${size}`} className="-rotate-90" style={{ width: size, height: size }}>
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          stroke="var(--hc-chart-track)"
          strokeWidth={thickness}
        />
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          stroke={stroke}
          strokeWidth={thickness}
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={circumference * (1 - clamped)}
          className="transition-[stroke-dashoffset] duration-slow ease-out"
        />
      </svg>
      {children && (
        <span className="absolute text-[9px] font-semibold tabular text-fg-secondary">{children}</span>
      )}
    </span>
  );
}
