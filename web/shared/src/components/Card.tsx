import { forwardRef, type HTMLAttributes, type ReactNode } from "react";
import { cn } from "../lib/cn";

export type CardProps = HTMLAttributes<HTMLDivElement> & {
  /**
   * flat    — hairline only. The default; most of the product is flat.
   * raised  — fill + hairline + top highlight. For content that floats.
   * sunken  — recessed. For read-only payloads, previews, code.
   */
  variant?: "flat" | "raised" | "sunken";
  /** Adds hover affordance. Only for cards that are entirely clickable. */
  interactive?: boolean;
};

export const Card = forwardRef<HTMLDivElement, CardProps>(function Card(
  { variant = "flat", interactive, className, ...props },
  ref,
) {
  return (
    <div
      ref={ref}
      className={cn(
        "rounded-lg border border-line",
        variant === "flat" && "bg-surface",
        variant === "raised" && "bg-raised inset-shadow-highlight shadow-2",
        variant === "sunken" && "bg-inset",
        interactive &&
          "cursor-pointer transition-[border-color,background-color] duration-fast ease-out hover:border-line-strong hover:bg-surface-hover",
        className,
      )}
      {...props}
    />
  );
});

export function CardHeader({
  title,
  description,
  actions,
  className,
  children,
}: {
  title?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  className?: string;
  children?: ReactNode;
}) {
  return (
    <div
      className={cn(
        "flex items-start justify-between gap-4 border-b border-line px-[var(--hc-card-p)] py-3",
        className,
      )}
    >
      <div className="min-w-0">
        {title && <h3 className="text-sm font-semibold text-fg">{title}</h3>}
        {description && (
          <p className="mt-0.5 text-xs leading-snug text-fg-muted">{description}</p>
        )}
        {children}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-1.5">{actions}</div>}
    </div>
  );
}

export function CardBody({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("p-[var(--hc-card-p)]", className)} {...props} />;
}

export function CardFooter({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "flex items-center justify-between gap-3 border-t border-line bg-inset px-[var(--hc-card-p)] py-3",
        className,
      )}
      {...props}
    />
  );
}
