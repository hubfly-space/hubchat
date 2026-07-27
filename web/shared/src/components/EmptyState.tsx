import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../lib/cn";

export type EmptyStateProps = {
  icon?: LucideIcon;
  title: string;
  /** One sentence. Say what the thing is *for*, not that it is empty. */
  description?: ReactNode;
  action?: ReactNode;
  secondaryAction?: ReactNode;
  size?: "sm" | "md" | "lg";
  /** "filtered" is the no-results variant — same layout, different copy duty. */
  variant?: "empty" | "filtered" | "error";
  className?: string;
};

/**
 * Empty states carry a faint grid behind the glyph. It is the one decorative
 * flourish in the system, and it exists because a truly blank pane reads as a
 * failed load rather than an intentional zero state.
 */
export function EmptyState({
  icon: Icon,
  title,
  description,
  action,
  secondaryAction,
  size = "md",
  variant = "empty",
  className,
}: EmptyStateProps) {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center text-center",
        size === "sm" && "gap-2 px-6 py-8",
        size === "md" && "gap-3 px-8 py-14",
        size === "lg" && "gap-3 px-8 py-24",
        className,
      )}
    >
      {Icon && (
        <div className="relative mb-1 grid place-items-center">
          <div
            aria-hidden="true"
            className="hc-grid-bg absolute size-32 rounded-full opacity-60 [mask-image:radial-gradient(circle,#000_10%,transparent_68%)]"
          />
          <div
            className={cn(
              "relative grid place-items-center rounded-xl border border-line bg-surface shadow-1",
              size === "sm" ? "size-9" : "size-11",
              variant === "error" && "border-danger-border bg-danger-subtle",
            )}
          >
            <Icon
              aria-hidden="true"
              className={cn(
                size === "sm" ? "size-4" : "size-5",
                variant === "error" ? "text-danger-text" : "text-fg-muted",
              )}
            />
          </div>
        </div>
      )}

      <div className="max-w-measure-narrow">
        <p className={cn("font-semibold text-fg", size === "sm" ? "text-sm" : "text-md")}>
          {title}
        </p>
        {description && (
          <p className="mt-1 text-xs leading-normal text-fg-muted">{description}</p>
        )}
      </div>

      {(action || secondaryAction) && (
        <div className="mt-1 flex items-center gap-2">
          {action}
          {secondaryAction}
        </div>
      )}
    </div>
  );
}
