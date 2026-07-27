import { Search, X } from "lucide-react";
import {
  forwardRef,
  useId,
  type InputHTMLAttributes,
  type ReactNode,
  type TextareaHTMLAttributes,
} from "react";
import { cn } from "../lib/cn";

export type InputSize = "sm" | "md" | "lg";

const SIZES: Record<InputSize, string> = {
  sm: "h-control-sm text-xs rounded-sm px-2",
  md: "h-control-md text-sm rounded-md px-2.5",
  lg: "h-control-lg text-base rounded-md px-3",
};

/**
 * The shared shell for every text-entry control.
 *
 * Inputs sit *below* the surface they live on rather than above it — a sunken
 * fill plus a hairline. That inversion is what makes a form scan as editable
 * without needing a heavier border.
 */
export const inputShell = cn(
  "w-full min-w-0 bg-inset text-fg",
  "border border-line",
  "placeholder:text-fg-disabled",
  "transition-[border-color,background-color,box-shadow] duration-fast ease-out",
  "hover:border-line-strong",
  "focus:border-accent focus:bg-surface focus:outline-none",
  "focus:shadow-[0_0_0_3px_var(--hc-accent-subtle)]",
  "disabled:cursor-not-allowed disabled:bg-fill disabled:text-fg-disabled disabled:hover:border-line",
  "aria-[invalid=true]:border-danger aria-[invalid=true]:focus:border-danger",
  "aria-[invalid=true]:focus:shadow-[0_0_0_3px_var(--hc-danger-subtle)]",
);

export type InputProps = Omit<InputHTMLAttributes<HTMLInputElement>, "size"> & {
  inputSize?: InputSize;
  /** Rendered inside the field on the leading edge. Icons only. */
  leading?: ReactNode;
  /** Rendered inside the field on the trailing edge. Icon, unit, or shortcut. */
  trailing?: ReactNode;
  invalid?: boolean;
  /** Static text fused to the field, e.g. "https://" or ".hubchat.app". */
  prefix?: string;
  suffix?: string;
  mono?: boolean;
};

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  {
    inputSize = "md",
    leading,
    trailing,
    invalid,
    prefix,
    suffix,
    mono,
    className,
    disabled,
    ...props
  },
  ref,
) {
  const field = (
    <input
      ref={ref}
      disabled={disabled}
      aria-invalid={invalid || undefined}
      className={cn(
        inputShell,
        SIZES[inputSize],
        mono && "font-mono text-xs tracking-normal",
        leading && "pl-8",
        trailing && "pr-8",
        (prefix || suffix) && "rounded-none border-x-0 focus:shadow-none",
        prefix && "rounded-l-none",
        suffix && "rounded-r-none",
        !prefix && !suffix && className,
      )}
      {...props}
    />
  );

  if (!leading && !trailing && !prefix && !suffix) return field;

  // Affixes need a wrapper that owns the focus ring, so the ring traces the
  // whole control rather than just the <input> in the middle.
  if (prefix || suffix) {
    return (
      <div
        className={cn(
          "flex w-full items-stretch",
          "rounded-md border border-line bg-inset",
          "transition-[border-color,box-shadow] duration-fast ease-out",
          "focus-within:border-accent focus-within:shadow-[0_0_0_3px_var(--hc-accent-subtle)]",
          invalid && "border-danger focus-within:border-danger",
          disabled && "opacity-60",
          className,
        )}
      >
        {prefix && <Affix position="start" size={inputSize}>{prefix}</Affix>}
        <div className="relative flex-1">{field}</div>
        {suffix && <Affix position="end" size={inputSize}>{suffix}</Affix>}
      </div>
    );
  }

  return (
    <div className={cn("relative w-full", className)}>
      {leading && (
        <span className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-muted [&_svg]:size-3.5">
          {leading}
        </span>
      )}
      {field}
      {trailing && (
        <span className="absolute right-2 top-1/2 -translate-y-1/2 text-fg-muted [&_svg]:size-3.5">
          {trailing}
        </span>
      )}
    </div>
  );
});

function Affix({
  children,
  position,
  size,
}: {
  children: ReactNode;
  position: "start" | "end";
  size: InputSize;
}) {
  return (
    <span
      className={cn(
        "flex shrink-0 select-none items-center bg-fill px-2.5 text-fg-muted",
        size === "sm" ? "text-xs" : "text-sm",
        position === "start" ? "rounded-l-md border-r border-line" : "rounded-r-md border-l border-line",
      )}
    >
      {children}
    </span>
  );
}

