import { api, Button, Eyebrow, Tooltip, useQuery, type Inbox } from "@hubchat/shared";
import {
  BellRing,
  CheckCircle2,
  Clock,
  Hourglass,
  Inbox as InboxIcon,
  Plus,
  UserCheck,
  Users,
} from "lucide-react";
import { useLocation, useNavigate } from "react-router-dom";
import { SidebarItem } from "../../app/shell/SectionSidebar";

type Counts = {
  all: number;
  unassigned: number;
  mine: number;
  following: number;
  waiting_on_us: number;
  waiting_on_customer: number;
  snoozed: number;
  resolved: number;
  spam: number;
};

/**
 * The inbox's own sidebar, substituted for the generic SectionSidebar.
 *
 * Counts arrive as one aggregate call (`GET /v1/conversations/counts`)
 * rather than one query per badge. Mentions, breached/approaching SLA, and
 * saved views from the original design have no backend yet (there is no
 * @mention concept anywhere in the schema, SLA is Stage 8, saved filter sets
 * are Stage 3) and are left out rather than shown as permanently-empty
 * buttons.
 */
export function InboxSidebar() {
  const { pathname } = useLocation();
  const navigate = useNavigate();

  const counts = useQuery<Counts>(["conversation-counts"], (signal) => api.get("/conversations/counts", { signal }));
  const inboxes = useQuery<{ data: Inbox[] }>(["inboxes"], (signal) => api.get("/inboxes", { signal }));

  const activeView = pathname.split("/")[2] ?? "all";
  const c = counts.data;

  return (
    <nav
      aria-label="Inbox views"
      className="flex w-sidebar shrink-0 flex-col overflow-y-auto rounded-[var(--hc-float-radius)] border-r border-line bg-surface shadow-[var(--hc-float-shadow)] transition-[border-radius,box-shadow,width] duration-base ease-out"
    >
      <div className="sticky top-0 z-[var(--z-sticky)] flex items-center justify-between border-b border-line bg-surface px-3 py-2">
        <h2 className="text-sm font-semibold tracking-tight text-fg">Inbox</h2>
        <Tooltip content="New inbox">
          <Button
            variant="ghost"
            size="xs"
            iconOnly
            aria-label="New inbox"
            leading={<Plus />}
            onClick={() => navigate("/channels/inboxes")}
          />
        </Tooltip>
      </div>

      <div className="flex flex-col gap-[var(--hc-sidebar-group-gap)] p-[var(--hc-sidebar-pad)]">
        <ul className="flex flex-col gap-px">
          <li>
            <SidebarItem
              to="/inbox/all"
              label="All active"
              icon={<InboxIcon />}
              count={c?.all ?? 0}
              active={activeView === "all"}
            />
          </li>
          <li>
            <SidebarItem
              to="/inbox/unassigned"
              label="Unassigned"
              icon={<Users />}
              count={c?.unassigned ?? 0}
              active={activeView === "unassigned"}
              accent
            />
          </li>
          <li>
            <SidebarItem
              to="/inbox/mine"
              label="Assigned to me"
              icon={<UserCheck />}
              count={c?.mine ?? 0}
              active={activeView === "mine"}
            />
          </li>
          <li>
            <SidebarItem
              to="/inbox/following"
              label="Following"
              icon={<BellRing />}
              count={c?.following ?? 0}
              active={activeView === "following"}
            />
          </li>
        </ul>

        <div>
          <Eyebrow className="px-2 pb-1.5">Status</Eyebrow>
          <ul className="flex flex-col gap-px">
            <li>
              <SidebarItem
                to="/inbox/waiting-support"
                label="Waiting on us"
                icon={<Hourglass />}
                count={c?.waiting_on_us ?? 0}
                active={activeView === "waiting-support"}
              />
            </li>
            <li>
              <SidebarItem
                to="/inbox/waiting-customer"
                label="Waiting on customer"
                icon={<Clock />}
                count={c?.waiting_on_customer ?? 0}
                active={activeView === "waiting-customer"}
              />
            </li>
            <li>
              <SidebarItem
                to="/inbox/snoozed"
                label="Snoozed"
                icon={<Clock />}
                count={c?.snoozed ?? 0}
                active={activeView === "snoozed"}
              />
            </li>
            <li>
              <SidebarItem
                to="/inbox/resolved"
                label="Resolved"
                icon={<CheckCircle2 />}
                count={c?.resolved ?? 0}
                active={activeView === "resolved"}
              />
            </li>
          </ul>
        </div>

        <div>
          <Eyebrow className="px-2 pb-1.5">Inboxes</Eyebrow>
          <ul className="flex flex-col gap-px">
            {(inboxes.data?.data ?? []).map((inbox) => (
              <li key={inbox.id}>
                <SidebarItem
                  to={`/inbox/${inbox.slug}`}
                  label={inbox.name}
                  icon={<InboxIcon />}
                  count={inbox.open_count}
                  active={activeView === inbox.slug}
                />
              </li>
            ))}
          </ul>
        </div>
      </div>
    </nav>
  );
}
