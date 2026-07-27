import { CountBadge, Tooltip, cn } from "@hubchat/shared";
import { NavLink, useLocation } from "react-router-dom";
import { useWorkspace } from "../workspace-context";
import { NAV_SECTIONS, type NavSection } from "./nav-config";
import { WorkspaceSwitcher } from "./WorkspaceSwitcher";
import { AccountMenu } from "./AccountMenu";

/**
 * The 56px icon rail. Always visible, never collapses — it is the only
 * navigation element guaranteed to be on screen, so it doubles as the
 * "where am I" indicator.
 *
 * The active marker is a 2px bar on the leading edge rather than a filled
 * icon: at 20px an icon fill is ambiguous, a bar is not.
 */
export function NavRail({ unreadCount }: { unreadCount: number }) {
  const { pathname } = useLocation();
  const { can } = useWorkspace();

  const visible = NAV_SECTIONS.filter(
    (section) => !section.capability || can(section.capability),
  );
  const primary = visible.filter((section) => !section.footer);
  const footer = visible.filter((section) => section.footer);

  return (
    <nav
      aria-label="Primary"
      className="flex w-rail shrink-0 flex-col items-center gap-1 rounded-[var(--hc-float-radius)] border-r border-line bg-sunken py-2 shadow-[var(--hc-float-shadow)] transition-[border-radius,box-shadow,width] duration-base ease-out"
    >
      <div className="mb-1">
        <WorkspaceSwitcher />
      </div>

      <div className="flex flex-1 flex-col items-center gap-0.5">
        {primary.map((section) => (
          <RailLink
            key={section.id}
            section={section}
            active={pathname.startsWith(section.match)}
            badge={section.id === "inbox" ? unreadCount : undefined}
          />
        ))}
      </div>

      <div className="flex flex-col items-center gap-0.5">
        {footer.map((section) => (
          <RailLink
            key={section.id}
            section={section}
            active={pathname.startsWith(section.match)}
          />
        ))}
        <div className="my-1 h-px w-6 bg-line" aria-hidden="true" />
        <AccountMenu />
      </div>
    </nav>
  );
}

function RailLink({
  section,
  active,
  badge,
}: {
  section: NavSection;
  active: boolean;
  badge?: number;
}) {
  const Icon = section.icon;

  return (
    <Tooltip content={section.label} side="right">
      <NavLink
        to={section.to}
        aria-current={active ? "page" : undefined}
        className={cn(
          "group relative grid size-9 place-items-center rounded-md",
          "transition-colors duration-fast",
          active ? "bg-fill text-fg" : "text-fg-muted hover:bg-fill hover:text-fg-secondary",
        )}
      >
        <span
          aria-hidden="true"
          className={cn(
            "absolute -left-2 h-4 w-0.5 rounded-r-full bg-accent transition-all duration-base ease-out",
            active ? "opacity-100" : "h-1 opacity-0",
          )}
        />
        <Icon aria-hidden="true" className="size-[18px]" />
        {badge != null && badge > 0 && (
          <CountBadge
            count={badge}
            tone="accent"
            className="absolute -right-0.5 -top-0.5 ring-2 ring-sunken"
          />
        )}
      </NavLink>
    </Tooltip>
  );
}
