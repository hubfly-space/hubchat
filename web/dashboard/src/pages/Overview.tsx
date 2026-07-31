import {
  ApiError,
  Avatar,
  Badge,
  Button,
  Card,
  ConversationStateBadge,
  EmptyState,
  Page,
  PageBody,
  PageHeader,
  Section,
  formatRelativeShort,
  api,
  useQuery,
  type Conversation,
  type Paginated,
} from "@hubchat/shared";
import { ArrowRight, Inbox, Timer, Users } from "lucide-react";
import { Link } from "react-router-dom";
import { useWorkspace } from "../app/workspace-context";

type Counts = {
  all: number;
  unassigned: number;
  resolved: number;
};

/** Workspace overview backed by the same live APIs as the inbox. */
export default function Overview() {
  const { workspace, members, inboxes, memberById } = useWorkspace();
  const counts = useQuery<Counts>(["overview", "counts"], (signal) => api.get("/conversations/counts", { signal }));
  const conversations = useQuery<Paginated<Conversation>>(
    ["overview", "attention"],
    (signal) => api.get("/conversations?limit=50&state=new,open,pending,waiting_for_customer,waiting_for_support", { signal }),
  );

  const rows = conversations.data?.data ?? [];
  const needsAttention = rows.filter((item) => item.assignee_id === null || item.sla?.state === "breached" || item.sla?.state === "approaching").slice(0, 8);
  const breached = rows.filter((item) => item.sla?.state === "breached").length;
  const online = members.filter((member) => member.presence === "online").length;
  const openByInbox = new Map(inboxes.map((inbox) => [inbox.id, inbox.open_count]));

  return (
    <Page>
      <PageHeader title={`Good afternoon, ${workspace.name}`} description="Live queue health and the conversations that need attention." />
      <PageBody>
        <Section title="Queue health">
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <StatCard to="/inbox/unassigned" icon={<Inbox />} label="Unassigned" value={counts.data?.unassigned ?? 0} hint="Waiting for an owner" tone={(counts.data?.unassigned ?? 0) > 0 ? "warning" : "neutral"} />
            <StatCard to="/inbox/breached" icon={<Timer />} label="Breached SLA" value={breached} hint="From the live attention window" tone={breached > 0 ? "danger" : "success"} />
            <StatCard to="/inbox/all" icon={<Inbox />} label="Open conversations" value={counts.data?.all ?? 0} hint="Across every inbox" tone="neutral" />
            <StatCard to="/settings/members" icon={<Users />} label="Agents online" value={online} hint={`${members.filter((member) => member.accepting_conversations).length} accepting work`} tone="neutral" />
          </div>
        </Section>

        <Section title="Needs attention" description="Unassigned threads and conversations approaching or past their SLA target." actions={<Button variant="ghost" size="sm" trailing={<ArrowRight />} asChild><Link to="/inbox/unassigned">Open inbox</Link></Button>}>
          {conversations.isLoading ? <p className="text-sm text-fg-muted">Loading live conversations…</p> : conversations.isError ? <EmptyState icon={Inbox} title="Queue unavailable" description={conversations.error instanceof ApiError ? conversations.error.message : "Try again in a moment."} action={<Button variant="secondary" size="sm" onClick={conversations.refetch}>Try again</Button>} /> : needsAttention.length === 0 ? <EmptyState icon={Inbox} title="Nothing needs attention" description="The live queue has no unassigned or at-risk conversations." /> : <Card><ul>{needsAttention.map((conversation) => { const member = conversation.assignee_id ? memberById(conversation.assignee_id) : undefined; return <li key={conversation.id} className="border-b border-line-subtle last:border-b-0"><Link to={`/inbox/all/${conversation.id}`} className="flex items-center gap-3 px-4 py-2.5 transition-colors hover:bg-surface-hover"><Avatar name={member?.name ?? "Anonymous visitor"} seed={conversation.customer_id ?? conversation.id} size="sm" /><span className="min-w-0 flex-1"><span className="block truncate text-sm text-fg">{conversation.subject ?? conversation.last_message_preview}</span><span className="block truncate text-xs text-fg-muted">{member?.name ?? "Unassigned"} · {formatRelativeShort(conversation.last_message_at)} ago</span></span><ConversationStateBadge state={conversation.state} />{conversation.sla && <Badge tone={conversation.sla.state === "breached" ? "danger" : conversation.sla.state === "approaching" ? "warning" : "neutral"}>{conversation.sla.state}</Badge>}</Link></li>; })}</ul></Card>}
        </Section>

        <Section title="Inboxes" description="Current open volume by destination." actions={<Button variant="ghost" size="xs" asChild><Link to="/channels/inboxes">Manage</Link></Button>}>
          <Card><ul>{inboxes.map((inbox) => <li key={inbox.id} className="flex items-center gap-3 border-b border-line-subtle px-4 py-2.5 last:border-b-0"><span className="min-w-0 flex-1"><span className="flex items-center gap-2"><span className="truncate text-sm text-fg">{inbox.name}</span>{inbox.is_default && <Badge tone="neutral">Default</Badge>}</span><span className="block truncate text-xs text-fg-muted">{inbox.channels.join(" · ")}</span></span><span className="text-sm font-medium tabular text-fg">{openByInbox.get(inbox.id) ?? 0}</span></li>)}</ul></Card>
        </Section>
      </PageBody>
    </Page>
  );
}

function StatCard({ to, icon, label, value, hint, tone }: { to: string; icon: React.ReactNode; label: string; value: number; hint: string; tone: "neutral" | "warning" | "danger" | "success" }) {
  return <Card interactive className="p-0"><Link to={to} className="block p-4"><div className="flex items-start justify-between gap-2"><span className="text-xs text-fg-muted">{label}</span><span className={tone === "danger" ? "text-danger-text [&_svg]:size-3.5" : tone === "warning" ? "text-warning-text [&_svg]:size-3.5" : tone === "success" ? "text-success-text [&_svg]:size-3.5" : "text-fg-muted [&_svg]:size-3.5"}>{icon}</span></div><p className="mt-2 text-2xl font-semibold tabular tracking-tighter text-fg">{value}</p><p className="mt-0.5 text-2xs text-fg-muted">{hint}</p></Link></Card>;
}