export type TextareaProps = TextareaHTMLAttributes<HTMLTextAreaElement> & {
  invalid?: boolean;
  /** Grows with content up to `maxRows`, then scrolls. */
  autoResize?: boolean;
  maxRows?: number;
};

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(function Textarea(
  { invalid, autoResize, maxRows = 12, className, rows = 3, onInput, ...props },
  ref,
) {
  return (
    <textarea
      ref={ref}
      rows={rows}
      aria-invalid={invalid || undefined}
      onInput={(event) => {
        if (autoResize) {
          const el = event.currentTarget;
          el.style.height = "auto";
          const lineHeight = parseFloat(getComputedStyle(el).lineHeight || "20");
          el.style.height = `${Math.min(el.scrollHeight, lineHeight * maxRows)}px`;
        }
        onInput?.(event);
      }}
      className={cn(
        inputShell,
        "resize-y rounded-md px-2.5 py-2 text-sm leading-normal",
        autoResize && "resize-none overflow-y-auto",
        className,
      )}
      {...props}
    />
  );
});

export type SearchInputProps = Omit<InputProps, "leading" | "type"> & {
  onClear?: () => void;
  /** Rendered as a ⌘K-style hint on the trailing edge while empty. */
  shortcut?: string;
};

/** Search field with a leading glyph, a clear affordance, and an optional hint. */
export const SearchInput = forwardRef<HTMLInputElement, SearchInputProps>(function SearchInput(
  { onClear, shortcut, value, className, ...props },
  ref,
) {
  const hasValue = typeof value === "string" ? value.length > 0 : value != null;

  return (
    <Input
      ref={ref}
      type="search"
      role="searchbox"
      value={value}
      leading={<Search aria-hidden="true" />}
      className={cn("[&::-webkit-search-cancel-button]:hidden", className)}
      trailing={
        hasValue && onClear ? (
          <button
            type="button"
            onClick={onClear}
            aria-label="Clear search"
            className="rounded-xs p-0.5 text-fg-muted transition-colors hover:bg-fill hover:text-fg"
          >
            <X aria-hidden="true" />
          </button>
        ) : shortcut ? (
          <kbd className="pointer-events-none rounded-xs border border-line bg-fill px-1 py-px font-mono text-2xs text-fg-muted">
            {shortcut}
          </kbd>
        ) : undefined
      }
      {...props}
    />
  );
});

export type FieldProps = {
  label?: ReactNode;
  /** Guidance shown before the user acts. Never used for errors. */
  description?: ReactNode;
  /** Replaces `description` while present and marks the control invalid. */
  error?: ReactNode;
  hint?: ReactNode;
  required?: boolean;
  /** Renders label and control side by side — used in settings forms. */
  orientation?: "vertical" | "horizontal";
  htmlFor?: string;
  children: ReactNode;
  className?: string;
};

/**
 * Wires up label/description/error to the control via aria-describedby.
 * Pass a function child to receive the generated id when the control is not a
 * plain input (Radix Select, a custom editor, and so on).
 */
export function Field({
  label,
  description,
  error,
  hint,
  required,
  orientation = "vertical",
  htmlFor,
  children,
  className,
}: FieldProps) {
  const generatedId = useId();
  const controlId = htmlFor ?? generatedId;
  const describedBy = error ? `${controlId}-error` : description ? `${controlId}-desc` : undefined;

  return (
    <div
      className={cn(
        orientation === "horizontal"
          ? "grid grid-cols-[minmax(0,180px)_minmax(0,1fr)] items-start gap-x-6 gap-y-1"
          : "flex flex-col gap-1.5",
        className,
      )}
    >
      {label && (
        <div className={cn("flex items-baseline justify-between gap-2", orientation === "horizontal" && "pt-1.5")}>
          <label htmlFor={controlId} className="text-xs font-medium text-fg-secondary">
            {label}
            {required && (
              <span className="ml-0.5 text-danger-text" aria-hidden="true">
                *
              </span>
            )}
          </label>
          {hint && <span className="text-2xs text-fg-muted">{hint}</span>}
        </div>
      )}

      <div className={cn("flex flex-col gap-1.5", orientation === "horizontal" && "min-w-0")}>
        <div id={describedBy ? undefined : controlId} className="contents">
          {children}
        </div>

        {error ? (
          <p id={`${controlId}-error`} role="alert" className="text-xs text-danger-text">
            {error}
          </p>
        ) : description ? (
          <p id={`${controlId}-desc`} className="text-xs leading-snug text-fg-muted">
            {description}
          </p>
        ) : null}
      </div>
    </div>
  );
}
