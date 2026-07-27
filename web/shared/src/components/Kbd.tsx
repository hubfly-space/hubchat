import { cn } from "../lib/cn";

const SYMBOLS: Record<string, string> = {
  mod: typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘" : "Ctrl",
  cmd: "⌘",
  ctrl: "Ctrl",
  shift: "⇧",
  alt: typeof navigator !== "undefined" && /Mac/.test(navigator.platform) ? "⌥" : "Alt",
  enter: "↵",
  escape: "Esc",
  esc: "Esc",
  backspace: "⌫",
  delete: "⌦",
  tab: "⇥",
  space: "Space",
  up: "↑",
  down: "↓",
  left: "←",
  right: "→",
};

export type KbdProps = {
  /** "mod+k", "shift+enter", or a bare key. Platform symbols are substituted. */
  keys: string;
  size?: "sm" | "md";
  className?: string;
};

/**
 * Keyboard hint. §6.2 makes the inbox fully keyboard-driven, so shortcuts are
 * surfaced everywhere — in menus, tooltips, empty states, and the composer.
 */
export function Kbd({ keys, size = "sm", className }: KbdProps) {
  const parts = keys.split("+").map((part) => SYMBOLS[part.toLowerCase()] ?? part.toUpperCase());

  return (
    <kbd
      className={cn(
        "inline-flex select-none items-center gap-0.5 font-sans font-medium text-fg-muted",
        className,
      )}
    >
      {parts.map((part, index) => (
        <span
          key={index}
          className={cn(
            "inline-grid place-items-center rounded-xs border border-line bg-fill",
            size === "sm" ? "h-4 min-w-4 px-1 text-2xs" : "h-5 min-w-5 px-1.5 text-xs",
          )}
        >
          {part}
        </span>
      ))}
    </kbd>
  );
}
