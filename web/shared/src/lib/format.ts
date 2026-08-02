/**
 * Formatting helpers shared by every surface.
 *
 * Locale and timezone are always explicit parameters with workspace-level
 * defaults supplied by the caller (§6.1 workspace settings). Nothing here reads
 * the browser's timezone implicitly — an agent in Lisbon and an agent in Lagos
 * must see identical timestamps for the same workspace.
 */

export type FormatOptions = {
  locale?: string;
  timeZone?: string;
  minimumFractionDigits?: number;
  maximumFractionDigits?: number;
};

const DEFAULT_LOCALE = "en";

/** Absolute timestamp, e.g. "12 Mar 2026, 14:08". */
export function formatDateTime(value: Date | string | number, opts: FormatOptions = {}): string {
  return new Intl.DateTimeFormat(opts.locale ?? DEFAULT_LOCALE, {
    day: "numeric",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
    timeZone: opts.timeZone,
  }).format(toDate(value));
}

/**
 * Value for a `datetime-local` input in an explicit IANA timezone.
 *
 * The input has no timezone information, so using Date#getHours() here would
 * accidentally make scheduling depend on the agent's browser. Keep this
 * conversion next to the other explicit formatting helpers instead.
 */
export function formatDateTimeLocal(value: Date | string | number, timeZone = "UTC"): string {
  const parts = zonedDateParts(toDate(value), timeZone);
  return `${parts.year}-${parts.month}-${parts.day}T${parts.hour}:${parts.minute}`;
}

/**
 * Convert a `datetime-local` wall-clock value in an IANA timezone to UTC.
 * Returns null for malformed values and for wall-clock times skipped by a DST
 * transition. Ambiguous fall-back times resolve deterministically to the
 * offset selected by the runtime's timezone data.
 */
export function parseDateTimeLocal(value: string, timeZone = "UTC"): string | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(value);
  if (!match) return null;

  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const hour = Number(match[4]);
  const minute = Number(match[5]);
  const wallClock = Date.UTC(year, month - 1, day, hour, minute);
  if (
    !Number.isFinite(wallClock) ||
    new Date(wallClock).getUTCFullYear() !== year ||
    new Date(wallClock).getUTCMonth() !== month - 1 ||
    new Date(wallClock).getUTCDate() !== day ||
    hour > 23 ||
    minute > 59
  ) {
    return null;
  }

  let candidate = wallClock;
  for (let attempt = 0; attempt < 4; attempt += 1) {
    const offset = timezoneOffset(new Date(candidate), timeZone);
    const next = wallClock - offset;
    if (next === candidate) break;
    candidate = next;
  }

  const instant = new Date(candidate);
  return formatDateTimeLocal(instant, timeZone) === value ? instant.toISOString() : null;
}

/** Date only, e.g. "12 Mar 2026". */
export function formatDate(value: Date | string | number, opts: FormatOptions = {}): string {
  return new Intl.DateTimeFormat(opts.locale ?? DEFAULT_LOCALE, {
    day: "numeric",
    month: "short",
    year: "numeric",
    timeZone: opts.timeZone,
  }).format(toDate(value));
}

/** Time only, e.g. "14:08". */
export function formatTime(value: Date | string | number, opts: FormatOptions = {}): string {
  return new Intl.DateTimeFormat(opts.locale ?? DEFAULT_LOCALE, {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
    timeZone: opts.timeZone,
  }).format(toDate(value));
}

/**
 * Compact relative time for dense lists: "now", "4m", "3h", "2d", "6w".
 * Deliberately terse — a conversation row has ~40px for this.
 */
export function formatRelativeShort(value: Date | string | number, now: Date = new Date()): string {
  const diff = Math.floor((now.getTime() - toDate(value).getTime()) / 1000);
  if (diff < 45) return "now";
  if (diff < 3600) return `${Math.floor(diff / 60)}m`;
  if (diff < 86_400) return `${Math.floor(diff / 3600)}h`;
  if (diff < 604_800) return `${Math.floor(diff / 86_400)}d`;
  if (diff < 2_592_000) return `${Math.floor(diff / 604_800)}w`;
  if (diff < 31_536_000) return `${Math.floor(diff / 2_592_000)}mo`;
  return `${Math.floor(diff / 31_536_000)}y`;
}

/** Prose relative time for timelines and detail views: "4 minutes ago". */
export function formatRelativeLong(
  value: Date | string | number,
  now: Date = new Date(),
  opts: FormatOptions = {},
): string {
  const rtf = new Intl.RelativeTimeFormat(opts.locale ?? DEFAULT_LOCALE, { numeric: "auto" });
  const seconds = Math.round((toDate(value).getTime() - now.getTime()) / 1000);
  const abs = Math.abs(seconds);

  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ["year", 31_536_000],
    ["month", 2_592_000],
    ["week", 604_800],
    ["day", 86_400],
    ["hour", 3600],
    ["minute", 60],
  ];

  for (const [unit, secondsPerUnit] of units) {
    if (abs >= secondsPerUnit) return rtf.format(Math.round(seconds / secondsPerUnit), unit);
  }
  return rtf.format(seconds, "second");
}

