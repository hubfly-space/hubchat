import * as RadixTooltip from "@radix-ui/react-tooltip";
import { cloneElement, isValidElement, type ReactElement, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { Kbd } from "./Kbd";

export const TooltipProvider = RadixTooltip.Provider;

export type TooltipProps = {
  content: ReactNode;
  /** Rendered on the trailing edge — the shortcut that triggers this action. */
  shortcut?: string;
  side?: "top" | "right" | "bottom" | "left";
  align?: "start" | "center" | "end";
  delay?: number;
  /** Disables the tooltip without unmounting the trigger. */
  disabled?: boolean;
  children: ReactNode;
};

/**
 * Tooltips name things; they never explain them. If the content needs more than
 * a short phrase plus an optional shortcut, it belongs in a HoverCard, a
 * Callout, or the field description.
 */
export function Tooltip({
  content,
  shortcut,
  side = "top",
  align = "center",
  delay = 300,
  disabled,
  children,
}: TooltipProps) {
  if (disabled || !content) return <>{children}</>;

  return (
    <RadixTooltip.Root delayDuration={delay}>
      <RadixTooltip.Trigger asChild>
        {isValidElement(children) ? (children as ReactElement) : <span>{children}</span>}
      </RadixTooltip.Trigger>
      <RadixTooltip.Portal>
        <RadixTooltip.Content
          side={side}
          align={align}
          sideOffset={6}
          collisionPadding={8}
          className={cn(
            "z-[var(--z-tooltip)] flex items-center gap-2",
            "rounded-md border border-line-strong bg-overlay px-2 py-1 shadow-3",
            "text-xs text-fg",
            "select-none data-[state=delayed-open]:animate-zoom-in",
            "max-w-64",
          )}
        >
          <span className="min-w-0">{content}</span>
          {shortcut && <Kbd keys={shortcut} className="shrink-0" />}
          <RadixTooltip.Arrow
            width={9}
            height={4}
            className="fill-[var(--hc-overlay)] drop-shadow-[0_1px_0_var(--hc-border-strong)]"
          />
        </RadixTooltip.Content>
      </RadixTooltip.Portal>
    </RadixTooltip.Root>
  );
}

/** Wraps a trigger that is disabled — disabled elements do not fire pointer events. */
export function DisabledTooltip({ reason, children }: { reason: string; children: ReactElement }) {
  return (
    <Tooltip content={reason}>
      <span className="inline-flex cursor-not-allowed">
        {cloneElement(children, { style: { pointerEvents: "none" } } as never)}
      </span>
    </Tooltip>
  );
}
