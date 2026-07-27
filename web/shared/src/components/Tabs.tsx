import * as RadixTabs from "@radix-ui/react-tabs";
import * as RadixToggleGroup from "@radix-ui/react-toggle-group";
import type { ReactNode } from "react";
import { cn } from "../lib/cn";
import { CountBadge } from "./Badge";

export const Tabs = RadixTabs.Root;
export const TabsContent = RadixTabs.Content;

export type TabItem = {
  value: string;
  label: ReactNode;
  icon?: ReactNode;
  count?: number;
  disabled?: boolean;
};

export type TabsListProps = {
  items: TabItem[];
  /**
   * underline — page-level sections. Sits on a rule that spans the container.
   * pills     — filter-like switching inside a panel.
   */
  variant?: "underline" | "pills";
  className?: string;
};

export function TabsList({ items, variant = "underline", className }: TabsListProps) {
  if (variant === "pills") {
    return (
      <RadixTabs.List
        className={cn(
          "hc-no-scrollbar inline-flex items-center gap-0.5 overflow-x-auto rounded-md border border-line bg-inset p-0.5",
          className,
        )}
      >
        {items.map((item) => (
          <RadixTabs.Trigger
            key={item.value}
            value={item.value}
            disabled={item.disabled}
            className={cn(
              "inline-flex h-6 shrink-0 items-center gap-1.5 rounded-sm px-2.5 text-xs font-medium",
              "text-fg-muted transition-colors duration-fast",
              "hover:text-fg-secondary",
              "data-[state=active]:bg-raised data-[state=active]:text-fg data-[state=active]:shadow-1",
              "disabled:pointer-events-none disabled:text-fg-disabled",
              "[&_svg]:size-3.5",
            )}
          >
            {item.icon}
            {item.label}
            {item.count != null && <CountBadge count={item.count} />}
          </RadixTabs.Trigger>
        ))}
      </RadixTabs.List>
    );
  }

  return (
    <RadixTabs.List
      className={cn(
        "hc-no-scrollbar flex items-center gap-4 overflow-x-auto border-b border-line",
        className,
      )}
    >
      {items.map((item) => (
        <RadixTabs.Trigger
          key={item.value}
          value={item.value}
          disabled={item.disabled}
          className={cn(
            "group relative inline-flex shrink-0 items-center gap-1.5 pb-2.5 pt-1 text-sm",
            "text-fg-muted transition-colors duration-fast",
            "hover:text-fg-secondary",
            "data-[state=active]:font-medium data-[state=active]:text-fg",
            "disabled:pointer-events-none disabled:text-fg-disabled",
            "[&_svg]:size-3.5",
            // The indicator is a pseudo-element rather than a sliding div so it
            // survives horizontal scroll and never needs measurement.
            "after:absolute after:inset-x-0 after:-bottom-px after:h-0.5 after:rounded-full",
            "after:bg-accent after:opacity-0 after:transition-opacity",
            "data-[state=active]:after:opacity-100",
          )}
        >
          {item.icon}
          {item.label}
          {item.count != null && <CountBadge count={item.count} />}
        </RadixTabs.Trigger>
      ))}
    </RadixTabs.List>
  );
}

export type SegmentedOption<T extends string = string> = {
  value: T;
  label?: ReactNode;
  icon?: ReactNode;
  ariaLabel?: string;
};

export type SegmentedControlProps<T extends string = string> = {
  value: T;
  onValueChange: (value: T) => void;
  options: SegmentedOption<T>[];
  size?: "sm" | "md";
  className?: string;
  "aria-label": string;
};

/**
 * A committed either/or: list density, chart granularity, light/dark preview.
 * Unlike Tabs it does not swap a panel — it changes how the current panel
 * renders — and unlike a Select it exposes every option at once.
 */
export function SegmentedControl<T extends string = string>({
  value,
  onValueChange,
  options,
  size = "sm",
  className,
  ...aria
}: SegmentedControlProps<T>) {
  return (
    <RadixToggleGroup.Root
      type="single"
      value={value}
      onValueChange={(next) => next && onValueChange(next as T)}
      aria-label={aria["aria-label"]}
      className={cn(
        "inline-flex shrink-0 items-center gap-0.5 rounded-md border border-line bg-inset p-0.5",
        className,
      )}
    >
      {options.map((option) => (
        <RadixToggleGroup.Item
          key={option.value}
          value={option.value}
          aria-label={option.ariaLabel}
          className={cn(
            "inline-flex items-center justify-center gap-1.5 rounded-sm font-medium",
            "text-fg-muted transition-colors duration-fast",
            "hover:text-fg-secondary",
            "data-[state=on]:bg-raised data-[state=on]:text-fg data-[state=on]:shadow-1",
            size === "sm" ? "h-6 px-2 text-xs" : "h-7 px-2.5 text-sm",
            !option.label && (size === "sm" ? "w-6 px-0" : "w-7 px-0"),
            "[&_svg]:size-3.5",
          )}
        >
          {option.icon}
          {option.label}
        </RadixToggleGroup.Item>
      ))}
    </RadixToggleGroup.Root>
  );
}
