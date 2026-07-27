import { AlertTriangle, CheckCircle2, Info, X, XCircle } from "lucide-react";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { cn } from "../lib/cn";
import { Spinner } from "./Spinner";

export type ToastTone = "neutral" | "success" | "warning" | "danger" | "loading";

export type Toast = {
  id: string;
  tone: ToastTone;
  title: ReactNode;
  description?: ReactNode;
  /** Single inline action. Undo, Retry, View — never more than one. */
  action?: { label: string; onClick: () => void };
  /** 0 keeps the toast until dismissed. Errors default to sticky. */
  duration?: number;
};

type ToastInput = Omit<Toast, "id" | "tone"> & { id?: string };

type ToastContextValue = {
  toast: (input: ToastInput & { tone?: ToastTone }) => string;
  success: (input: ToastInput) => string;
  error: (input: ToastInput) => string;
  warning: (input: ToastInput) => string;
  loading: (input: ToastInput) => string;
  dismiss: (id: string) => void;
  /** Swaps a live toast in place — for optimistic action → confirmed/failed. */
  update: (id: string, next: Partial<Omit<Toast, "id">>) => void;
};

const ToastContext = createContext<ToastContextValue | null>(null);

const DEFAULT_DURATION: Record<ToastTone, number> = {
  neutral: 4000,
  success: 3500,
  warning: 6000,
  danger: 0,
  loading: 0,
};

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const timers = useRef(new Map<string, number>());

  const dismiss = useCallback((id: string) => {
    setToasts((current) => current.filter((item) => item.id !== id));
    const timer = timers.current.get(id);
    if (timer) {
      window.clearTimeout(timer);
      timers.current.delete(id);
    }
  }, []);

  const schedule = useCallback(
    (id: string, duration: number) => {
      const existing = timers.current.get(id);
      if (existing) window.clearTimeout(existing);
      if (duration > 0) {
        timers.current.set(id, window.setTimeout(() => dismiss(id), duration));
      }
    },
    [dismiss],
  );

  const push = useCallback(
    (input: ToastInput & { tone?: ToastTone }) => {
      const tone = input.tone ?? "neutral";
      const id = input.id ?? `toast_${Math.random().toString(36).slice(2, 10)}`;
      const duration = input.duration ?? DEFAULT_DURATION[tone];

      setToasts((current) => {
        const next: Toast = { ...input, id, tone };
        const index = current.findIndex((item) => item.id === id);
        if (index >= 0) return current.map((item, i) => (i === index ? next : item));
        // Cap the stack. Older toasts drop off the top rather than piling up
        // and covering the composer.
        return [...current, next].slice(-4);
      });

      schedule(id, duration);
      return id;
    },
    [schedule],
  );

  const update = useCallback(
    (id: string, next: Partial<Omit<Toast, "id">>) => {
      setToasts((current) =>
        current.map((item) => (item.id === id ? { ...item, ...next } : item)),
      );
      const tone = next.tone;
      if (tone) schedule(id, next.duration ?? DEFAULT_DURATION[tone]);
    },
    [schedule],
  );

  useEffect(() => {
    const map = timers.current;
    return () => map.forEach((timer) => window.clearTimeout(timer));
  }, []);

  const value = useMemo<ToastContextValue>(
    () => ({
      toast: push,
      success: (input) => push({ ...input, tone: "success" }),
      error: (input) => push({ ...input, tone: "danger" }),
      warning: (input) => push({ ...input, tone: "warning" }),
      loading: (input) => push({ ...input, tone: "loading" }),
      dismiss,
      update,
    }),
    [push, dismiss, update],
  );

  return (
    <ToastContext.Provider value={value}>
      {children}
      <ToastViewport toasts={toasts} onDismiss={dismiss} />
    </ToastContext.Provider>
  );
}

export function useToast(): ToastContextValue {
  const context = useContext(ToastContext);
  if (!context) throw new Error("useToast must be used within a <ToastProvider>");
  return context;
}

const TONE_ICON: Record<ToastTone, ReactNode> = {
  neutral: <Info className="size-4 text-fg-muted" />,
  success: <CheckCircle2 className="size-4 text-success-text" />,
  warning: <AlertTriangle className="size-4 text-warning-text" />,
  danger: <XCircle className="size-4 text-danger-text" />,
  loading: <Spinner className="size-4 text-accent-text" />,
};

function ToastViewport({
  toasts,
  onDismiss,
}: {
  toasts: Toast[];
  onDismiss: (id: string) => void;
}) {
  return (
    <div
      // Bottom-right, above the composer but below dialogs' own focus scope.
      className="pointer-events-none fixed bottom-4 right-4 z-[var(--z-toast)] flex w-80 flex-col-reverse gap-2"
      role="region"
      aria-label="Notifications"
    >
      {toasts.map((toast) => (
        <div
          key={toast.id}
          role={toast.tone === "danger" ? "alert" : "status"}
          aria-live={toast.tone === "danger" ? "assertive" : "polite"}
          className={cn(
            "pointer-events-auto flex items-start gap-2.5 rounded-lg border p-3",
            "bg-overlay shadow-3 inset-shadow-highlight",
            "animate-fade-up",
            toast.tone === "danger" ? "border-danger-border" : "border-line-strong",
          )}
        >
          <span className="mt-px shrink-0">{TONE_ICON[toast.tone]}</span>

          <div className="min-w-0 flex-1">
            <p className="text-sm font-medium text-fg">{toast.title}</p>
            {toast.description && (
              <p className="mt-0.5 text-xs leading-snug text-fg-muted">{toast.description}</p>
            )}
            {toast.action && (
              <button
                type="button"
                onClick={() => {
                  toast.action?.onClick();
                  onDismiss(toast.id);
                }}
                className="mt-1.5 text-xs font-medium text-accent-text underline-offset-2 hover:underline"
              >
                {toast.action.label}
              </button>
            )}
          </div>

          <button
            type="button"
            onClick={() => onDismiss(toast.id)}
            aria-label="Dismiss"
            className="-m-0.5 shrink-0 rounded-xs p-0.5 text-fg-muted transition-colors hover:bg-fill hover:text-fg"
          >
            <X aria-hidden="true" className="size-3.5" />
          </button>
        </div>
      ))}
    </div>
  );
}
