import { Slot, Slottable } from "@radix-ui/react-slot";
import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { Spinner } from "./Spinner";

export type ButtonVariant =
  | "primary"
  | "secondary"
  | "ghost"
  | "outline"
  | "danger"
  | "danger-ghost"
  | "link";

export type ButtonSize = "xs" | "sm" | "md" | "lg";

/**
 * Variant vocabulary, deliberately small:
 *
 *   primary   — the single most important action on a screen. One per view.
 *   secondary — everything else that commits a change.
 *   outline   — secondary, but on a busy surface where a fill would add noise.
 *   ghost     — toolbar and row-level actions; earns a fill only on hover.
 *   danger    — destructive and irreversible.
 *   link      — inline navigation inside prose.
 *
 * Nothing here uses a gradient or a coloured shadow. Depth comes from the
 * 1px top highlight that every raised element in the system shares.
 */
const VARIANTS: Record<ButtonVariant, string> = {
  primary: cn(
    "bg-accent text-accent-fg shadow-1",
    "inset-shadow-[0_1px_0_0_rgb(255_255_255/0.14)]",
    "hover:bg-accent-hover active:bg-accent-active",
    "disabled:bg-fill disabled:text-fg-disabled disabled:shadow-none disabled:inset-shadow-none",
  ),
  secondary: cn(
    "bg-fill text-fg border border-line",
    "inset-shadow-highlight",
    "hover:bg-fill-hover hover:border-line-strong active:bg-fill-active",
    "disabled:text-fg-disabled disabled:bg-fill disabled:border-line-subtle",
  ),
  outline: cn(
    "text-fg border border-line-strong",
    "hover:bg-fill hover:border-line-loud active:bg-fill-hover",
    "disabled:text-fg-disabled disabled:border-line",
  ),
  ghost: cn(
    "text-fg-secondary",
    "hover:bg-fill hover:text-fg active:bg-fill-hover",
    "disabled:text-fg-disabled disabled:bg-transparent",
  ),
  danger: cn(
    "bg-danger text-danger-fg shadow-1",
    "inset-shadow-[0_1px_0_0_rgb(255_255_255/0.14)]",
    "hover:bg-danger-hover active:bg-danger",
    "disabled:bg-fill disabled:text-fg-disabled disabled:shadow-none",
  ),
  "danger-ghost": cn(
    "text-danger-text",
    "hover:bg-danger-subtle active:bg-danger-subtle",
    "disabled:text-fg-disabled disabled:bg-transparent",
  ),
  link: cn(
    "text-accent-text underline-offset-4 hover:underline",
    "disabled:text-fg-disabled disabled:no-underline",
  ),
};

const SIZES: Record<ButtonSize, string> = {
  xs: "h-control-xs px-1.5 gap-1 text-2xs rounded-xs",
  sm: "h-control-sm px-2.5 gap-1.5 text-xs rounded-sm",
  md: "h-control-md px-3 gap-1.5 text-sm rounded-md",
  lg: "h-control-lg px-4 gap-2 text-base rounded-md",
};

const ICON_SIZES: Record<ButtonSize, string> = {
  xs: "w-control-xs px-0",
  sm: "w-control-sm px-0",
  md: "w-control-md px-0",
  lg: "w-control-lg px-0",
};

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant;
  size?: ButtonSize;
  /** Renders a spinner in place of `leading` and blocks interaction. */
  loading?: boolean;
  leading?: ReactNode;
  trailing?: ReactNode;
  /** Square, no horizontal padding. `aria-label` becomes mandatory. */
  iconOnly?: boolean;
  fullWidth?: boolean;
  /** Merge props onto the child element instead of rendering a <button>. */
  asChild?: boolean;
};

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  {
    variant = "secondary",
    size = "md",
    loading = false,
    leading,
    trailing,
    iconOnly = false,
    fullWidth = false,
    asChild = false,
    className,
    children,
    disabled,
    type = "button",
    ...props
  },
  ref,
) {
  const Component = asChild ? Slot : "button";
  const isDisabled = disabled || loading;

  return (
    <Component
      ref={ref}
      type={asChild ? undefined : type}
      disabled={asChild ? undefined : isDisabled}
      aria-busy={loading || undefined}
      data-variant={variant}
      className={cn(
        "relative inline-flex shrink-0 select-none items-center justify-center whitespace-nowrap",
        "font-medium tracking-normal",
        "transition-[background-color,border-color,color,box-shadow] duration-fast ease-out",
        "disabled:pointer-events-none",
        // Icons never scale with the label; a 14px glyph reads correctly at
        // every size in this scale and keeps optical weight consistent.
        "[&_svg]:size-3.5 [&_svg]:shrink-0",
        size === "lg" && "[&_svg]:size-4",
        VARIANTS[variant],
        variant !== "link" && SIZES[size],
        iconOnly && ICON_SIZES[size],
        fullWidth && "w-full",
        className,
      )}
      {...props}
    >
      {/*
        `Slottable` is what lets a button keep its icons while delegating to a
        child element — `<Button asChild leading={<X />}><Link/></Button>`.
        Without it Slot counts three children and refuses to merge.

        It is applied unconditionally in the asChild path, including when
        `iconOnly` is set: dropping the child there would leave Slot with
        nothing to merge onto, which is exactly the crash this guards against.
      */}
      {loading ? <Spinner className={iconOnly ? undefined : "-ml-0.5"} /> : leading}
      {asChild ? <Slottable>{children}</Slottable> : !iconOnly && children}
      {!loading && trailing}
    </Component>
  );
});

/** Convenience wrapper. Enforces the label that `iconOnly` alone cannot. */
export type IconButtonProps = Omit<ButtonProps, "iconOnly" | "children" | "trailing"> & {
  label: string;
  children: ReactNode;
};

export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(function IconButton(
  { label, children, variant = "ghost", ...props },
  ref,
) {
  return (
    <Button ref={ref} iconOnly variant={variant} aria-label={label} leading={children} {...props} />
  );
});

/** Groups buttons into a single segmented control with shared borders. */
export function ButtonGroup({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <div
      role="group"
      className={cn(
        "inline-flex items-center",
        "[&>*:not(:first-child)]:rounded-l-none [&>*:not(:last-child)]:rounded-r-none",
        "[&>*:not(:first-child)]:-ml-px [&>*:hover]:z-10 [&>*:focus-visible]:z-10",
        className,
      )}
    >
      {children}
    </div>
  );
}
