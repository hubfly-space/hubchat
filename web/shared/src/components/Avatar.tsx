import * as RadixAvatar from "@radix-ui/react-avatar";
import { Bot, Building2, User } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../lib/cn";
import { hashToIndex, initials } from "../lib/format";
import { StatusDot } from "./Badge";

export type AvatarSize = "2xs" | "xs" | "sm" | "md" | "lg" | "xl";

const SIZES: Record<AvatarSize, string> = {
  "2xs": "size-4 text-[8px]",
  xs: "size-5 text-2xs",
  sm: "size-6 text-2xs",
  md: "size-8 text-xs",
  lg: "size-10 text-sm",
  xl: "size-16 text-lg",
};

/**
 * Fallback tints are drawn from the chart palette rather than a bespoke avatar
 * palette — it keeps the total hue count in the product fixed, and the same
 * customer resolves to the same tint on every agent's screen because the index
 * is hashed from a stable id.
 */
const TINTS = [1, 2, 3, 4, 5, 6] as const;

export type AvatarProps = {
  name?: string | null;
  src?: string | null;
  /** Stable identity for tint selection. Falls back to `name`. */
  seed?: string;
  size?: AvatarSize;
  /** Companies get a square-ish tile so they never read as a person. */
  shape?: "circle" | "square";
  kind?: "person" | "company" | "bot";
  status?: "online" | "away" | "busy" | "offline" | "live" | null;
  pulse?: boolean;
  className?: string;
};

export function Avatar({
  name,
  src,
  seed,
  size = "md",
  shape = "circle",
  kind = "person",
  status,
  pulse,
  className,
}: AvatarProps) {
  const identity = seed ?? name ?? "";
  const tint = TINTS[hashToIndex(identity, TINTS.length)] ?? 3;
  const FallbackIcon = kind === "company" ? Building2 : kind === "bot" ? Bot : User;
  const label = name ?? "Unknown";

  return (
    <span className={cn("relative inline-flex shrink-0", className)}>
      <RadixAvatar.Root
        className={cn(
          "inline-flex select-none items-center justify-center overflow-hidden",
          "border border-line-subtle bg-fill font-semibold",
          shape === "circle" ? "rounded-full" : "rounded-md",
          SIZES[size],
        )}
      >
        {src && (
          <RadixAvatar.Image
            src={src}
            alt={label}
            className="size-full object-cover"
          />
        )}
        <RadixAvatar.Fallback
          delayMs={src ? 200 : 0}
          className="flex size-full items-center justify-center"
          style={{
            backgroundColor: `color-mix(in oklab, var(--hc-chart-${tint}) 22%, transparent)`,
            color: `var(--hc-chart-${tint})`,
          }}
        >
          {name ? (
            initials(name)
          ) : (
            <FallbackIcon aria-hidden="true" className="size-1/2 opacity-70" />
          )}
        </RadixAvatar.Fallback>
      </RadixAvatar.Root>

      {status && (
        <StatusDot
          status={status}
          size={size === "lg" || size === "xl" ? "md" : "sm"}
          pulse={pulse}
          className={cn(
            "absolute -bottom-px -right-px ring-2 ring-surface",
            shape === "square" && "-bottom-0.5 -right-0.5",
          )}
        />
      )}
    </span>
  );
}

/**
 * Overlapping stack for participants, viewers, and team rosters.
 * Overflow collapses into a "+N" chip rather than growing unbounded.
 */
export function AvatarGroup({
  people,
  max = 4,
  size = "sm",
  className,
}: {
  people: { id: string; name?: string | null; src?: string | null }[];
  max?: number;
  size?: AvatarSize;
  className?: string;
}) {
  const shown = people.slice(0, max);
  const overflow = people.length - shown.length;

  return (
    <span className={cn("flex items-center", className)}>
      {shown.map((person) => (
        <Avatar
          key={person.id}
          name={person.name}
          src={person.src}
          seed={person.id}
          size={size}
          className="-ml-1.5 ring-2 ring-surface first:ml-0 [&>*]:rounded-full"
        />
      ))}
      {overflow > 0 && (
        <span
          className={cn(
            "-ml-1.5 inline-flex items-center justify-center rounded-full",
            "border border-line-subtle bg-fill font-semibold text-fg-muted ring-2 ring-surface",
            SIZES[size],
          )}
        >
          +{overflow}
        </span>
      )}
    </span>
  );
}

/** Avatar plus name/secondary line — the standard identity cell in tables. */
export function IdentityCell({
  name,
  secondary,
  src,
  seed,
  size = "sm",
  kind,
  status,
  trailing,
  className,
}: AvatarProps & { secondary?: ReactNode; trailing?: ReactNode }) {
  return (
    <span className={cn("flex min-w-0 items-center gap-2", className)}>
      <Avatar name={name} src={src} seed={seed} size={size} kind={kind} status={status} />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm text-fg">{name ?? "Anonymous visitor"}</span>
        {secondary && (
          <span className="block truncate text-xs text-fg-muted">{secondary}</span>
        )}
      </span>
      {trailing}
    </span>
  );
}
