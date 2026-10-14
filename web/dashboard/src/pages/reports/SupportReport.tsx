import { AreaChart, Select, QueryError, Button, Card, CardBody, EmptyState, Metric, Page, PageBody, PageHeader, Section, SegmentedControl, api, downloadCSV, formatCompact, formatDateTime as formatDateTimeValue, formatDuration, formatPercent, useQuery, type FormatOptions } from "@hubchat/shared";
import { Download, Inbox } from "lucide-react";
import { useState } from "react";
import { RANGES } from "./ReportsOverview";
import { latestComputedAt, reportWindow, useAnalyticsRollups } from "../../lib/analytics";
import { useWorkspace } from "../../app/workspace-context";
import { workspaceFormatOptions } from "../../app/workspace-context";

type Summary = { first_response_seconds: number; next_response_seconds: number; resolution_seconds: number; sla_compliance_percent: number; sla_instances: number; backlog_conversations: number; backlog_tickets: number; tickets_reopened: number; active_sla_instances: number; open_sla_breached: number };
type WorkloadRow = { subject_type: "member" | "team"; subject_id: string; name: string; active_conversations: number; active_tickets: number; replies_sent: number; resolved: number };

/** Support operations report backed by live event and SLA APIs. */
export default function SupportReport() {
  const { workspace, inboxes, teams } = useWorkspace();
  const [groupBy,setGroupBy]=useState("inbox");
  const [inboxID,setInboxID]=useState("all");
  const [teamID,setTeamID]=useState("all");
  const dateFormat = workspaceFormatOptions(workspace);
  const formatDateTime = (value: string, options: FormatOptions = {}) => formatDateTimeValue(value, { ...dateFormat, ...options });
  const [range, setRange] = useState("30d");
  const period = reportWindow(range, workspace.timezone);
  const drillParams=new URLSearchParams({from:period.from,to:period.to,timezone:period.timezone,group_by:groupBy});
  if(inboxID!=="all")drillParams.set("inbox_id",inboxID);
  if(teamID!=="all")drillParams.set("team_id",teamID);
  const drilldown=useQuery<{data:{key:string;label:string;current:number;previous:number;first_response_p50:number|null;first_response_p90:number|null;resolution_p50:number|null;resolution_p90:number|null}[]}>(["report-drilldown",drillParams.toString()],signal=>api.get(`/analytics/drilldown?${drillParams}`,{signal}));
  const conversations = useAnalyticsRollups(["support-report", "conversations", range, period.timezone], "conversations.created", period.from, period.to, period.timezone);
  const tickets = useAnalyticsRollups(["support-report", "tickets", range, period.timezone], "tickets.created", period.from, period.to, period.timezone);
  const summary = useQuery<Summary>(["support-report", "summary", range, period.timezone], (signal) => api.get(`/analytics/summary?from=${encodeURIComponent(period.from)}&to=${encodeURIComponent(period.to)}&timezone=${encodeURIComponent(period.timezone)}`, { signal }));
  const workload = useQuery<{ data: WorkloadRow[] }>(["support-report", "workload", range, period.timezone], (signal) => api.get(`/analytics/workload?from=${encodeURIComponent(period.from)}&to=${encodeURIComponent(period.to)}&timezone=${encodeURIComponent(period.timezone)}`, { signal }));
  const conversationPoints = conversations.items.map((item) => ({ t: item.bucket, v: item.value }));
  const breaches = summary.data?.open_sla_breached ?? 0;
  const active = summary.data?.active_sla_instances ?? 0;
  const freshness = latestComputedAt(conversations.items, tickets.items);
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

  return <Page><PageHeader title="Support operations" description={`Live volume, queue health, and SLA outcomes. Reporting timezone: ${period.timezone}.`} actions={<><SegmentedControl aria-label="Date range" value={range} onValueChange={setRange} options={RANGES.map((item) => ({ value: item.value, label: item.label }))} /><Button variant="secondary" size="sm" leading={<Download />} disabled={!conversations.items.length && !tickets.items.length} onClick={exportReport}>Export CSV</Button></>} /><PageBody><p className="mb-5 text-xs text-fg-muted">Rollup data last computed {freshness ? formatDateTime(freshness, { timeZone: period.timezone }) : "not yet"}.</p>
    {conversations.isError || tickets.isError || summary.isError || workload.isError ? <EmptyState icon={Inbox} title="Support data unavailable" description="Could not load report rollups." action={<Button variant="secondary" onClick={() => { conversations.refetch(); tickets.refetch(); summary.refetch(); workload.refetch(); }}>Try again</Button>} /> : <>
      <Section title="Headline"><Card><CardBody className="grid gap-6 sm:grid-cols-2 xl:grid-cols-4"><Metric label="Conversations" value={formatCompact(sum(conversationPoints))} definition="Conversations created in the selected period." /><Metric label="Tickets" value={formatCompact(sum(tickets.items.map((item) => ({ v: item.value }))))} definition="Tickets created in the selected period." /><Metric label="First response" value={summary.data?.first_response_seconds ? formatDuration(summary.data.first_response_seconds) : "—"} definition="Average elapsed time from first customer message to first agent reply." /><Metric label="Next response" value={summary.data?.next_response_seconds ? formatDuration(summary.data.next_response_seconds) : "—"} definition="Average elapsed time from each customer reply to the next agent reply." /><Metric label="SLA compliance" value={summary.data && summary.data.sla_instances > 0 ? formatPercent(summary.data.sla_compliance_percent / 100) : "—"} definition="Satisfied SLA instances divided by met or breached instances." /><Metric label="Backlog" value={summary.data ? formatCompact(summary.data.backlog_conversations + summary.data.backlog_tickets) : "—"} definition="Open conversations and tickets at the end of the period." /><Metric label="Reopened tickets" value={summary.data ? formatCompact(summary.data.tickets_reopened) : "—"} definition="Tickets moved from resolved or closed back to an active state." /><Metric label="Active SLA timers" value={formatCompact(active)} definition="Timers currently running across this workspace." /><Metric label="Breached timers" value={formatCompact(breaches)} definition="Timers that have crossed their business-hours deadline." /></CardBody></Card></Section>
      <Section title="Conversation volume"><Card><CardBody>{conversationPoints.length ? <AreaChart height={220} series={[{ key: "conversations", label: "Conversations", points: conversationPoints, tone: 1 }]} formatLabel={(label) => label.slice(5)} /> : <EmptyState icon={Inbox} title="No conversation events yet" description="The chart will populate after the analytics worker folds events." />}</CardBody></Card></Section>
      <Section title="Breakdown and period comparison" description="Conversation creation cohorts compared with the preceding period. Percentiles exclude unanswered or unresolved threads; tag groups can overlap."><Card><CardBody>
        <div className="mb-4 flex flex-wrap gap-2"><Select aria-label="Group report by" value={groupBy} onValueChange={setGroupBy} options={["inbox","team","channel","tag"].map(value=>({value,label:value}))}/><Select aria-label="Report inbox" value={inboxID} onValueChange={setInboxID} options={[{value:"all",label:"All visible inboxes"},...inboxes.map(inbox=>({value:inbox.id,label:inbox.name}))]}/><Select aria-label="Report team" value={teamID} onValueChange={setTeamID} options={[{value:"all",label:"All teams"},...teams.map(team=>({value:team.id,label:team.name}))]}/></div>
        {drilldown.error ? <QueryError error={drilldown.error} retry={drilldown.refetch}/> : drilldown.isLoading ? <p role="status">Loading breakdown…</p> : <div className="overflow-x-auto"><table className="w-full text-left text-sm"><thead><tr>{["Group","This period","Previous","Response p50","Response p90","Resolution p50","Resolution p90"].map(label=><th className="p-2" key={label}>{label}</th>)}</tr></thead><tbody>{drilldown.data?.data.map(row=><tr key={row.key} className="border-t border-line"><td className="p-2">{row.label}</td><td className="p-2">{row.current}</td><td className="p-2">{row.previous}</td>{[row.first_response_p50,row.first_response_p90,row.resolution_p50,row.resolution_p90].map((value,index)=><td className="p-2" key={index}>{value===null?"—":formatDuration(value)}</td>)}</tr>)}</tbody></table></div>}
      </CardBody></Card></Section>
      <Section title="Agent and team workload"><Card><CardBody>{workload.isLoading ? <p className="text-sm text-fg-muted">Loading workload…</p> : workload.data?.data.length ? <div className="overflow-x-auto"><table className="w-full min-w-[680px] text-left text-sm"><thead className="border-b border-line text-xs text-fg-muted"><tr><th className="px-3 py-2 font-medium">Owner</th><th className="px-3 py-2 text-right font-medium">Active conversations</th><th className="px-3 py-2 text-right font-medium">Active tickets</th><th className="px-3 py-2 text-right font-medium">Replies</th><th className="px-3 py-2 text-right font-medium">Resolved</th></tr></thead><tbody className="divide-y divide-line-subtle">{workload.data.data.map((item) => <tr key={`${item.subject_type}-${item.subject_id}`}><td className="px-3 py-2 text-fg"><span>{item.name}</span><span className="ml-2 text-xs text-fg-muted">{item.subject_type}</span></td><td className="px-3 py-2 text-right tabular text-fg-secondary">{item.active_conversations.toLocaleString()}</td><td className="px-3 py-2 text-right tabular text-fg-secondary">{item.active_tickets.toLocaleString()}</td><td className="px-3 py-2 text-right tabular text-fg-secondary">{item.replies_sent.toLocaleString()}</td><td className="px-3 py-2 text-right tabular text-fg-secondary">{item.resolved.toLocaleString()}</td></tr>)}</tbody></table></div> : <EmptyState icon={Inbox} title="No assigned workload yet" description="Agent and team rows appear after members, teams, or assigned work are created." />}</CardBody></Card></Section>
    </>}
  </PageBody></Page>;
}

function sum(points: { v: number }[]) { return points.reduce((total, point) => total + point.v, 0); }
