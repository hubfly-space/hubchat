import { Button, Eyebrow, Tooltip } from "@hubchat/shared";
import {
  AlertTriangle,
  AtSign,
  Ban,
  BellRing,
  CheckCircle2,
  Clock,
  Filter,
  Hourglass,
  Inbox,
  Plus,
  Timer,
  UserCheck,
  Users,
} from "lucide-react";
import { useLocation } from "react-router-dom";
import { SidebarItem } from "../../app/shell/SectionSidebar";
import { conversations, inboxes, savedViews } from "../../data/fixtures";
import { useWorkspace } from "../../app/workspace-context";

/**
 * The inbox's own sidebar, substituted for the generic SectionSidebar.
 *
 * View counts are computed here from the loaded page for the fixtures build.
 * Against the real API they arrive as a single aggregate call
 * (`GET /api/v1/inbox/counts`) — computing them client-side would mean loading
 * every conversation just to render a number, which is exactly the pattern
 * §17 warns about.
 */
export function InboxSidebar() {
  const { pathname } = useLocation();
  const { viewer } = useWorkspace();

  const count = (predicate: (conversation: (typeof conversations)[number]) => boolean) =>
    conversations.filter(predicate).length;

  const activeView = pathname.split("/")[2] ?? "all";

  return (
    <nav
      aria-label="Inbox views"
      className="flex w-sidebar shrink-0 flex-col overflow-y-auto rounded-[var(--hc-float-radius)] border-r border-line bg-surface shadow-[var(--hc-float-shadow)] transition-[border-radius,box-shadow,width] duration-base ease-out"
    >
      <div className="sticky top-0 z-[var(--z-sticky)] flex items-center justify-between border-b border-line bg-surface px-3 py-2">
        <h2 className="text-sm font-semibold tracking-tight text-fg">Inbox</h2>
        <Tooltip content="New saved view" shortcut="mod+shift+v">
          <Button variant="ghost" size="xs" iconOnly aria-label="New saved view" leading={<Plus />} />
        </Tooltip>
      </div>

      <div className="flex flex-col gap-[var(--hc-sidebar-group-gap)] p-[var(--hc-sidebar-pad)]">
        <ul className="flex flex-col gap-px">
          <li>
            <SidebarItem
              to="/inbox/all"
              label="All active"
              icon={<Inbox />}
              count={count((c) => !["closed", "resolved", "spam"].includes(c.state))}
              active={activeView === "all"}
            />
          </li>
          <li>
            <SidebarItem
              to="/inbox/unassigned"
              label="Unassigned"
              icon={<Users />}
              count={count((c) => c.assignee_id === null && c.state !== "closed")}
              active={activeView === "unassigned"}
              accent
            />
          </li>
          <li>
            <SidebarItem
              to="/inbox/mine"
              label="Assigned to me"
              icon={<UserCheck />}
              count={count((c) => c.assignee_id === viewer.id)}
              active={activeView === "mine"}
            />
          </li>
          <li>
            <SidebarItem
              to="/inbox/mentions"
              label="Mentions"
              icon={<AtSign />}
              count={1}
              active={activeView === "mentions"}
            />
          </li>
          <li>
            <SidebarItem
              to="/inbox/following"
              label="Following"
              icon={<BellRing />}
              count={3}
              active={activeView === "following"}
            />
          </li>
        </ul>

        <div>
          <Eyebrow className="px-2 pb-1.5">Service level</Eyebrow>
          <ul className="flex flex-col gap-px">
            <li>
              <SidebarItem
                to="/inbox/breached"
                label="Breached SLA"
                icon={<AlertTriangle />}
                count={count((c) => c.sla?.state === "breached")}
                active={activeView === "breached"}
                accent
              />
            </li>
            <li>
              <SidebarItem
                to="/inbox/approaching"
                label="Approaching SLA"
                icon={<Timer />}
                count={count((c) => c.sla?.state === "approaching")}
                active={activeView === "approaching"}
              />
            </li>
          </ul>
        </div>

        <div>
          <Eyebrow className="px-2 pb-1.5">Status</Eyebrow>
          <ul className="flex flex-col gap-px">
            <li>
              <SidebarItem
                to="/inbox/waiting-support"
                label="Waiting on us"
                icon={<Hourglass />}
                count={count((c) => c.state === "waiting_for_support")}
                active={activeView === "waiting-support"}
              />
            </li>
            <li>
              <SidebarItem
                to="/inbox/waiting-customer"
                label="Waiting on customer"
                icon={<Clock />}
                count={count((c) => c.state === "waiting_for_customer" || c.state === "pending")}
                active={activeView === "waiting-customer"}
              />
            </li>
            <li>
              <SidebarItem
                to="/inbox/snoozed"
                label="Snoozed"
                icon={<Clock />}
                count={count((c) => c.state === "snoozed")}
                active={activeView === "snoozed"}
              />
            </li>
            <li>
              <SidebarItem
                to="/inbox/resolved"
                label="Resolved"
                icon={<CheckCircle2 />}
                count={count((c) => c.state === "resolved")}
                active={activeView === "resolved"}
              />
            </li>
            <li>
              <SidebarItem
                to="/inbox/spam"
                label="Spam"
                icon={<Ban />}
                count={count((c) => c.state === "spam")}
                active={activeView === "spam"}
              />
            </li>
          </ul>
        </div>

        <div>
          <Eyebrow className="px-2 pb-1.5">Inboxes</Eyebrow>
          <ul className="flex flex-col gap-px">
            {inboxes.map((inbox) => (
              <li key={inbox.id}>
                <SidebarItem
                  to={`/inbox/${inbox.slug}`}
                  label={inbox.name}
                  icon={<Inbox />}
                  count={inbox.open_count}
                  active={activeView === inbox.slug}
                />
              </li>
            ))}
          </ul>
        </div>

        <div>
          <Eyebrow className="px-2 pb-1.5">Saved views</Eyebrow>
          <ul className="flex flex-col gap-px">
            {savedViews.map((view) => (
              <li key={view.id}>
                <SidebarItem
                  to={`/inbox/${view.id}`}
                  label={view.name}
                  icon={<Filter />}
                  count={view.count}
                  active={activeView === view.id}
                />
              </li>
            ))}
          </ul>
        </div>
      </div>
    </nav>
  );
}
