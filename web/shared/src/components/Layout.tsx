import { ChevronRight } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../lib/cn";

/* -------------------------------------------------------------------------- */
/*  Page scaffolding                                                           */
/* -------------------------------------------------------------------------- */

export type PageHeaderProps = {
  title: ReactNode;
  description?: ReactNode;
  breadcrumbs?: { label: string; href?: string }[];
  actions?: ReactNode;
  /** Status chips beside the title — enabled/disabled, version, environment. */
  meta?: ReactNode;
  /** Tabs or a filter row fused to the bottom edge of the header. */
  tabs?: ReactNode;
  /** Back affordance for detail routes. */
  back?: ReactNode;
  className?: string;
};

export function PageHeader({
  title,
  description,
  breadcrumbs,
  actions,
  meta,
  tabs,
  back,
  className,
}: PageHeaderProps) {
  return (
    <header className={cn("shrink-0 border-b border-line bg-surface", className)}>
      <div className="px-6 pb-4 pt-5">
        {breadcrumbs && breadcrumbs.length > 0 && (
          <Breadcrumbs items={breadcrumbs} className="mb-2" />
        )}

        <div className="flex items-start justify-between gap-6">
          <div className="flex min-w-0 items-start gap-2">
            {back}
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <h1 className="truncate text-lg font-semibold tracking-tight text-fg">{title}</h1>
                {meta}
              </div>
              {description && (
                <p className="mt-1 max-w-measure text-xs leading-normal text-fg-muted">
                  {description}
                </p>
              )}
            </div>
          </div>

          {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
        </div>
      </div>

      {tabs && <div className="px-6">{tabs}</div>}
    </header>
  );
}

/** Scrollable body for a standard page. Owns the max width and page padding. */
export function PageBody({
  children,
  className,
  width = "wide",
}: {
  children: ReactNode;
  className?: string;
  /** narrow — forms and settings. wide — tables and dashboards. full — canvases. */
  width?: "narrow" | "wide" | "full";
}) {
  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div
        className={cn(
          "px-[var(--hc-page-px)] py-[var(--hc-page-py)]",
          width === "narrow" && "mx-auto max-w-3xl",
          width === "wide" && "mx-auto max-w-7xl",
          width === "full" && "max-w-none",
          className,
        )}
      >
        {children}
      </div>
    </div>
  );
}

/** Full-height column: header stays put, body scrolls. */
export function Page({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("flex h-full min-h-0 flex-col bg-canvas", className)}>{children}</div>;
}

/* -------------------------------------------------------------------------- */
/*  Toolbar                                                                    */
/* -------------------------------------------------------------------------- */

export function Toolbar({
  leading,
  trailing,
  className,
}: {
  leading?: ReactNode;
  trailing?: ReactNode;
  className?: string;
}) {
  return (
    <div
      role="toolbar"
      className={cn(
        "flex shrink-0 items-center justify-between gap-3 border-b border-line bg-surface px-3 py-2",
        className,
      )}
    >
      <div className="flex min-w-0 items-center gap-1.5">{leading}</div>
      <div className="flex shrink-0 items-center gap-1.5">{trailing}</div>
    </div>
  );
}

export function ToolbarDivider() {
  return <span aria-hidden="true" className="mx-1 h-4 w-px shrink-0 bg-line" />;
}

/* -------------------------------------------------------------------------- */
/*  Sections & settings rows                                                   */
/* -------------------------------------------------------------------------- */

export function Section({
  title,
  description,
  actions,
  children,
  className,
}: {
  title?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={cn("mb-[var(--hc-section-gap)] last:mb-0", className)}>
      {(title || actions) && (
        <div className="mb-3 flex items-end justify-between gap-4">
          <div className="min-w-0">
            {title && <h2 className="text-sm font-semibold text-fg">{title}</h2>}
            {description && (
              <p className="mt-0.5 max-w-measure text-xs leading-normal text-fg-muted">
                {description}
              </p>
            )}
          </div>
          {actions && <div className="flex shrink-0 items-center gap-1.5">{actions}</div>}
        </div>
      )}
      {children}
    </section>
  );
}

