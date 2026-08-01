import {
  api,
  Button,
  Dialog,
  DialogClose,
  DialogContent,
  Eyebrow,
  Field,
  Input,
  Select,
  Tooltip,
  idempotencyKey,
  useAllPages,
  useInfinite,
  useMutation,
  useQuery,
  type Inbox,
  type Paginated,
  type SavedView,
} from "@hubchat/shared";
import {
  Bookmark,
  BellRing,
  CheckCircle2,
  Clock,
  Hourglass,
  Inbox as InboxIcon,
  Plus,
  UserCheck,
  Users,
} from "lucide-react";
import { useState } from "react";
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
 * Mentions and breached/approaching SLA shortcuts remain outside this sidebar
 * until their dedicated notification queues exist. Saved views are live API
 * resources and are rendered only when the server returns them.
 */
export function InboxSidebar() {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const [createOpen, setCreateOpen] = useState(false);
  const [name, setName] = useState("");
  const [scope, setScope] = useState<"personal" | "workspace">("personal");
  const [state, setState] = useState("");

  const counts = useQuery<Counts>(["conversation-counts"], (signal) => api.get("/conversations/counts", { signal }));
  const inboxes = useAllPages<Inbox>(["inboxes", "lookup"], (cursor, signal) => api.get<Paginated<Inbox>>(`/inboxes?limit=200${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }));
  const savedViews = useInfinite<SavedView>(["saved-views", "conversation"], (cursor, signal) => {
    const params = new URLSearchParams({ entity_type: "conversation", limit: "50" });
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<SavedView>>(`/saved-views?${params.toString()}`, { signal });
  });
  const create = useMutation<{ name: string; entity_type: string; scope: string; filters: Record<string, unknown>; sort: Record<string, unknown> }, SavedView>(
    (input) => api.post("/saved-views", input, { idempotencyKey: idempotencyKey() }),
    {
      invalidates: [["saved-views", "conversation"]],
      onSuccess: () => {
        setCreateOpen(false);
        setName("");
        setScope("personal");
        setState("");
      },
    },
  );

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
          <div className="flex items-center justify-between px-2 pb-1.5">
            <Eyebrow>Saved views</Eyebrow>
            <Tooltip content="New saved view">
              <Button variant="ghost" size="xs" iconOnly aria-label="New saved view" leading={<Plus />} onClick={() => setCreateOpen(true)} />
            </Tooltip>
          </div>
          <ul className="flex flex-col gap-px">
            {savedViews.items.map((view) => (
              <li key={view.id}>
                <SidebarItem to={`/inbox/${view.id}`} label={view.name} icon={<Bookmark />} active={activeView === view.id} />
              </li>
            ))}
            {!savedViews.isLoading && savedViews.items.length === 0 && <li className="px-2 text-2xs text-fg-disabled">Save a filter for quick access.</li>}
          </ul>
        </div>

        <div>
          <Eyebrow className="px-2 pb-1.5">Inboxes</Eyebrow>
          <ul className="flex flex-col gap-px">
            {inboxes.items.map((inbox) => (
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

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent
          title="New saved view"
          description="Save a named conversation filter for yourself or the whole workspace."
          footer={<><DialogClose asChild><Button variant="ghost" size="sm">Cancel</Button></DialogClose><Button variant="primary" size="sm" loading={create.isPending} disabled={!name.trim()} onClick={() => void create.mutate({ name: name.trim(), entity_type: "conversation", scope, filters: { match: "all", conditions: state ? [{ field: "state", operator: "is", value: state }] : [] }, sort: { field: "last_message_at", direction: "desc" } }).catch(() => {})}>Create view</Button></>}
        >
          <div className="space-y-4 pb-2">
            <Field label="Name" required><Input autoFocus value={name} onChange={(event) => setName(event.target.value)} placeholder="Urgent conversations" /></Field>
            <Field label="Visibility"><Select value={scope} onValueChange={(value) => setScope(value as "personal" | "workspace")} options={[{ value: "personal", label: "Only me" }, { value: "workspace", label: "Everyone in the workspace" }]} /></Field>
            <Field label="State" description="Leave empty to include all active states."><Select value={state} onValueChange={setState} options={[{ value: "", label: "All active" }, { value: "new", label: "New" }, { value: "open", label: "Open" }, { value: "pending", label: "Pending" }, { value: "waiting_for_support", label: "Waiting on us" }, { value: "waiting_for_customer", label: "Waiting on customer" }, { value: "resolved", label: "Resolved" }]} /></Field>
            {Boolean(create.error) && <p className="text-sm text-danger">Could not create this saved view. Try again.</p>}
          </div>
        </DialogContent>
      </Dialog>
    </nav>
  );
}
