import type { ReactNode } from "react";
import { AlertTriangle, WifiOff } from "lucide-react";

import { Button } from "./Button";
import { Callout } from "./Callout";
import { EmptyState } from "./EmptyState";
import { SkeletonRow } from "./Skeleton";
import { ApiError } from "../lib/client";

export type QueryBoundaryProps<T> = {
  /** The state from useQuery. */
  query: {
    data: T | undefined;
    error: unknown;
    isLoading: boolean;
    refetch: () => void;
  };
  /** Rendered once data has loaded. */
  children: (data: T) => ReactNode;
  /** Shown while the first load runs. Defaults to table-shaped skeleton rows. */
  loading?: ReactNode;
  /** Shown when the loaded data is empty. Decide emptiness with `isEmpty`. */
  empty?: ReactNode;
  isEmpty?: (data: T) => boolean;
  /** Overrides the default error presentation. */
  error?: (error: unknown, retry: () => void) => ReactNode;
};

/**
 * The three states every server-backed view has, rendered consistently.
 *
 * Written as one component because otherwise each of ~90 screens invents its
 * own answer to "what does loading look like", and they diverge — which is how
 * a product ends up with four spinners, two of which are centred.
 *
 * Errors are shown with a retry, never as a bare message. §6 of the design
 * notes asks empty states to explain purpose rather than announce emptiness;
 * the same applies to failures, which should say what to do next.
 */
export function QueryBoundary<T>({
  query,
  children,
  loading,
  empty,
  isEmpty,
  error,
}: QueryBoundaryProps<T>) {
  if (query.isLoading) {
    return <>{loading ?? <SkeletonRow count={6} />}</>;
  }

  if (query.error !== undefined) {
    if (error) return <>{error(query.error, query.refetch)}</>;
    return <QueryError error={query.error} retry={query.refetch} />;
  }

  if (query.data === undefined) {
    return <>{loading ?? <SkeletonRow count={6} />}</>;
  }

  if (isEmpty?.(query.data) && empty) {
    return <>{empty}</>;
  }

  return <>{children(query.data)}</>;
}

/**
 * The default failure presentation.
 *
 * It distinguishes the cases a user can act on. "You do not have permission"
 * needs no retry button — offering one invites them to click it repeatedly for
 * a result that will not change. A dropped connection, on the other hand, is
 * usually fixed by trying again.
 */
export function QueryError({ error, retry }: { error: unknown; retry: () => void }) {
  if (error instanceof ApiError && error.isForbidden) {
    return (
      <EmptyState
        icon={AlertTriangle}
        variant="error"
        title="You do not have access to this"
        description="Ask a workspace administrator if you need it."
      />
    );
  }

  if (error instanceof ApiError && error.isNotFound) {
    return (
      <EmptyState
        icon={AlertTriangle}
        variant="error"
        title="Not found"
        description="This may have been deleted, or the link may be wrong."
      />
    );
  }

  const isOffline = !(error instanceof ApiError);

  return (
    <Callout
      tone="danger"
      icon={isOffline ? <WifiOff size={16} /> : <AlertTriangle size={16} />}
      title={isOffline ? "Could not reach the server" : "Something went wrong"}
      actions={
        <Button size="sm" variant="secondary" onClick={retry}>
          Try again
        </Button>
      }
    >
      <p>{describe(error)}</p>
      {error instanceof ApiError && error.requestId ? (
        // Surfaced so a support report can be tied to a log line (§16). It is
        // deliberately unobtrusive: useful to quote, not worth reading.
        <p className="mt-1 font-mono text-[11px] opacity-60">Reference: {error.requestId}</p>
      ) : null}
    </Callout>
  );
}

function describe(error: unknown): string {
  if (error instanceof ApiError) return error.message;
  if (error instanceof Error) return "Check your connection and try again.";
  return "An unexpected error occurred.";
}