/**
 * Label-left / control-right row. The backbone of every settings screen, so the
 * label column width is fixed globally rather than per page — settings pages
 * across the product line up when you switch between them.
 */
export function SettingsRow({
  label,
  description,
  children,
  htmlFor,
  className,
}: {
  label: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  htmlFor?: string;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "grid gap-x-6 gap-y-2 border-b border-line-subtle py-4 last:border-b-0",
        "sm:grid-cols-[minmax(0,260px)_minmax(0,1fr)]",
        className,
      )}
    >
      <div className="min-w-0">
        <label htmlFor={htmlFor} className="text-sm font-medium text-fg">
          {label}
        </label>
        {description && (
          <p className="mt-1 text-xs leading-normal text-fg-muted">{description}</p>
        )}
      </div>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/*  Breadcrumbs                                                                */
/* -------------------------------------------------------------------------- */

export function Breadcrumbs({
  items,
  className,
}: {
  items: { label: string; href?: string }[];
  className?: string;
}) {
  return (
    <nav aria-label="Breadcrumb" className={cn("flex items-center gap-1 text-xs", className)}>
      {items.map((item, index) => {
        const isLast = index === items.length - 1;
        return (
          <span key={`${item.label}-${index}`} className="flex items-center gap-1">
            {index > 0 && (
              <ChevronRight aria-hidden="true" className="size-3 shrink-0 text-fg-disabled" />
            )}
            {item.href && !isLast ? (
              <a
                href={item.href}
                className="rounded-xs text-fg-muted transition-colors hover:text-fg"
              >
                {item.label}
              </a>
            ) : (
              <span className={isLast ? "text-fg-secondary" : "text-fg-muted"} aria-current={isLast ? "page" : undefined}>
                {item.label}
              </span>
            )}
          </span>
        );
      })}
    </nav>
  );
}

/* -------------------------------------------------------------------------- */
/*  Split pane                                                                 */
/* -------------------------------------------------------------------------- */

/**
 * Fixed-width sidebars around a fluid centre. Widths come from shell geometry
 * tokens so the inbox, ticket view, and search results all align to the same
 * vertical rules.
 */
export function SplitPane({
  start,
  children,
  end,
  className,
}: {
  start?: ReactNode;
  children: ReactNode;
  end?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex h-full min-h-0 w-full", className)}>
      {start && (
        <div className="flex w-list shrink-0 flex-col border-r border-line bg-surface">{start}</div>
      )}
      <div className="flex min-w-0 flex-1 flex-col">{children}</div>
      {end && (
        <aside className="hidden w-context shrink-0 flex-col overflow-y-auto border-l border-line bg-surface xl:flex">
          {end}
        </aside>
      )}
    </div>
  );
}

export function Separator({
  orientation = "horizontal",
  className,
}: {
  orientation?: "horizontal" | "vertical";
  className?: string;
}) {
  return (
    <div
      role="separator"
      aria-orientation={orientation}
      className={cn(
        "shrink-0 bg-line",
        orientation === "horizontal" ? "h-px w-full" : "h-full w-px",
        className,
      )}
    />
  );
}

/** Small uppercase heading used inside panels and context sidebars. */
export function Eyebrow({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <p
      className={cn(
        "text-2xs font-semibold uppercase tracking-caps text-fg-muted",
        className,
      )}
    >
      {children}
    </p>
  );
}

/** Key/value line for context panels and detail summaries. */
export function DetailRow({
  label,
  children,
  className,
}: {
  label: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex items-start justify-between gap-3 py-1.5 text-xs", className)}>
      <dt className="shrink-0 text-fg-muted">{label}</dt>
      <dd className="min-w-0 text-right text-fg-secondary">{children}</dd>
    </div>
  );
}
