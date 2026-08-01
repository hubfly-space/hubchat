import { api, useAllPages, type Paginated, type QueryKey } from "@hubchat/shared";
import { useCallback } from "react";

export type AnalyticsRollup = {
  metric: string;
  grain: string;
  bucket: string;
  dimensions: Record<string, unknown>;
  value: number;
  count: number;
  computed_at: string;
};

/** Loads every cursor page for a bounded report window. */
export function useAnalyticsRollups(key: QueryKey | null, metric: string, from?: string, to?: string) {
  const fetchPage = useCallback((cursor: string | null, signal: AbortSignal) => {
    const params = new URLSearchParams({ metric, grain: "day", limit: "200" });
    if (from) params.set("from", from);
    if (to) params.set("to", to);
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<AnalyticsRollup>>(`/analytics/rollups?${params.toString()}`, { signal });
  }, [from, metric, to]);

  return useAllPages(key, fetchPage);
}
