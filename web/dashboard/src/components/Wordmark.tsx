import { cn } from "@hubchat/shared";

/**
 * The mark: a speech bubble whose tail doubles as a cursor caret, drawn on the
 * same 24px grid as every icon in the product. Monochrome by default; the
 * accent appears only on the caret, which is the one element that implies a
 * live conversation.
 */
export function Logo({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
      className={cn("size-6", className)}
    >
      <path
        d="M4 6.5A2.5 2.5 0 0 1 6.5 4h11A2.5 2.5 0 0 1 20 6.5v8a2.5 2.5 0 0 1-2.5 2.5H11l-4.2 3.5A.5.5 0 0 1 6 20.1V17h-.5A1.5 1.5 0 0 1 4 15.5v-9Z"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinejoin="round"
      />
      <path
        d="M9 10.5h6M9 13.5h3.5"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        opacity="0.55"
      />
      <circle cx="17.5" cy="6.5" r="2.5" fill="var(--hc-accent)" stroke="var(--hc-canvas)" strokeWidth="1.5" />
    </svg>
  );
}

export function Wordmark({
  className,
  size = "md",
}: {
  className?: string;
  size?: "sm" | "md";
}) {
  return (
    <span className={cn("inline-flex items-center gap-2 text-fg", className)}>
      <Logo className={size === "sm" ? "size-5" : "size-6"} />
      <span
        className={cn(
          "font-semibold tracking-tight",
          size === "sm" ? "text-sm" : "text-md",
        )}
      >
        Hubchat
      </span>
    </span>
  );
}
