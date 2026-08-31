import { cn } from "@hubchat/shared";
import { Outlet } from "react-router-dom";
import { Wordmark } from "../../components/Wordmark";

/**
 * Chrome for every unauthenticated screen.
 *
 * Two columns on wide viewports: the form on the left at a fixed, comfortable
 * measure, and a quiet panel on the right. The panel carries no marketing —
 * this is a self-hosted tool and the person signing in already chose it. It
 * states what the deployment is, which is genuinely useful when an operator
 * runs staging and production side by side.
 */
export function AuthLayout() {
  return (
    <div className="flex min-h-dvh bg-canvas">
      <div className="flex w-full flex-col justify-center px-6 py-12 lg:w-[46%] lg:px-16">
        <div className="mx-auto w-full max-w-sm">
          <Wordmark className="mb-10" />
          <Outlet />
        </div>
      </div>

      <aside
        aria-hidden="true"
        className={cn(
          "relative hidden flex-1 overflow-hidden border-l border-line bg-sunken lg:block",
        )}
      >
        <div className="hc-grid-bg absolute inset-0 opacity-70" />
        <div
          className="absolute inset-0"
          style={{
            background:
              "radial-gradient(60% 50% at 70% 30%, color-mix(in oklab, var(--hc-accent) 14%, transparent), transparent 70%)",
          }}
        />

        <div className="relative flex h-full flex-col justify-end p-16">
          <blockquote className="max-w-md">
            <p className="text-xl font-semibold leading-snug tracking-tight text-fg">
              Live chat, ticketing, portals, feedback, and customer context in one Go
              binary you can run yourself.
            </p>
            <footer className="mt-4 flex items-center gap-2 text-xs text-fg-muted">
              <span className="h-px w-6 bg-line-loud" />
              Self-hosted · PostgreSQL · No AI
            </footer>
          </blockquote>
        </div>
      </aside>
    </div>
  );
}
