import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";

/** SSR-safe layout effect. The portal renders on the server in some deployments. */
export const useIsomorphicLayoutEffect =
  typeof window !== "undefined" ? useLayoutEffect : useEffect;

/**
 * Persisted state backed by localStorage. Used for density, theme, sidebar
 * collapse, saved-view selection — anything that is a device preference rather
 * than account data. Failures are swallowed: private browsing must not break
 * the inbox.
 */
export function useLocalStorage<T>(key: string, initial: T) {
  const [value, setValue] = useState<T>(() => {
    if (typeof window === "undefined") return initial;
    try {
      const raw = window.localStorage.getItem(key);
      return raw === null ? initial : (JSON.parse(raw) as T);
    } catch {
      return initial;
    }
  });

  useEffect(() => {
    try {
      window.localStorage.setItem(key, JSON.stringify(value));
    } catch {
      /* quota exceeded or storage disabled — preference simply will not persist */
    }
  }, [key, value]);

  return [value, setValue] as const;
}

/** Debounce a rapidly-changing value. Search inputs, filter fields, autosave. */
export function useDebounced<T>(value: T, delay = 250): T {
  const [debounced, setDebounced] = useState(value);

  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay);
    return () => window.clearTimeout(timer);
  }, [value, delay]);

  return debounced;
}

/** Media query subscription. */
export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() => {
    if (typeof window === "undefined") return false;
    return window.matchMedia(query).matches;
  });

  useEffect(() => {
    const list = window.matchMedia(query);
    const onChange = (event: MediaQueryListEvent) => setMatches(event.matches);
    setMatches(list.matches);
    list.addEventListener("change", onChange);
    return () => list.removeEventListener("change", onChange);
  }, [query]);

  return matches;
}

/**
 * Global keyboard shortcuts.
 *
 * `combo` uses "mod+k" where mod is ⌘ on Apple platforms and Ctrl elsewhere.
 * Handlers are suppressed while the user is typing in a field unless
 * `allowInInput` is set, so single-letter shortcuts (§6.2 keyboard navigation)
 * are safe to register.
 */
export function useHotkey(
  combo: string,
  handler: (event: KeyboardEvent) => void,
  options: { allowInInput?: boolean; enabled?: boolean } = {},
) {
  const { allowInInput = false, enabled = true } = options;
  const handlerRef = useRef(handler);
  handlerRef.current = handler;

  useEffect(() => {
    if (!enabled) return;

    const parts = combo.toLowerCase().split("+");
    const key = parts[parts.length - 1] ?? "";
    const needsMod = parts.includes("mod");
    const needsShift = parts.includes("shift");
    const needsAlt = parts.includes("alt");

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key.toLowerCase() !== key) return;

      const mod = event.metaKey || event.ctrlKey;
      if (needsMod !== mod) return;
      if (needsShift !== event.shiftKey) return;
      if (needsAlt !== event.altKey) return;

      if (!allowInInput && isEditableTarget(event.target)) return;

      event.preventDefault();
      handlerRef.current(event);
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [combo, allowInInput, enabled]);
}

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  return (
    tag === "INPUT" ||
    tag === "TEXTAREA" ||
    tag === "SELECT" ||
    target.isContentEditable ||
    target.closest("[data-editor]") !== null
  );
}

/** Copy-to-clipboard with a self-resetting "copied" flag for button feedback. */
export function useCopyToClipboard(resetAfter = 1600) {
  const [copied, setCopied] = useState(false);
  const timerRef = useRef<number | undefined>(undefined);

  useEffect(() => () => window.clearTimeout(timerRef.current), []);

  const copy = useCallback(
    async (text: string) => {
      try {
        await navigator.clipboard.writeText(text);
        setCopied(true);
        window.clearTimeout(timerRef.current);
        timerRef.current = window.setTimeout(() => setCopied(false), resetAfter);
        return true;
      } catch {
        return false;
      }
    },
    [resetAfter],
  );

  return { copied, copy };
}

/**
 * A ticking clock for SLA countdowns and relative timestamps.
 *
 * One interval per component rather than one per row: the conversation list
 * passes the tick down so a thousand rows do not schedule a thousand timers.
 */
export function useNow(intervalMs = 30_000): Date {
  const [now, setNow] = useState(() => new Date());

  useEffect(() => {
    const id = window.setInterval(() => setNow(new Date()), intervalMs);
    return () => window.clearInterval(id);
  }, [intervalMs]);

  return now;
}

/** Observe an element's size. Used by virtualised lists and chart containers. */
export function useResizeObserver<T extends HTMLElement>() {
  const ref = useRef<T | null>(null);
  const [size, setSize] = useState({ width: 0, height: 0 });

  useIsomorphicLayoutEffect(() => {
    const element = ref.current;
    if (!element) return;

    const observer = new ResizeObserver(([entry]) => {
      if (!entry) return;
      const { width, height } = entry.contentRect;
      setSize({ width, height });
    });

    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  return { ref, ...size };
}

/** Detect clicks outside a node — for bare popovers not built on Radix. */
export function useClickOutside<T extends HTMLElement>(onOutside: () => void, enabled = true) {
  const ref = useRef<T | null>(null);
  const cbRef = useRef(onOutside);
  cbRef.current = onOutside;

  useEffect(() => {
    if (!enabled) return;
    const onPointerDown = (event: PointerEvent) => {
      const node = ref.current;
      if (node && event.target instanceof Node && !node.contains(event.target)) cbRef.current();
    };
    document.addEventListener("pointerdown", onPointerDown, true);
    return () => document.removeEventListener("pointerdown", onPointerDown, true);
  }, [enabled]);

  return ref;
}

/**
 * List keyboard navigation (j/k and arrows) with a controlled active index.
 * Shared by the conversation list, search results, and the mention picker.
 */
export function useListNavigation(count: number, onActivate?: (index: number) => void) {
  const [index, setIndex] = useState(0);

  useEffect(() => {
    if (index > count - 1) setIndex(Math.max(0, count - 1));
  }, [count, index]);

  const onKeyDown = useCallback(
    (event: React.KeyboardEvent) => {
      switch (event.key) {
        case "ArrowDown":
        case "j":
          event.preventDefault();
          setIndex((i) => Math.min(i + 1, count - 1));
          break;
        case "ArrowUp":
        case "k":
          event.preventDefault();
          setIndex((i) => Math.max(i - 1, 0));
          break;
        case "Home":
          event.preventDefault();
          setIndex(0);
          break;
        case "End":
          event.preventDefault();
          setIndex(count - 1);
          break;
        case "Enter":
          event.preventDefault();
          onActivate?.(index);
          break;
        default:
          break;
      }
    },
    [count, index, onActivate],
  );

  return { index, setIndex, onKeyDown };
}

/** True after the component has mounted. Guards portal/`document` access. */
export function useMounted(): boolean {
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);
  return mounted;
}
