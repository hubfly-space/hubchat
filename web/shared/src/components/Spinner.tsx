import { cn } from "../lib/cn";

export type SpinnerProps = {
  className?: string;
  /** Announced to screen readers when the spinner is the only feedback. */
  label?: string;
};

/**
 * A 3/4 arc rather than a dotted ring — it reads as motion at 14px, where a
 * dotted spinner turns into a grey smudge.
 */
export function Spinner({ className, label }: SpinnerProps) {
  return (
    <>
      <svg
        viewBox="0 0 16 16"
        fill="none"
        aria-hidden="true"
        className={cn("size-3.5 shrink-0 animate-spin", className)}
      >
        <circle cx="8" cy="8" r="6.5" stroke="currentColor" strokeOpacity="0.22" strokeWidth="2" />
        <path
          d="M14.5 8a6.5 6.5 0 0 0-6.5-6.5"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
        />
      </svg>
      {label && <span className="sr-only">{label}</span>}
    </>
  );
}

/** Full-region loading state for panes that have nothing to show yet. */
export function LoadingPane({ label = "Loading" }: { label?: string }) {
  return (
    <div
      role="status"
      className="flex min-h-40 flex-1 flex-col items-center justify-center gap-2 text-fg-muted"
    >
      <Spinner className="size-4" />
      <span className="text-xs">{label}</span>
    </div>
  );
}