/**
 * Duration for SLA timers and handling times: "2h 14m", "48s", "3d 4h".
 * Always two units at most — an agent glancing at a countdown does not need
 * seconds of precision on a four-hour target.
 */
export function formatDuration(seconds: number): string {
  const sign = seconds < 0 ? "-" : "";
  let remaining = Math.abs(Math.floor(seconds));

  if (remaining < 60) return `${sign}${remaining}s`;

  const days = Math.floor(remaining / 86_400);
  remaining %= 86_400;
  const hours = Math.floor(remaining / 3600);
  remaining %= 3600;
  const minutes = Math.floor(remaining / 60);

  if (days > 0) return `${sign}${days}d${hours ? ` ${hours}h` : ""}`;
  if (hours > 0) return `${sign}${hours}h${minutes ? ` ${minutes}m` : ""}`;
  return `${sign}${minutes}m`;
}

/** Thousands separators with tabular figures assumed at the render site. */
export function formatNumber(value: number, opts: FormatOptions = {}): string {
  return new Intl.NumberFormat(opts.locale ?? DEFAULT_LOCALE, {
    minimumFractionDigits: opts.minimumFractionDigits,
    maximumFractionDigits: opts.maximumFractionDigits,
  }).format(value);
}

/** Compact counts for badges and metrics: 1_284 → "1.3k". */
export function formatCompact(value: number, opts: FormatOptions = {}): string {
  return new Intl.NumberFormat(opts.locale ?? DEFAULT_LOCALE, {
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(value);
}

/** Percentage from a 0–1 ratio. */
export function formatPercent(ratio: number, fractionDigits = 0, opts: FormatOptions = {}): string {
  return new Intl.NumberFormat(opts.locale ?? DEFAULT_LOCALE, {
    style: "percent",
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits,
  }).format(ratio);
}

/** Human file size for attachments and storage limits. */
export function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / Math.pow(1024, exponent);
  return `${value.toFixed(exponent === 0 ? 0 : value >= 10 ? 0 : 1)} ${units[exponent]}`;
}

/**
 * Initials for avatar fallbacks. Takes first + last token so
 * "Amara Chidinma Okeke" → "AO", and falls back to the local part of an email.
 */
export function initials(name: string | null | undefined, fallback = "?"): string {
  const source = (name ?? "").trim();
  if (!source) return fallback;

  const cleaned = source.includes("@") ? (source.split("@")[0] ?? source) : source;
  const tokens = cleaned.split(/[\s._-]+/).filter(Boolean);
  if (tokens.length === 0) return fallback;
  if (tokens.length === 1) return (tokens[0] ?? "").slice(0, 2).toUpperCase();

  const first = tokens[0]?.[0] ?? "";
  const last = tokens[tokens.length - 1]?.[0] ?? "";
  return `${first}${last}`.toUpperCase();
}

/** Truncate on a word boundary where possible; used in list previews. */
export function truncate(text: string, max: number): string {
  if (text.length <= max) return text;
  const slice = text.slice(0, max);
  const lastSpace = slice.lastIndexOf(" ");
  return `${(lastSpace > max * 0.6 ? slice.slice(0, lastSpace) : slice).trimEnd()}…`;
}

/** Collapse whitespace and strip newlines for single-line previews. */
export function toPreview(text: string, max = 120): string {
  return truncate(text.replace(/\s+/g, " ").trim(), max);
}

/**
 * Deterministic index into a fixed palette, so the same customer always gets
 * the same avatar tint across sessions and across agents' screens.
 */
export function hashToIndex(seed: string, buckets: number): number {
  let hash = 0;
  for (let i = 0; i < seed.length; i++) {
    hash = (hash << 5) - hash + seed.charCodeAt(i);
    hash |= 0;
  }
  return Math.abs(hash) % buckets;
}

/** "SUP-1042" from a workspace prefix and display number. */
export function ticketNumber(prefix: string, number: number): string {
  return `${prefix}-${number}`;
}

/** Mask a secret for display, preserving a recognisable head and tail. */
export function maskSecret(secret: string, visible = 4): string {
  if (secret.length <= visible * 2) return "•".repeat(secret.length);
  return `${secret.slice(0, visible)}${"•".repeat(8)}${secret.slice(-visible)}`;
}

function toDate(value: Date | string | number): Date {
  return value instanceof Date ? value : new Date(value);
}

type ZonedDateParts = { year: string; month: string; day: string; hour: string; minute: string };

function zonedDateParts(value: Date, timeZone: string): ZonedDateParts {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone,
    calendar: "gregory",
    numberingSystem: "latn",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  }).formatToParts(value);
  const get = (type: string) => parts.find((part) => part.type === type)?.value ?? "";
  return { year: get("year"), month: get("month"), day: get("day"), hour: get("hour"), minute: get("minute") };
}

function timezoneOffset(value: Date, timeZone: string): number {
  const parts = zonedDateParts(value, timeZone);
  return Date.UTC(Number(parts.year), Number(parts.month) - 1, Number(parts.day), Number(parts.hour), Number(parts.minute)) - value.getTime();
}
