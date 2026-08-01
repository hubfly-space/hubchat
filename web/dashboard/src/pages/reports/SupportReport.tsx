import { AreaChart, Button, Card, CardBody, EmptyState, Metric, Page, PageBody, PageHeader, Section, SegmentedControl, api, downloadCSV, formatCompact, formatDuration, formatPercent, useQuery } from "@hubchat/shared";
import { Download, Inbox } from "lucide-react";
import { useState } from "react";
import { RANGES } from "./ReportsOverview";
import { useAnalyticsRollups } from "../../lib/analytics";

type Summary = { first_response_seconds: number; next_response_seconds: number; resolution_seconds: number; sla_compliance_percent: number; sla_instances: number; backlog_conversations: number; backlog_tickets: number; tickets_reopened: number; active_sla_instances: number; open_sla_breached: number };
type WorkloadRow = { subject_type: "member" | "team"; subject_id: string; name: string; active_conversations: number; active_tickets: number; replies_sent: number; resolved: number };

/** Support operations report backed by live event and SLA APIs. */
export default function SupportReport() {
  const [range, setRange] = useState("30d");
  const conversations = useAnalyticsRollups(["support-report", "conversations", range], "conversations.created", fromFor(range));
  const tickets = useAnalyticsRollups(["support-report", "tickets", range], "tickets.created", fromFor(range));
  const summary = useQuery<Summary>(["support-report", "summary", range], (signal) => api.get(`/analytics/summary?from=${encodeURIComponent(fromFor(range))}`, { signal }));
  const workload = useQuery<{ data: WorkloadRow[] }>(["support-report", "workload", range], (signal) => api.get(`/analytics/workload?from=${encodeURIComponent(fromFor(range))}`, { signal }));
  const conversationPoints = conversations.items.map((item) => ({ t: item.bucket, v: item.value }));
  const breaches = summary.data?.open_sla_breached ?? 0;
  const active = summary.data?.active_sla_instances ?? 0;
  const exportReport = () => downloadCSV(`hubchat-support-${range}.csv`, ["metric", "bucket", "value"], [
    ...conversations.items.map((item) => ["conversations.created", item.bucket, item.value] as (string | number | null)[]),
    ...tickets.items.map((item) => ["tickets.created", item.bucket, item.value] as (string | number | null)[]),
    ["summary.first_response_seconds", "", summary.data?.first_response_seconds ?? null],
    ["summary.next_response_seconds", "", summary.data?.next_response_seconds ?? null],
    ["summary.resolution_seconds", "", summary.data?.resolution_seconds ?? null],
    ["summary.sla_compliance_percent", "", summary.data?.sla_compliance_percent ?? null],
    ...(workload.data?.data ?? []).flatMap((item) => [
      [`${item.subject_type}.active_conversations`, item.name, item.active_conversations],
      [`${item.subject_type}.active_tickets`, item.name, item.active_tickets],
      [`${item.subject_type}.replies_sent`, item.name, item.replies_sent],
      [`${item.subject_type}.resolved`, item.name, item.resolved],
    ]),
  ]);

  return <Page><PageHeader title="Support operations" description="Live volume, queue health, and SLA outcomes." actions={<><SegmentedControl aria-label="Date range" value={range} onValueChange={setRange} options={RANGES.map((item) => ({ value: item.value, label: item.label }))} /><Button variant="secondary" size="sm" leading={<Download />} disabled={!conversations.items.length && !tickets.items.length} onClick={exportReport}>Export CSV</Button></>} /><PageBody>
    {conversations.isError || tickets.isError || summary.isError || workload.isError ? <EmptyState icon={Inbox} title="Support data unavailable" description="Could not load report rollups." action={<Button variant="secondary" onClick={() => { conversations.refetch(); tickets.refetch(); summary.refetch(); workload.refetch(); }}>Try again</Button>} /> : <>
      <Section title="Headline"><Card><CardBody className="grid gap-6 sm:grid-cols-2 xl:grid-cols-4"><Metric label="Conversations" value={formatCompact(sum(conversationPoints))} definition="Conversations created in the selected period." /><Metric label="Tickets" value={formatCompact(sum(tickets.items.map((item) => ({ v: item.value }))))} definition="Tickets created in the selected period." /><Metric label="First response" value={summary.data?.first_response_seconds ? formatDuration(summary.data.first_response_seconds) : "—"} definition="Average elapsed time from first customer message to first agent reply." /><Metric label="Next response" value={summary.data?.next_response_seconds ? formatDuration(summary.data.next_response_seconds) : "—"} definition="Average elapsed time from each customer reply to the next agent reply." /><Metric label="SLA compliance" value={summary.data && summary.data.sla_instances > 0 ? formatPercent(summary.data.sla_compliance_percent / 100) : "—"} definition="Satisfied SLA instances divided by met or breached instances." /><Metric label="Backlog" value={summary.data ? formatCompact(summary.data.backlog_conversations + summary.data.backlog_tickets) : "—"} definition="Open conversations and tickets at the end of the period." /><Metric label="Reopened tickets" value={summary.data ? formatCompact(summary.data.tickets_reopened) : "—"} definition="Tickets moved from resolved or closed back to an active state." /><Metric label="Active SLA timers" value={formatCompact(active)} definition="Timers currently running across this workspace." /><Metric label="Breached timers" value={formatCompact(breaches)} definition="Timers that have crossed their business-hours deadline." /></CardBody></Card></Section>
      <Section title="Conversation volume"><Card><CardBody>{conversationPoints.length ? <AreaChart height={220} series={[{ key: "conversations", label: "Conversations", points: conversationPoints, tone: 1 }]} formatLabel={(label) => label.slice(5)} /> : <EmptyState icon={Inbox} title="No conversation events yet" description="The chart will populate after the analytics worker folds events." />}</CardBody></Card></Section>
      <Section title="Agent and team workload"><Card><CardBody>{workload.isLoading ? <p className="text-sm text-fg-muted">Loading workload…</p> : workload.data?.data.length ? <div className="overflow-x-auto"><table className="w-full min-w-[680px] text-left text-sm"><thead className="border-b border-line text-xs text-fg-muted"><tr><th className="px-3 py-2 font-medium">Owner</th><th className="px-3 py-2 text-right font-medium">Active conversations</th><th className="px-3 py-2 text-right font-medium">Active tickets</th><th className="px-3 py-2 text-right font-medium">Replies</th><th className="px-3 py-2 text-right font-medium">Resolved</th></tr></thead><tbody className="divide-y divide-line-subtle">{workload.data.data.map((item) => <tr key={`${item.subject_type}-${item.subject_id}`}><td className="px-3 py-2 text-fg"><span>{item.name}</span><span className="ml-2 text-xs text-fg-muted">{item.subject_type}</span></td><td className="px-3 py-2 text-right tabular text-fg-secondary">{item.active_conversations.toLocaleString()}</td><td className="px-3 py-2 text-right tabular text-fg-secondary">{item.active_tickets.toLocaleString()}</td><td className="px-3 py-2 text-right tabular text-fg-secondary">{item.replies_sent.toLocaleString()}</td><td className="px-3 py-2 text-right tabular text-fg-secondary">{item.resolved.toLocaleString()}</td></tr>)}</tbody></table></div> : <EmptyState icon={Inbox} title="No assigned workload yet" description="Agent and team rows appear after members, teams, or assigned work are created." />}</CardBody></Card></Section>
    </>}
  </PageBody></Page>;
}

function fromFor(range: string) { const days = range === "7d" ? 7 : range === "90d" ? 90 : 30; return new Date(Date.now() - days * 86400000).toISOString(); }
function sum(points: { v: number }[]) { return points.reduce((total, point) => total + point.v, 0); }
