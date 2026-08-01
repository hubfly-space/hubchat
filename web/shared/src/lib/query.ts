/**
 * A small server-state cache with React bindings.
 *
 * Deliberately not a general-purpose query library. It does four things the
 * ~90 screens in this product actually need — read with loading/error state,
 * write with optimistic rollback, invalidate by key prefix, and page through
 * cursors — and nothing else.
 *
 * The caching is intentionally simple: entries are kept until invalidated or
 * garbage-collected, and a mounted query refetches when it is stale. There is
 * no request-priority queue, no structural sharing, no suspense integration.
 * Realtime events do the work that aggressive cache tuning would otherwise
 * have to: when the server says something changed, the affected keys are
 * invalidated, so the cache does not have to guess.
 */

import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";

import { AbortedError, ApiError } from "./client";
import type { Paginated } from "../types";

/**
 * A cache key is an array, so it can be matched by prefix.
 *
 * `["conversations", inboxId]` is invalidated by `["conversations"]`, which is
 * what lets a `message.created` event refresh every conversation list without
 * knowing which ones are mounted.
 */
export type QueryKey = readonly (string | number | boolean | null | undefined)[];

type Entry<T = unknown> = {
  data?: T;
  error?: unknown;
  /** When the data was last successfully fetched. */
  updatedAt: number;
  /** A fetch is in flight. */
  fetching: boolean;
  promise?: Promise<unknown>;
  listeners: Set<() => void>;
  /** Snapshot handed to useSyncExternalStore; replaced whenever anything changes. */
  snapshot: QueryState<T>;
  /**
   * The most recently mounted `useQuery`'s fetch function, kept so
   * `invalidate()` can force an actual network refetch for a key that is
   * currently on screen — not just mark it stale for whenever it next
   * happens to remount. Last writer wins; any mounted caller of the same key
   * fetches the same thing, so it does not matter which one's closure runs.
   */
  refetch?: () => void;
};

export type QueryState<T> = {
  data: T | undefined;
  error: unknown;
  /** No data yet and a fetch is running — render a skeleton. */
  isLoading: boolean;
  /** A fetch is running but stale data is available — keep rendering it. */
  isFetching: boolean;
  isError: boolean;
  isSuccess: boolean;
  updatedAt: number;
};

const cache = new Map<string, Entry>();

function serialize(key: QueryKey): string {
  return JSON.stringify(key);
}

function entryFor<T>(key: string): Entry<T> {
  let entry = cache.get(key) as Entry<T> | undefined;
  if (!entry) {
    entry = {
      updatedAt: 0,
      fetching: false,
      listeners: new Set(),
      snapshot: {
        data: undefined,
        error: undefined,
        isLoading: false,
        isFetching: false,
        isError: false,
        isSuccess: false,
        updatedAt: 0,
      },
    };
    cache.set(key, entry as Entry);
  }
  return entry;
}

/**
 * Rebuilds the snapshot and notifies subscribers.
 *
 * The snapshot is a new object every time because `useSyncExternalStore`
 * compares by identity — mutating in place would render nothing.
 */
function publish<T>(entry: Entry<T>) {
  entry.snapshot = {
    data: entry.data,
    error: entry.error,
    isLoading: entry.fetching && entry.updatedAt === 0,
    isFetching: entry.fetching,
    isError: entry.error !== undefined,
    isSuccess: entry.updatedAt > 0 && entry.error === undefined,
    updatedAt: entry.updatedAt,
  };
  entry.listeners.forEach((listen) => listen());
}

async function runFetch<T>(key: string, fetcher: () => Promise<T>): Promise<T | undefined> {
  const entry = entryFor<T>(key);

  // Another component already started this exact fetch. Share it rather than
  // duplicating the round trip.
  if (entry.promise) return entry.promise as Promise<T>;

  entry.fetching = true;
  publish(entry);

  const promise = fetcher()
    .then((data) => {
      entry.data = data;
      entry.error = undefined;
      entry.updatedAt = Date.now();
      return data;
    })
    .catch((error: unknown) => {
      // A cancelled request is not a failure the user should see — it means
      // the component asking for it went away.
      if (!(error instanceof AbortedError)) {
        entry.error = error;
      }
      return undefined;
    })
    .finally(() => {
      entry.fetching = false;
      entry.promise = undefined;
      publish(entry);
    });

  entry.promise = promise;
  return promise;
}

export type UseQueryOptions = {
  /**
   * How long data stays fresh. A mounted query with data newer than this does
   * not refetch on mount.
   *
   * The default is deliberately short. Most lists in this product are also
   * kept current by realtime invalidation, so staleness is a fallback for the
   * cases realtime does not cover rather than the primary mechanism.
   */
  staleTime?: number;
  /** Set false to hold off — for queries that depend on an id not yet known. */
  enabled?: boolean;
  /** Refetch when the tab regains focus. On by default for lists. */
  refetchOnFocus?: boolean;
};

/**
 * Reads server state.
 *
 * The fetcher receives an AbortSignal; pass it through to the client so an
 * unmount cancels the request rather than leaving it to resolve into nothing.
 */
