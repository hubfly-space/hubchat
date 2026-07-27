import { Skeleton } from "@hubchat/shared";

/** Sketches the standard portal page shape so the layout does not jump. */
export function RouteFallback() {
  return (
    <div role="status" aria-label="Loading">
      <Skeleton className="h-3 w-40" />
      <Skeleton className="mt-4 h-7 w-72" />
      <Skeleton className="mt-3 h-3 w-full max-w-md" />
      <div className="mt-8 space-y-2">
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={index} className="h-20 rounded-lg" />
        ))}
      </div>
    </div>
  );
}
