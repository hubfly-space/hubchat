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
  return new Intl.NumberFormat(opts.locale ?? DEFAULT_LOCALE).format(value);
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
