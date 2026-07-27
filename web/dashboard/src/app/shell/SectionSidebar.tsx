import { CountBadge, Eyebrow, cn } from "@hubchat/shared";
import { NavLink, useLocation } from "react-router-dom";
import { useWorkspace } from "../workspace-context";
import type { NavLeaf, NavSection } from "./nav-config";

/**
 * Contextual sidebar for the active rail section. Renders nothing for sections
 * that are a single page (Overview, Tickets) — an empty 232px column is worse
 * than no column.
 */
export function SectionSidebar({ section }: { section: NavSection }) {
  const { pathname } = useLocation();
  const { can } = useWorkspace();

  if (!section.groups?.length) return null;

  return (
    <nav
      aria-label={`${section.label} navigation`}
      className="flex w-sidebar shrink-0 flex-col overflow-y-auto border-r border-line bg-surface"
    >
      <div className="sticky top-0 z-[var(--z-sticky)] border-b border-line bg-surface px-3 py-3">
        <h2 className="text-sm font-semibold tracking-tight text-fg">{section.label}</h2>
      </div>

      <div className="flex flex-col gap-4 p-2">
        {section.groups.map((group, index) => {
          const items = group.items.filter(
            (item) => !item.capability || can(item.capability),
          );
          if (items.length === 0) return null;

          return (
            <div key={group.label ?? index}>
              {group.label && <Eyebrow className="px-2 pb-1.5">{group.label}</Eyebrow>}
              <ul className="flex flex-col gap-px">
                {items.map((item) => (
                  <li key={item.to}>
                    <SidebarLink item={item} pathname={pathname} />
                  </li>
                ))}
              </ul>
            </div>
          );
        })}
      </div>
    </nav>
  );
}

function SidebarLink({ item, pathname }: { item: NavLeaf; pathname: string }) {
  const Icon = item.icon;
  const active = item.matchPrefix ? pathname.startsWith(item.to) : pathname === item.to;

  return (
    <NavLink
      to={item.to}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex items-center gap-2 rounded-md px-2 py-1.5 text-sm",
        "transition-colors duration-fast",
        active
          ? "bg-accent-subtle font-medium text-fg"
          : "text-fg-secondary hover:bg-fill hover:text-fg",
      )}
    >
      {Icon && (
        <Icon
          aria-hidden="true"
          className={cn("size-3.5 shrink-0", active ? "text-accent-text" : "text-fg-muted")}
        />
      )}
      <span className="min-w-0 flex-1 truncate">{item.label}</span>
    </NavLink>
  );
}

/** Reusable sidebar row for data-driven lists (saved views, boards, inboxes). */
export function SidebarItem({
  to,
  label,
  icon,
  count,
  active,
  accent,
}: {
  to: string;
  label: string;
  icon?: React.ReactNode;
  count?: number | null;
  active?: boolean;
  /** Emphasises the count — used for unread and breached queues. */
  accent?: boolean;
}) {
  return (
    <NavLink
      to={to}
      aria-current={active ? "page" : undefined}
      className={cn(
        "group flex items-center gap-2 rounded-md px-2 py-1.5 text-sm",
        "transition-colors duration-fast",
        active
          ? "bg-accent-subtle font-medium text-fg"
          : "text-fg-secondary hover:bg-fill hover:text-fg",
      )}
    >
      {icon && (
        <span
          className={cn(
            "shrink-0 [&_svg]:size-3.5",
            active ? "text-accent-text" : "text-fg-muted",
          )}
        >
          {icon}
        </span>
      )}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {count != null && count > 0 && (
        <CountBadge count={count} tone={accent ? "accent" : "neutral"} />
      )}
    </NavLink>
  );
}
