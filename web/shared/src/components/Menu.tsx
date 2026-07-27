import * as RadixMenu from "@radix-ui/react-dropdown-menu";
import { Check, ChevronRight } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../lib/cn";
import { Kbd } from "./Kbd";

export const Menu = RadixMenu.Root;
export const MenuTrigger = RadixMenu.Trigger;
export const MenuGroup = RadixMenu.Group;
export const MenuRadioGroup = RadixMenu.RadioGroup;

/** Shared surface treatment for every floating panel: menu, popover, combobox. */
export const overlaySurface = cn(
  "rounded-lg border border-line-strong bg-overlay shadow-3 inset-shadow-highlight",
  "data-[state=open]:animate-zoom-in",
  "z-[var(--z-dropdown)]",
);

export function MenuContent({
  children,
  align = "start",
  side = "bottom",
  sideOffset = 4,
  className,
  ...props
}: React.ComponentPropsWithoutRef<typeof RadixMenu.Content>) {
  return (
    <RadixMenu.Portal>
      <RadixMenu.Content
        align={align}
        side={side}
        sideOffset={sideOffset}
        collisionPadding={8}
        className={cn(
          overlaySurface,
          "min-w-48 max-w-72 overflow-y-auto p-1",
          "max-h-[min(28rem,var(--radix-dropdown-menu-content-available-height))]",
          className,
        )}
        {...props}
      >
        {children}
      </RadixMenu.Content>
    </RadixMenu.Portal>
  );
}

const itemBase = cn(
  "relative flex cursor-default select-none items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-none",
  "text-fg-secondary transition-colors duration-fast",
  "data-[highlighted]:bg-fill data-[highlighted]:text-fg",
  "data-[disabled]:pointer-events-none data-[disabled]:text-fg-disabled",
  "[&_svg]:size-3.5 [&_svg]:shrink-0 [&_svg]:text-fg-muted",
  "data-[highlighted]:[&_svg]:text-fg-secondary",
);

export type MenuItemProps = React.ComponentPropsWithoutRef<typeof RadixMenu.Item> & {
  icon?: ReactNode;
  shortcut?: string;
  destructive?: boolean;
  /** Secondary line under the label. Use sparingly — menus should stay scannable. */
  description?: string;
};

export function MenuItem({
  icon,
  shortcut,
  destructive,
  description,
  children,
  className,
  ...props
}: MenuItemProps) {
  return (
    <RadixMenu.Item
      className={cn(
        itemBase,
        destructive &&
          "text-danger-text data-[highlighted]:bg-danger-subtle data-[highlighted]:text-danger-text [&_svg]:text-danger-text",
        description && "items-start py-2",
        className,
      )}
      {...props}
    >
      {icon && <span className={cn("shrink-0", description && "mt-0.5")}>{icon}</span>}
      <span className="min-w-0 flex-1">
        <span className="block truncate">{children}</span>
        {description && (
          <span className="mt-0.5 block text-xs leading-snug text-fg-muted">{description}</span>
        )}
      </span>
      {shortcut && <Kbd keys={shortcut} className="ml-auto shrink-0" />}
    </RadixMenu.Item>
  );
}

export function MenuCheckboxItem({
  children,
  className,
  ...props
}: React.ComponentPropsWithoutRef<typeof RadixMenu.CheckboxItem>) {
  return (
    <RadixMenu.CheckboxItem className={cn(itemBase, "pl-7", className)} {...props}>
      <RadixMenu.ItemIndicator className="absolute left-2">
        <Check aria-hidden="true" className="size-3.5 text-accent-text" />
      </RadixMenu.ItemIndicator>
      {children}
    </RadixMenu.CheckboxItem>
  );
}

export function MenuRadioItem({
  children,
  className,
  ...props
}: React.ComponentPropsWithoutRef<typeof RadixMenu.RadioItem>) {
  return (
    <RadixMenu.RadioItem className={cn(itemBase, "pl-7", className)} {...props}>
      <RadixMenu.ItemIndicator className="absolute left-2.5">
        <span className="block size-1.5 rounded-full bg-accent" />
      </RadixMenu.ItemIndicator>
      {children}
    </RadixMenu.RadioItem>
  );
}

export function MenuSub({
  label,
  icon,
  children,
}: {
  label: ReactNode;
  icon?: ReactNode;
  children: ReactNode;
}) {
  return (
    <RadixMenu.Sub>
      <RadixMenu.SubTrigger className={cn(itemBase, "data-[state=open]:bg-fill")}>
        {icon}
        <span className="flex-1 truncate">{label}</span>
        <ChevronRight aria-hidden="true" className="ml-auto" />
      </RadixMenu.SubTrigger>
      <RadixMenu.Portal>
        <RadixMenu.SubContent
          sideOffset={2}
          alignOffset={-4}
          collisionPadding={8}
          className={cn(overlaySurface, "min-w-44 max-w-72 p-1")}
        >
          {children}
        </RadixMenu.SubContent>
      </RadixMenu.Portal>
    </RadixMenu.Sub>
  );
}

export function MenuSeparator({ className }: { className?: string }) {
  return <RadixMenu.Separator className={cn("-mx-1 my-1 h-px bg-line", className)} />;
}

export function MenuLabel({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <RadixMenu.Label
      className={cn(
        "px-2 pb-1 pt-2 text-2xs font-semibold uppercase tracking-caps text-fg-muted",
        className,
      )}
    >
      {children}
    </RadixMenu.Label>
  );
}
