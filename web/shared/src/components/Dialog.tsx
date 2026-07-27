import * as RadixDialog from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../lib/cn";
import { Button } from "./Button";

export const Dialog = RadixDialog.Root;
export const DialogTrigger = RadixDialog.Trigger;
export const DialogClose = RadixDialog.Close;

const scrim = cn(
  "fixed inset-0 z-[var(--z-overlay)] bg-scrim backdrop-blur-[2px]",
  "data-[state=open]:animate-fade",
);

export type DialogContentProps = {
  title: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
  size?: "sm" | "md" | "lg" | "xl";
  /** Suppresses the × — for flows that must be completed or explicitly cancelled. */
  hideClose?: boolean;
  className?: string;
};

const SIZES = {
  sm: "max-w-sm",
  md: "max-w-md",
  lg: "max-w-xl",
  xl: "max-w-3xl",
} as const;

export function DialogContent({
  title,
  description,
  children,
  footer,
  size = "md",
  hideClose,
  className,
}: DialogContentProps) {
  return (
    <RadixDialog.Portal>
      <RadixDialog.Overlay className={scrim} />
      <RadixDialog.Content
        className={cn(
          "fixed left-1/2 top-1/2 z-[var(--z-dialog)] w-[calc(100vw-2rem)] -translate-x-1/2 -translate-y-1/2",
          "flex max-h-[calc(100vh-4rem)] flex-col overflow-hidden",
          "rounded-xl border border-line-strong bg-overlay shadow-4 inset-shadow-highlight",
          "data-[state=open]:animate-zoom-in",
          SIZES[size],
          className,
        )}
      >
        <header className="flex items-start justify-between gap-4 px-5 pb-3 pt-4">
          <div className="min-w-0">
            <RadixDialog.Title className="text-md font-semibold tracking-tight text-fg">
              {title}
            </RadixDialog.Title>
            {description && (
              <RadixDialog.Description className="mt-1 text-xs leading-normal text-fg-muted">
                {description}
              </RadixDialog.Description>
            )}
          </div>
          {!hideClose && (
            <RadixDialog.Close asChild>
              <Button variant="ghost" size="sm" iconOnly aria-label="Close" leading={<X />} />
            </RadixDialog.Close>
          )}
        </header>

        {children && <div className="min-h-0 flex-1 overflow-y-auto px-5 py-1">{children}</div>}

        {footer && (
          <footer className="mt-2 flex items-center justify-end gap-2 border-t border-line bg-inset px-5 py-3">
            {footer}
          </footer>
        )}
      </RadixDialog.Content>
    </RadixDialog.Portal>
  );
}

export type ConfirmDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  destructive?: boolean;
  loading?: boolean;
  onConfirm: () => void;
  /** Requires the user to type this exact string. For irreversible operations. */
  confirmationPhrase?: string;
  children?: ReactNode;
};

/**
 * The one confirmation surface. §12 requires deletion flows to state precisely
 * what will happen — so `description` is required and should describe the
 * outcome ("42 conversations will be anonymised"), never ask "Are you sure?".
 */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel = "Confirm",
  cancelLabel = "Cancel",
  destructive,
  loading,
  onConfirm,
  children,
}: ConfirmDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={title}
        description={description}
        size="sm"
        footer={
          <>
            <DialogClose asChild>
              <Button variant="ghost" size="sm">
                {cancelLabel}
              </Button>
            </DialogClose>
            <Button
              variant={destructive ? "danger" : "primary"}
              size="sm"
              loading={loading}
              onClick={onConfirm}
            >
              {confirmLabel}
            </Button>
          </>
        }
      >
        {children}
      </DialogContent>
    </Dialog>
  );
}

/* -------------------------------------------------------------------------- */
/*  Sheet — the side panel variant                                             */
/* -------------------------------------------------------------------------- */

export const Sheet = RadixDialog.Root;
export const SheetTrigger = RadixDialog.Trigger;
export const SheetClose = RadixDialog.Close;

export type SheetContentProps = {
  title: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
  side?: "right" | "bottom";
  size?: "sm" | "md" | "lg";
  className?: string;
};

/**
 * Sheets are for editing something *in context* — a ticket's fields while its
 * timeline stays visible, a widget's appearance beside its live preview. If the
 * user must leave the context anyway, use a route, not a sheet.
 */
export function SheetContent({
  title,
  description,
  children,
  footer,
  side = "right",
  size = "md",
  className,
}: SheetContentProps) {
  const widths = { sm: "sm:max-w-sm", md: "sm:max-w-md", lg: "sm:max-w-2xl" } as const;

  return (
    <RadixDialog.Portal>
      <RadixDialog.Overlay className={scrim} />
      <RadixDialog.Content
        className={cn(
          "fixed z-[var(--z-dialog)] flex flex-col overflow-hidden bg-surface shadow-4",
          side === "right" && [
            "inset-y-0 right-0 w-full border-l border-line",
            widths[size],
            "data-[state=open]:animate-slide-right",
          ],
          side === "bottom" && [
            "inset-x-0 bottom-0 max-h-[85vh] rounded-t-xl border-t border-line",
            "data-[state=open]:animate-slide-bottom",
          ],
          className,
        )}
      >
        <header className="flex items-start justify-between gap-4 border-b border-line px-4 py-3">
          <div className="min-w-0">
            <RadixDialog.Title className="text-sm font-semibold text-fg">{title}</RadixDialog.Title>
            {description && (
              <RadixDialog.Description className="mt-0.5 text-xs text-fg-muted">
                {description}
              </RadixDialog.Description>
            )}
          </div>
          <RadixDialog.Close asChild>
            <Button variant="ghost" size="sm" iconOnly aria-label="Close" leading={<X />} />
          </RadixDialog.Close>
        </header>

        <div className="min-h-0 flex-1 overflow-y-auto p-4">{children}</div>

        {footer && (
          <footer className="flex items-center justify-end gap-2 border-t border-line bg-inset px-4 py-3">
            {footer}
          </footer>
        )}
      </RadixDialog.Content>
    </RadixDialog.Portal>
  );
}
