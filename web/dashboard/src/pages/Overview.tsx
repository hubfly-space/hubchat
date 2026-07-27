import {
  AreaChart,
  Avatar,
  BarChart,
  Badge,
  Button,
  Card,
  CardBody,
  CardHeader,
  ConversationStateBadge,
  DonutChart,
  Metric,
  Page,
  PageBody,
  PageHeader,
  Section,
  SegmentedControl,
  SlaBadge,
  formatCompact,
  formatDuration,
  formatPercent,
  formatRelativeShort,
} from "@hubchat/shared";
import { ArrowRight, Inbox, Timer, TrendingUp, Users } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { useWorkspace } from "../app/workspace-context";
import { NOW, analytics, conversations, inboxes, members } from "../data/fixtures";

type Range = "24h" | "7d" | "30d";

/**
 * Workspace overview — the "what needs me right now" screen.
 *
 * Ordered by urgency, not by data availability: queue health first (something
 * an agent can act on in the next minute), then trend, then composition. A
 * dashboard that opens with a 30-day chart teaches people to skip it.
 */
export default function Overview() {
  const { workspace, viewer, customerById } = useWorkspace();
  const [range, setRange] = useState<Range>("7d");

  const days = range === "24h" ? 1 : range === "7d" ? 7 : 30;
  const slice = <T,>(points: T[]) => points.slice(-days);

  const needsAttention = conversations
    .filter(
      (conversation) =>
        conversation.sla?.state === "breached" ||
        conversation.sla?.state === "approaching" ||
        (conversation.assignee_id === null && conversation.state !== "closed"),
    )
    .slice(0, 5);

  const unassigned = conversations.filter(
    (conversation) => conversation.assignee_id === null && conversation.state !== "closed",
  ).length;
  const breached = conversations.filter(
    (conversation) => conversation.sla?.state === "breached",
  ).length;

  return (
    <Page>
      <PageHeader
        title={`Good afternoon, ${viewer.name.split(" ")[0]}`}
        description={`Here is how ${workspace.name} is doing.`}
        actions={
          <SegmentedControl
            aria-label="Date range"
            value={range}
            onValueChange={setRange}
            options={[
              { value: "24h", label: "24h" },
              { value: "7d", label: "7d" },
              { value: "30d", label: "30d" },
            ]}
          />
        }
      />

      <PageBody>
        {/* Queue health --------------------------------------------------- */}
        <Section title="Queue health">
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <StatCard
              to="/inbox/unassigned"
              icon={<Inbox />}
              label="Unassigned"
              value={unassigned}
              tone={unassigned > 5 ? "warning" : "neutral"}
              hint="Waiting for an owner"
            />
            <StatCard
              to="/inbox/breached"
              icon={<Timer />}
              label="Breached SLA"
              value={breached}
              tone={breached > 0 ? "danger" : "success"}
              hint="Past their response target"
            />
            <StatCard
              to="/inbox/all"
              icon={<TrendingUp />}
              label="Open conversations"
              value={inboxes.reduce((sum, inbox) => sum + inbox.open_count, 0)}
              tone="neutral"
              hint="Across every inbox"
            />
            <StatCard
              to="/settings/members"
              icon={<Users />}
              label="Agents online"
              value={members.filter((member) => member.presence === "online").length}
              tone="neutral"
              hint={`${members.filter((m) => m.accepting_conversations).length} accepting work`}
            />
          </div>
        </Section>

        {/* Needs attention ------------------------------------------------ */}
        <Section
          title="Needs attention"
          description="Unassigned threads and anything at risk of breaching."
          actions={
            <Button variant="ghost" size="sm" trailing={<ArrowRight />} asChild>
              <Link to="/inbox/unassigned">Open inbox</Link>
            </Button>
          }
        >
          <Card>
            <ul>
              {needsAttention.map((conversation) => {
                const customer = customerById(conversation.customer_id);
                const remaining =
                  conversation.sla?.next_response_remaining ??
                  conversation.sla?.first_response_remaining ??
                  null;

                return (
                  <li key={conversation.id} className="border-b border-line-subtle last:border-b-0">
                    <Link
                      to={`/inbox/all/${conversation.id}`}
                      className="flex items-center gap-3 px-4 py-2.5 transition-colors hover:bg-surface-hover"
                    >
                      <Avatar name={customer?.name} seed={customer?.id ?? conversation.id} size="sm" />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm text-fg">
                          {conversation.subject ?? conversation.last_message_preview}
                        </span>
                        <span className="block truncate text-xs text-fg-muted">
                          {customer?.name ?? "Anonymous visitor"} ·{" "}
                          {formatRelativeShort(conversation.last_message_at, NOW)} ago
                        </span>
                      </span>
                      <ConversationStateBadge state={conversation.state} />
                      {conversation.sla && (
                        <SlaBadge
                          state={conversation.sla.state}
                          remaining={remaining != null ? formatDuration(remaining) : undefined}
                        />
                      )}
                    </Link>
                  </li>
                );
              })}
            </ul>
          </Card>
        </Section>

        {/* Trends ---------------------------------------------------------- */}
        <Section title="Trends">
          <div className="grid gap-3 lg:grid-cols-3">
            <Card className="lg:col-span-2">
              <CardHeader title="Conversation volume" description="New conversations per day" />
              <CardBody>
                <div className="mb-4 grid grid-cols-2 gap-6 sm:grid-cols-4">
                  <Metric
                    label="Conversations"
                    value={formatCompact(slice(analytics.conversations).reduce((s, p) => s + p.v, 0))}
                    delta={0.082}
                    definition="Conversations created in the selected period, across every channel."
                  />
                  <Metric
                    label="First response"
                    value={formatDuration(analytics.firstResponse.at(-1)?.v ?? 0)}
                    delta={-0.114}
                    higherIsBetter={false}
                    definition="Median time from the first customer message to the first public agent reply, counted in business hours."
                  />
                  <Metric
                    label="Resolution"
                    value={formatDuration(analytics.resolution.at(-1)?.v ?? 0)}
                    delta={-0.037}
                    higherIsBetter={false}
                    definition="Median time from creation to the resolved state, counted in business hours."
                  />
                  <Metric
                    label="CSAT"
                    value={formatPercent((analytics.csat.at(-1)?.v ?? 0) / 100, 0)}
                    delta={0.015}
                    definition="Share of satisfaction responses rating 4 or 5 stars."
                  />
                </div>

                <AreaChart
                  height={180}
                  series={[
                    { key: "conversations", label: "Conversations", points: slice(analytics.conversations), tone: 1 },
                    { key: "tickets", label: "Tickets", points: slice(analytics.tickets), tone: 3 },
                  ]}
                  formatLabel={(label) => label.slice(5)}
                />
              </CardBody>
            </Card>

            <Card>
              <CardHeader title="Where contacts arrive" description="Last 30 days" />
              <CardBody>
                <DonutChart
                  segments={analytics.channelSplit}
                  centerValue={formatCompact(
                    analytics.channelSplit.reduce((sum, segment) => sum + segment.value, 0),
                  )}
                  centerLabel="contacts"
                />
              </CardBody>
            </Card>
          </div>
        </Section>

        <Section title="Team">
          <div className="grid gap-3 lg:grid-cols-2">
            <Card>
              <CardHeader title="Agent workload" description="Conversations handled in the period" />
              <CardBody>
                <BarChart horizontal points={analytics.agentWorkload} />
              </CardBody>
            </Card>

            <Card>
              <CardHeader
                title="Inboxes"
                description="Open volume by destination"
                actions={
                  <Button variant="ghost" size="xs" asChild>
                    <Link to="/channels/inboxes">Manage</Link>
                  </Button>
                }
              />
              <CardBody className="p-0">
                <ul>
                  {inboxes.map((inbox) => (
                    <li
                      key={inbox.id}
                      className="flex items-center gap-3 border-b border-line-subtle px-4 py-2.5 last:border-b-0"
                    >
                      <span className="min-w-0 flex-1">
                        <span className="flex items-center gap-2">
                          <span className="truncate text-sm text-fg">{inbox.name}</span>
                          {inbox.is_default && <Badge tone="neutral">Default</Badge>}
                        </span>
                        <span className="block truncate text-xs text-fg-muted">
                          {inbox.channels.join(" · ")}
                        </span>
                      </span>
                      <span className="text-sm font-medium tabular text-fg">{inbox.open_count}</span>
                    </li>
                  ))}
                </ul>
              </CardBody>
            </Card>
          </div>
        </Section>
      </PageBody>
    </Page>
  );
}

function StatCard({
  to,
  icon,
  label,
  value,
  hint,
  tone,
}: {
  to: string;
  icon: React.ReactNode;
  label: string;
  value: number;
  hint: string;
  tone: "neutral" | "warning" | "danger" | "success";
}) {
  return (
    <Card interactive className="p-0">
      <Link to={to} className="block p-4">
        <div className="flex items-start justify-between gap-2">
          <span className="text-xs text-fg-muted">{label}</span>
          <span
            className={
              tone === "danger"
                ? "text-danger-text [&_svg]:size-3.5"
                : tone === "warning"
                  ? "text-warning-text [&_svg]:size-3.5"
                  : tone === "success"
                    ? "text-success-text [&_svg]:size-3.5"
                    : "text-fg-muted [&_svg]:size-3.5"
            }
          >
            {icon}
          </span>
        </div>
        <p className="mt-2 text-2xl font-semibold tabular tracking-tighter text-fg">{value}</p>
        <p className="mt-0.5 text-2xs text-fg-muted">{hint}</p>
      </Link>
    </Card>
  );
}
