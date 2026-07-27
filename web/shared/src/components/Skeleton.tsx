import { cn } from "../lib/cn";

/**
 * A sweeping highlight rather than a pulsing opacity. On a dark surface a pulse
 * reads as a flicker; a sweep reads as progress.
 */
export function Skeleton({ className }: { className?: string }) {
  return (
    <div
      aria-hidden="true"
      className={cn(
        "relative overflow-hidden rounded-sm bg-fill",
        "after:absolute after:inset-0 after:animate-shimmer",
        "after:bg-[linear-gradient(90deg,transparent,var(--hc-fill-hover),transparent)]",
        "after:bg-[length:200%_100%]",
        className,
      )}
    />
  );
}

/** Text placeholder with a ragged last line, so it reads as prose. */
export function SkeletonText({ lines = 3, className }: { lines?: number; className?: string }) {
  return (
    <div className={cn("flex flex-col gap-1.5", className)} role="status" aria-label="Loading">
      {Array.from({ length: lines }, (_, index) => (
        <Skeleton
          key={index}
          className={cn("h-3", index === lines - 1 && lines > 1 ? "w-3/5" : "w-full")}
        />
      ))}
    </div>
  );
}

/** Matches the conversation-list row geometry so the swap does not jump. */
export function SkeletonRow({ count = 6 }: { count?: number }) {
  return (
    <div role="status" aria-label="Loading list">
      {Array.from({ length: count }, (_, index) => (
        <div key={index} className="flex items-start gap-2.5 border-b border-line-subtle px-3 py-3">
          <Skeleton className="size-8 shrink-0 rounded-full" />
          <div className="flex min-w-0 flex-1 flex-col gap-1.5">
            <div className="flex items-center justify-between gap-2">
              <Skeleton className="h-3 w-28" />
              <Skeleton className="h-2.5 w-8" />
            </div>
            <Skeleton className="h-2.5 w-full" />
            <Skeleton className="h-2.5 w-2/3" />
          </div>
        </div>
      ))}
    </div>
  );
}
