import * as RadixSelect from "@radix-ui/react-select";
import { Check, ChevronDown, ChevronUp } from "lucide-react";
import { forwardRef, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { inputShell } from "./Input";

export type SelectOption<T extends string = string> = {
  value: T;
  label: string;
  description?: string;
  icon?: ReactNode;
  disabled?: boolean;
  /** Options sharing a group render under one sticky heading. */
  group?: string;
};

export type SelectProps<T extends string = string> = {
  value?: T;
  defaultValue?: T;
  onValueChange?: (value: T) => void;
  options: SelectOption<T>[];
  placeholder?: string;
  size?: "sm" | "md" | "lg";
  disabled?: boolean;
  invalid?: boolean;
  id?: string;
  name?: string;
  className?: string;
  /** Matches the trigger width to the widest option instead of the container. */
  contentWidth?: "trigger" | "auto";
  "aria-label"?: string;
};

const TRIGGER_SIZES = {
  sm: "h-control-sm text-xs rounded-sm px-2",
  md: "h-control-md text-sm rounded-md px-2.5",
  lg: "h-control-lg text-base rounded-md px-3",
} as const;

/**
 * Native <select> loses on two counts we care about: it cannot show a
 * description line per option, and it cannot be styled consistently across
 * platforms in a dense dark UI. Radix gives us both plus typeahead for free.
 */
export function Select<T extends string = string>({
  value,
  defaultValue,
  onValueChange,
  options,
  placeholder = "Select…",
  size = "md",
  disabled,
  invalid,
  id,
  name,
  className,
  contentWidth = "trigger",
  ...aria
}: SelectProps<T>) {
  const groups = groupOptions(options);

  return (
    <RadixSelect.Root
      value={value}
      defaultValue={defaultValue}
      onValueChange={onValueChange as (v: string) => void}
      disabled={disabled}
      name={name}
    >
      <RadixSelect.Trigger
        id={id}
        aria-invalid={invalid || undefined}
        aria-label={aria["aria-label"]}
        className={cn(
          inputShell,
          TRIGGER_SIZES[size],
          "flex items-center justify-between gap-2 text-left",
          "data-[placeholder]:text-fg-disabled",
          "[&>span]:truncate",
          className,
        )}
      >
        <RadixSelect.Value placeholder={placeholder} />
        <RadixSelect.Icon asChild>
          <ChevronDown aria-hidden="true" className="size-3.5 shrink-0 text-fg-muted" />
        </RadixSelect.Icon>
      </RadixSelect.Trigger>

      <RadixSelect.Portal>
        <RadixSelect.Content
          position="popper"
          sideOffset={4}
          className={cn(
            "z-[var(--z-dropdown)] overflow-hidden rounded-lg border border-line",
            "bg-overlay shadow-3 inset-shadow-highlight",
            "max-h-[min(24rem,var(--radix-select-content-available-height))]",
            "data-[state=open]:animate-zoom-in",
            contentWidth === "trigger" && "w-[var(--radix-select-trigger-width)]",
            contentWidth === "auto" && "min-w-[var(--radix-select-trigger-width)]",
          )}
        >
          <RadixSelect.ScrollUpButton className="flex h-5 items-center justify-center text-fg-muted">
            <ChevronUp className="size-3" />
          </RadixSelect.ScrollUpButton>

          <RadixSelect.Viewport className="p-1">
            {groups.map(({ label, items }, index) => (
              <RadixSelect.Group key={label ?? index}>
                {label && (
                  <RadixSelect.Label className="px-2 pb-1 pt-2 text-2xs font-semibold uppercase tracking-caps text-fg-muted">
                    {label}
                  </RadixSelect.Label>
                )}
                {items.map((option) => (
                  <SelectItem key={option.value} option={option} />
                ))}
              </RadixSelect.Group>
            ))}
          </RadixSelect.Viewport>

          <RadixSelect.ScrollDownButton className="flex h-5 items-center justify-center text-fg-muted">
            <ChevronDown className="size-3" />
          </RadixSelect.ScrollDownButton>
        </RadixSelect.Content>
      </RadixSelect.Portal>
    </RadixSelect.Root>
  );
}

const SelectItem = forwardRef<HTMLDivElement, { option: SelectOption<string> }>(
  function SelectItem({ option }, ref) {
    return (
      <RadixSelect.Item
        ref={ref}
        value={option.value}
        disabled={option.disabled}
        className={cn(
          "relative flex cursor-default select-none items-start gap-2 rounded-sm py-1.5 pl-2 pr-7 text-sm outline-none",
          "text-fg-secondary",
          "data-[highlighted]:bg-fill data-[highlighted]:text-fg",
          "data-[state=checked]:text-fg",
          "data-[disabled]:pointer-events-none data-[disabled]:text-fg-disabled",
        )}
      >
        {option.icon && (
          <span className="mt-0.5 shrink-0 text-fg-muted [&_svg]:size-3.5">{option.icon}</span>
        )}
        <span className="min-w-0 flex-1">
          <RadixSelect.ItemText>{option.label}</RadixSelect.ItemText>
          {option.description && (
            <span className="mt-0.5 block text-xs leading-snug text-fg-muted">
              {option.description}
            </span>
          )}
        </span>
        <RadixSelect.ItemIndicator className="absolute right-2 top-2">
          <Check aria-hidden="true" className="size-3.5 text-accent-text" />
        </RadixSelect.ItemIndicator>
      </RadixSelect.Item>
    );
  },
);

function groupOptions<T extends string>(options: SelectOption<T>[]) {
  const result: { label: string | null; items: SelectOption<T>[] }[] = [];
  for (const option of options) {
    const key = option.group ?? null;
    const existing = result.find((group) => group.label === key);
    if (existing) existing.items.push(option);
    else result.push({ label: key, items: [option] });
  }
  return result;
}
