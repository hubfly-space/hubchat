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

type CalendarParts = { year: number; month: number; day: number; hour: number; minute: number; second: number };

/** Loads every cursor page for a bounded report window. */
export function useAnalyticsRollups(key: QueryKey | null, metric: string, from?: string, to?: string, timezone = "UTC") {
  const fetchPage = useCallback((cursor: string | null, signal: AbortSignal) => {
    const params = new URLSearchParams({ metric, grain: "day", limit: "200", timezone });
    if (from) params.set("from", from);
    if (to) params.set("to", to);
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<AnalyticsRollup>>(`/analytics/rollups?${params.toString()}`, { signal });
  }, [from, metric, timezone, to]);

  return useAllPages(key, fetchPage);
}

/**
 * Builds the selected rolling window in the workspace timezone. Subtracting
 * calendar days before converting to UTC keeps DST transitions from shifting
 * the report boundary by an hour.
 */
export function reportWindow(range: string, timezone = "UTC") {
  const days = range === "7d" ? 7 : range === "90d" ? 90 : 30;
  const now = new Date();
  const effectiveTimezone = validTimezone(timezone) ? timezone : "UTC";
  const local = calendarParts(now, effectiveTimezone);
  const targetDate = new Date(Date.UTC(local.year, local.month - 1, local.day, local.hour, local.minute, local.second));
  targetDate.setUTCDate(targetDate.getUTCDate() - days);
  const from = fromCalendarParts({
    year: targetDate.getUTCFullYear(),
    month: targetDate.getUTCMonth() + 1,
    day: targetDate.getUTCDate(),
    hour: local.hour,
    minute: local.minute,
    second: local.second,
  }, effectiveTimezone);
  return { from: from.toISOString(), to: now.toISOString(), timezone: effectiveTimezone };
}

function validTimezone(timezone: string) {
  try {
    new Intl.DateTimeFormat("en-US", { timeZone: timezone }).format();
    return true;
  } catch {
    return false;
  }
}

function calendarParts(date: Date, timezone: string): CalendarParts {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone: timezone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hourCycle: "h23",
  }).formatToParts(date);
  const value = (type: Intl.DateTimeFormatPartTypes) => Number(parts.find((part) => part.type === type)?.value ?? 0);
  return { year: value("year"), month: value("month"), day: value("day"), hour: value("hour"), minute: value("minute"), second: value("second") };
}

function fromCalendarParts(desired: CalendarParts, timezone: string) {
  let guess = Date.UTC(desired.year, desired.month - 1, desired.day, desired.hour, desired.minute, desired.second);
  for (let attempt = 0; attempt < 3; attempt += 1) {
    const actual = calendarParts(new Date(guess), timezone);
    const desiredAsUTC = Date.UTC(desired.year, desired.month - 1, desired.day, desired.hour, desired.minute, desired.second);
    const actualAsUTC = Date.UTC(actual.year, actual.month - 1, actual.day, actual.hour, actual.minute, actual.second);
    guess += desiredAsUTC - actualAsUTC;
  }
  return new Date(guess);
}
