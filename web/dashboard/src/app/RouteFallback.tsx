import { Skeleton } from "@hubchat/shared";

/**
 * Shown while a lazy route chunk loads. Sketches the standard page shape —
 * header block, then content — so the layout does not jump when the real page
 * arrives. Deliberately not a spinner: on a fast connection a spinner appears
 * and vanishes inside 80ms, which reads as a flicker.
 */
export function RouteFallback() {
  return (
    <div className="flex h-full flex-col" role="status" aria-label="Loading page">
      <div className="shrink-0 border-b border-line bg-surface px-6 pb-4 pt-5">
        <Skeleton className="h-5 w-48" />
        <Skeleton className="mt-2 h-3 w-80" />
      </div>
      <div className="flex-1 space-y-4 p-6">
        <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
          {Array.from({ length: 4 }, (_, index) => (
            <Skeleton key={index} className="h-24 rounded-lg" />
          ))}
        </div>
        <Skeleton className="h-64 rounded-lg" />
      </div>
    </div>
  );
}