export function useQuery<T>(
  key: QueryKey | null,
  fetcher: (signal: AbortSignal) => Promise<T>,
  options: UseQueryOptions = {},
): QueryState<T> & { refetch: () => void } {
  const { staleTime = 10_000, enabled = true, refetchOnFocus = true } = options;

  const serialized = key === null ? null : serialize(key);
  const active = enabled && serialized !== null;

  // Keeping the fetcher in a ref means an inline arrow function does not
  // retrigger the effect on every render.
  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;

  const subscribe = useCallback(
    (onChange: () => void) => {
      if (!serialized) return () => {};
      const entry = entryFor(serialized);
      entry.listeners.add(onChange);
      return () => {
        entry.listeners.delete(onChange);
      };
    },
    [serialized],
  );

  const getSnapshot = useCallback(() => {
    if (!serialized) return EMPTY_STATE as QueryState<T>;
    return entryFor<T>(serialized).snapshot;
  }, [serialized]);

  const state = useSyncExternalStore(subscribe, getSnapshot, getSnapshot);

  const load = useCallback(
    (force: boolean) => {
      if (!serialized || !active) return;
      const entry = entryFor<T>(serialized);
      if (!force && entry.updatedAt > 0 && Date.now() - entry.updatedAt < staleTime) return;

      const controller = new AbortController();
      void runFetch(serialized, () => fetcherRef.current(controller.signal));
    },
    [serialized, active, staleTime],
  );

  useEffect(() => {
    load(false);
  }, [load]);

  // Registers this mount as the one `invalidate()` wakes up when this key is
  // marked stale while on screen (see the `refetch` field on Entry). Whoever
  // registers last wins across multiple mounts of the same key, which is
  // fine — they all fetch the same thing.
  useEffect(() => {
    if (!serialized || !active) return;
    entryFor(serialized).refetch = () => load(true);
  }, [serialized, active, load]);

  useEffect(() => {
    if (!refetchOnFocus || !active) return;
    const onFocus = () => load(false);
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [refetchOnFocus, active, load]);

  const refetch = useCallback(() => load(true), [load]);

  return { ...state, refetch };
}

const EMPTY_STATE: QueryState<unknown> = {
  data: undefined,
  error: undefined,
  isLoading: false,
  isFetching: false,
  isError: false,
  isSuccess: false,
  updatedAt: 0,
};

/**
 * Drops cached data for every key starting with `prefix`, and refetches the
 * ones currently mounted.
 *
 * Prefix matching is what makes this usable from a realtime handler: an event
 * knows a conversation changed, not which of the eleven mounted queries happen
 * to include it.
 */
export function invalidate(prefix: QueryKey) {
  const needle = serialize(prefix);
  // Compare against the prefix as a JSON array fragment, so ["a"] matches
  // ["a","b"] but not ["ab"].
  const needlePrefix = needle.slice(0, -1);

  for (const [key, entry] of cache) {
    const matches = key === needle || key.startsWith(needlePrefix + ",");
    if (!matches) continue;

    // Marking stale rather than deleting keeps the current data on screen
    // while the refetch runs — invalidation should not blank the UI.
    entry.updatedAt = 0;
    publish(entry);

    // A query with active listeners is on screen right now. Without this, a
    // mounted query only picks up fresh data on its next mount — the mutation
    // that just wrote the change would succeed while the visible list quietly
    // kept showing the old one.
    if (entry.listeners.size > 0) entry.refetch?.();
  }
}

/** Reads cached data without subscribing. For imperative code paths. */
export function peek<T>(key: QueryKey): T | undefined {
  return cache.get(serialize(key))?.data as T | undefined;
}

/**
 * Writes directly into the cache.
 *
 * Used for optimistic updates and for applying a realtime event's payload
 * without a refetch — when the event carries the whole new row, refetching it
 * is a round trip to learn what we were just told.
 */
export function setQueryData<T>(key: QueryKey, updater: T | ((current: T | undefined) => T)) {
  const serialized = serialize(key);
  const entry = entryFor<T>(serialized);
  entry.data =
    typeof updater === "function" ? (updater as (c: T | undefined) => T)(entry.data) : updater;
  entry.updatedAt = Date.now();
  entry.error = undefined;
  publish(entry);
}

/** Empties the cache. Called on sign-out so the next user sees nothing of the last. */
export function clearQueryCache() {
  for (const entry of cache.values()) {
    entry.data = undefined;
    entry.error = undefined;
    entry.updatedAt = 0;
    publish(entry);
  }
  cache.clear();
}

export type UseMutationOptions<TArgs, TResult> = {
  /**
   * Applies the change locally before the server confirms it, and returns a
   * rollback function. This is what makes sending a message feel instant.
   *
   * Returning a rollback rather than a snapshot keeps the undo logic next to
   * the change it undoes — the alternative, deep-cloning the cache, is both
   * slower and wrong for anything the server has since patched.
   */
  optimistic?: (args: TArgs) => () => void;
  onSuccess?: (result: TResult, args: TArgs) => void;
  onError?: (error: unknown, args: TArgs) => void;
  /** Key prefixes to invalidate once the write succeeds. */
  invalidates?: QueryKey[];
};

export type MutationState<TArgs, TResult> = {
  mutate: (args: TArgs) => Promise<TResult>;
  isPending: boolean;
  error: unknown;
  /**
   * True once a mutation has completed without error. Stays true across
   * re-renders until `reset` is called or `mutate` runs again — the "show a
   * confirmation screen" flag for one-shot actions like requesting a
   * password reset, where §11.4 requires the response to look identical
   * whether or not the account exists, so the component has nothing to
   * branch on except "did the request finish".
   */
  isSuccess: boolean;
  reset: () => void;
};

/** Writes server state, with optimistic update and rollback. */
export function useMutation<TArgs, TResult>(
  mutator: (args: TArgs) => Promise<TResult>,
  options: UseMutationOptions<TArgs, TResult> = {},
): MutationState<TArgs, TResult> {
  const [isPending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(undefined);
  const [isSuccess, setSuccess] = useState(false);

  const optionsRef = useRef(options);
  optionsRef.current = options;
  const mutatorRef = useRef(mutator);
  mutatorRef.current = mutator;

  const mutate = useCallback(async (args: TArgs) => {
    const { optimistic, onSuccess, onError, invalidates } = optionsRef.current;

    setPending(true);
    setError(undefined);
    setSuccess(false);

    const rollback = optimistic?.(args);

    try {
      const result = await mutatorRef.current(args);
      invalidates?.forEach(invalidate);
      setSuccess(true);
      onSuccess?.(result, args);
      return result;
    } catch (caught) {
      // Undo the optimistic change before surfacing the error, so the user
      // never sees a message that appears sent alongside a failure notice.
      rollback?.();
      setError(caught);
      onError?.(caught, args);
      throw caught;
    } finally {
      setPending(false);
    }
  }, []);

  const reset = useCallback(() => {
    setError(undefined);
    setSuccess(false);
  }, []);

  return { mutate, isPending, error, isSuccess, reset };
}

/**
 * Cursor pagination (§16).
 *
 * Pages accumulate rather than replace, because every list in this product is
 * an append-scrolling one — an inbox, a timeline, an audit log.
 */
export function useInfinite<T>(
  key: QueryKey | null,
  fetchPage: (cursor: string | null, signal: AbortSignal) => Promise<Paginated<T>>,
  options: UseQueryOptions = {},
) {
  const [pages, setPages] = useState<Paginated<T>[]>([]);
  const [loadingMore, setLoadingMore] = useState(false);

  const first = useQuery(key, (signal) => fetchPage(null, signal), options);

  useEffect(() => {
    if (first.data) setPages([first.data]);
  }, [first.data]);

  const last = pages[pages.length - 1];
  const hasMore = last?.has_more ?? false;

  const fetchNext = useCallback(async () => {
    if (!hasMore || loadingMore || !last?.next_cursor) return;
    setLoadingMore(true);
    try {
      const controller = new AbortController();
      const page = await fetchPage(last.next_cursor, controller.signal);
      setPages((current) => [...current, page]);
    } finally {
      setLoadingMore(false);
    }
  }, [hasMore, loadingMore, last, fetchPage]);

  return {
    items: pages.flatMap((page) => page.data),
    isLoading: first.isLoading,
    isFetching: first.isFetching || loadingMore,
    error: first.error,
    hasMore,
    fetchNext,
    refetch: first.refetch,
  };
}

/**
 * Loads every cursor page for a bounded lookup collection.
 *
 * This is for selectors and cross-reference filters (members, teams, tags,
 * inboxes), not primary tables: those should keep their explicit Load more
 * control. The helper preserves the same cancellation and error behavior as
 * useInfinite while preventing a paginated endpoint from being mistaken for
 * a single unbounded response by a lookup control.
 */
export function useAllPages<T>(
  key: QueryKey | null,
  fetchPage: (cursor: string | null, signal: AbortSignal) => Promise<Paginated<T>>,
  options: UseQueryOptions = {},
) {
  const page = useInfinite(key, fetchPage, options);
  const [additionalError, setAdditionalError] = useState<unknown>(null);
  const pageFetchNext = page.fetchNext;

  useEffect(() => {
    if (page.isFetching && !page.error) setAdditionalError(null);
  }, [page.error, page.isFetching]);

  const fetchNext = useCallback(async () => {
    try {
      await pageFetchNext();
    } catch (error) {
      setAdditionalError(error);
    }
  }, [pageFetchNext]);

  useEffect(() => {
    if (page.error || additionalError || !page.hasMore || page.isFetching) return;
    void fetchNext();
  }, [additionalError, fetchNext, page.error, page.hasMore, page.isFetching]);

  return {
    ...page,
    error: page.error ?? additionalError,
    isError: Boolean(page.error ?? additionalError),
  };
}

/** Re-exported so callers can narrow on it without importing the client too. */
export { ApiError };
