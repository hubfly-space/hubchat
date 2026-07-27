import * as RadixHoverCard from "@radix-ui/react-hover-card";
import * as RadixPopover from "@radix-ui/react-popover";
import type { ReactNode } from "react";
import { cn } from "../lib/cn";
import { overlaySurface } from "./Menu";

export const Popover = RadixPopover.Root;
export const PopoverTrigger = RadixPopover.Trigger;
export const PopoverClose = RadixPopover.Close;
export const PopoverAnchor = RadixPopover.Anchor;

export function PopoverContent({
  children,
  className,
  align = "start",
  sideOffset = 6,
  ...props
}: React.ComponentPropsWithoutRef<typeof RadixPopover.Content>) {
  return (
    <RadixPopover.Portal>
      <RadixPopover.Content
        align={align}
        sideOffset={sideOffset}
        collisionPadding={8}
        className={cn(
          overlaySurface,
          "w-72 max-w-[calc(100vw-2rem)] outline-none",
          "max-h-[min(30rem,var(--radix-popover-content-available-height))] overflow-y-auto",
          className,
        )}
        {...props}
      >
        {children}
      </RadixPopover.Content>
    </RadixPopover.Portal>
  );
}

/**
 * HoverCard is for previewing an entity without navigating: a customer behind a
 * name, an article behind a link, a rule behind an execution log row. It must
 * never contain the only path to an action — hover is not an affordance on
 * touch devices.
 */
export function HoverCard({
  trigger,
  children,
  side = "right",
  align = "start",
  openDelay = 400,
  className,
}: {
  trigger: ReactNode;
  children: ReactNode;
  side?: "top" | "right" | "bottom" | "left";
  align?: "start" | "center" | "end";
  openDelay?: number;
  className?: string;
}) {
  return (
    <RadixHoverCard.Root openDelay={openDelay} closeDelay={120}>
      <RadixHoverCard.Trigger asChild>{trigger}</RadixHoverCard.Trigger>
      <RadixHoverCard.Portal>
        <RadixHoverCard.Content
          side={side}
          align={align}
          sideOffset={8}
          collisionPadding={8}
          className={cn(overlaySurface, "w-72 p-3", className)}
        >
          {children}
        </RadixHoverCard.Content>
      </RadixHoverCard.Portal>
    </RadixHoverCard.Root>
  );
}
