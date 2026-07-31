import { AreaChart, Button, Card, CardBody, EmptyState, Metric, Page, PageBody, PageHeader, Section, SegmentedControl, api, formatCompact, formatDuration, formatPercent, useQuery } from "@hubchat/shared";
import { Download, Inbox } from "lucide-react";
import { useState } from "react";
import { RANGES } from "./ReportsOverview";

type Rollup = { bucket: string; value: number };
type Instance = { id: string; state: string; kind: string; target_minutes: number; elapsed_minutes: number };
type Summary = { first_response_seconds: number; resolution_seconds: number; sla_compliance_percent: number; sla_instances: number; backlog_conversations: number; backlog_tickets: number; tickets_reopened: number };

/** Support operations report backed by live event and SLA APIs. */
export default function SupportReport() {
  const [range, setRange] = useState("30d");
  const conversations = useQuery<{ data: Rollup[] }>(["support-report", "conversations", range], (signal) => api.get(`/analytics/rollups?metric=conversations.created&grain=day&from=${encodeURIComponent(fromFor(range))}`, { signal }));
  const tickets = useQuery<{ data: Rollup[] }>(["support-report", "tickets", range], (signal) => api.get(`/analytics/rollups?metric=tickets.created&grain=day&from=${encodeURIComponent(fromFor(range))}`, { signal }));
  const instances = useQuery<{ data: Instance[] }>(["support-report", "sla"], (signal) => api.get("/sla/instances?limit=200", { signal }));
  const summary = useQuery<Summary>(["support-report", "summary", range], (signal) => api.get(`/analytics/summary?from=${encodeURIComponent(fromFor(range))}`, { signal }));
  const conversationPoints = (conversations.data?.data ?? []).map((item) => ({ t: item.bucket, v: item.value }));
  const breaches = (instances.data?.data ?? []).filter((item) => item.state === "breached").length;
  const active = (instances.data?.data ?? []).filter((item) => item.state === "active").length;

  return <Page><PageHeader title="Support operations" description="Live volume, queue health, and SLA outcomes." actions={<><SegmentedControl aria-label="Date range" value={range} onValueChange={setRange} options={RANGES.map((item) => ({ value: item.value, label: item.label }))} /><Button variant="secondary" size="sm" leading={<Download />}>Export</Button></>} /><PageBody>
    {conversations.isError || tickets.isError || summary.isError ? <EmptyState icon={Inbox} title="Support data unavailable" description="Could not load report rollups." action={<Button variant="secondary" onClick={() => { conversations.refetch(); tickets.refetch(); summary.refetch(); }}>Try again</Button>} /> : <>
      <Section title="Headline"><Card><CardBody className="grid gap-6 sm:grid-cols-2 xl:grid-cols-4"><Metric label="Conversations" value={formatCompact(sum(conversationPoints))} definition="Conversations created in the selected period." /><Metric label="Tickets" value={formatCompact(sum((tickets.data?.data ?? []).map((item) => ({ v: item.value })) ))} definition="Tickets created in the selected period." /><Metric label="First response" value={summary.data?.first_response_seconds ? formatDuration(summary.data.first_response_seconds) : "—"} definition="Average elapsed time from first customer message to first agent reply." /><Metric label="SLA compliance" value={summary.data && summary.data.sla_instances > 0 ? formatPercent(summary.data.sla_compliance_percent / 100) : "—"} definition="Satisfied SLA instances divided by met or breached instances." /><Metric label="Backlog" value={summary.data ? formatCompact(summary.data.backlog_conversations + summary.data.backlog_tickets) : "—"} definition="Open conversations and tickets at the end of the period." /><Metric label="Reopened tickets" value={summary.data ? formatCompact(summary.data.tickets_reopened) : "—"} definition="Tickets moved from resolved or closed back to an active state." /><Metric label="Active SLA timers" value={formatCompact(active)} definition="Timers currently running across this workspace." /><Metric label="Breached timers" value={formatCompact(breaches)} definition="Timers that have crossed their business-hours deadline." /></CardBody></Card></Section>
      <Section title="Conversation volume"><Card><CardBody>{conversationPoints.length ? <AreaChart height={220} series={[{ key: "conversations", label: "Conversations", points: conversationPoints, tone: 1 }]} formatLabel={(label) => label.slice(5)} /> : <EmptyState icon={Inbox} title="No conversation events yet" description="The chart will populate after the analytics worker folds events." />}</CardBody></Card></Section>
    </>}
  </PageBody></Page>;
}

function fromFor(range: string) { const days = range === "7d" ? 7 : range === "90d" ? 90 : 30; return new Date(Date.now() - days * 86400000).toISOString(); }
function sum(points: { v: number }[]) { return points.reduce((total, point) => total + point.v, 0); }
