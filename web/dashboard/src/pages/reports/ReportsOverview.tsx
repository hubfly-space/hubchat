import {
  AreaChart,
  Button,
  Callout,
  Card,
  CardBody,
  Dialog,
  DialogContent,
  DonutChart,
  Field,
  EmptyState,
  Input,
  Metric,
  Page,
  PageBody,
  PageHeader,
  Section,
  SegmentedControl,
  api,
  downloadFile,
  formatCompact,
  formatDuration,
  formatPercent,
  idempotencyKey,
  useMutation,
  useQuery,
} from "@hubchat/shared";
import { CalendarClock, Download, Inbox, Info } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";

export const RANGES = [
  { value: "7d", label: "7 days" },
  { value: "30d", label: "30 days" },
  { value: "90d", label: "90 days" },
] as const;

type Rollup = {
  bucket: string;
  value: number;
  count: number;
  dimensions: Record<string, unknown>;
};
type Summary = { first_response_seconds: number; sla_compliance_percent: number; sla_instances: number; backlog_conversations: number; backlog_tickets: number };

/** Reporting overview backed by the durable event rollups. */
export default function ReportsOverview() {
  const { workspace } = useWorkspace();
  const [range, setRange] = useState<string>("30d");
  const [scheduleOpen, setScheduleOpen] = useState(false);
  const [recipient, setRecipient] = useState("");
  const [cadence, setCadence] = useState("weekly");
  const days = range === "7d" ? 7 : range === "90d" ? 90 : 30;
  const query = (metric: string) => {
    const from = new Date(Date.now() - days * 24 * 60 * 60 * 1000).toISOString();
    return `/analytics/rollups?metric=${encodeURIComponent(metric)}&grain=day&from=${encodeURIComponent(from)}`;
  };
  const conversations = useQuery<{ data: Rollup[] }>(["reports", "conversations", range], (signal) => api.get(query("conversations.created"), { signal }));
  const tickets = useQuery<{ data: Rollup[] }>(["reports", "tickets", range], (signal) => api.get(query("tickets.created"), { signal }));
  const summary = useQuery<Summary>(["reports", "summary", range], (signal) => api.get(`/analytics/summary?from=${encodeURIComponent(new Date(Date.now() - days * 86400000).toISOString())}`, { signal }));

  const conversationPoints = toPoints(conversations.data?.data ?? []);
  const ticketPoints = toPoints(tickets.data?.data ?? []);
  const channelSplit = splitChannels(conversations.data?.data ?? []);
  const loading = conversations.isLoading || tickets.isLoading || summary.isLoading;
  const failed = conversations.isError || tickets.isError || summary.isError;
  const schedule = useMutation<void, unknown>(async () => {
    const report = await api.post<{ id: string }>("/reports", {
      name: `Support overview (${range})`,
      definition: { metrics: ["conversations.created", "tickets.created"] },
      date_range: range === "7d" ? "last_7_days" : range === "90d" ? "last_90_days" : "last_30_days",
      timezone: "UTC",
    }, { idempotencyKey: idempotencyKey() });
    return api.post(`/reports/${encodeURIComponent(report.id)}/schedules`, {
      cadence,
      recipients: [recipient.trim()],
      format: "csv",
      options: { hour: 9, minute: 0, timezone: "UTC" },
    }, { idempotencyKey: idempotencyKey() });
  }, { onSuccess: () => { setScheduleOpen(false); setRecipient(""); } });
  const exportCSV = () => {
    const from = new Date(Date.now() - days * 24 * 60 * 60 * 1000).toISOString();
    void downloadFile(`/analytics/export.csv?metrics=${encodeURIComponent("conversations.created,tickets.created")}&from=${encodeURIComponent(from)}`, `hubchat-reports-${range}.csv`, workspace.id).catch(() => undefined);
  };

  return (
    <Page>
      <PageHeader
        title="Reports"
        description="Deterministic aggregates over stored events. Every metric shows its definition."
        actions={
          <>
            <SegmentedControl aria-label="Date range" value={range} onValueChange={setRange} options={RANGES.map((item) => ({ value: item.value, label: item.label }))} />
            <Button variant="secondary" size="sm" leading={<CalendarClock />} onClick={() => setScheduleOpen(true)}>Schedule</Button>
            <Button variant="secondary" size="sm" leading={<Download />} onClick={exportCSV}>Export CSV</Button>
          </>
        }
      />

      <PageBody>
        <Callout tone="info" icon={<Info />} className="mb-5">
          Figures are folded from the append-only event log in UTC day buckets. A rollup is empty until the worker has processed the workspace events.
        </Callout>

        {loading ? <p className="text-sm text-fg-muted">Loading live report rollups…</p> : failed ? <EmptyState icon={Inbox} title="Reports unavailable" description="The analytics rollup API could not be loaded." action={<Button variant="secondary" size="sm" onClick={() => { conversations.refetch(); tickets.refetch(); summary.refetch(); }}>Try again</Button>} /> : (
          <>
            <Section title="Headline">
              <Card>
                <CardBody className="grid gap-6 sm:grid-cols-2 xl:grid-cols-4">
                  <Metric label="Conversations" value={formatCompact(sum(conversationPoints))} sparkline={conversationPoints} definition="Conversations created in the selected period, across every channel." />
                  <Metric label="Tickets" value={formatCompact(sum(ticketPoints))} sparkline={ticketPoints} definition="Tickets created in the selected period." />
                  <Metric label="First response" value={summary.data?.first_response_seconds ? formatDuration(summary.data.first_response_seconds) : "—"} definition="Average elapsed wall-clock time from the first customer reply to the first agent reply." />
                  <Metric label="SLA compliance" value={summary.data && summary.data.sla_instances > 0 ? formatPercent(summary.data.sla_compliance_percent / 100) : "—"} definition="Satisfied SLA instances divided by all met or breached instances in the selected period." />
                  <Metric label="Backlog" value={summary.data ? formatCompact(summary.data.backlog_conversations + summary.data.backlog_tickets) : "—"} definition="Open conversations and tickets at the end of the selected period." />
                </CardBody>
              </Card>
            </Section>

            <Section title="Volume">
              <Card><CardBody><AreaChart height={220} series={[{ key: "conversations", label: "Conversations", points: conversationPoints, tone: 1 }, { key: "tickets", label: "Tickets", points: ticketPoints, tone: 2 }]} formatLabel={(label) => label.slice(5)} /></CardBody></Card>
            </Section>

            <Section title="Channel mix">
              {channelSplit.length === 0 ? <Card><CardBody><EmptyState icon={Inbox} title="No channel events yet" description="Channel mix appears after the first conversation events are folded." /></CardBody></Card> : <Card><CardBody><DonutChart segments={channelSplit} centerValue={formatCompact(channelSplit.reduce((total, item) => total + item.value, 0))} centerLabel="conversations" /></CardBody></Card>}
            </Section>

            <Section title="Go deeper">
              <div className="grid gap-3 sm:grid-cols-3">
                {[{ to: "/reports/support", title: "Support operations", detail: "Response and resolution times, backlog, SLA compliance, agent workload." }, { to: "/reports/experience", title: "Customer experience", detail: "Satisfaction, effort, recommendation, repeat contacts, article helpfulness." }, { to: "/reports/surfaces", title: "Widget & portal", detail: "Impressions, opens, conversation starts, form submissions, deflection." }].map((report) => <Card key={report.to} interactive className="p-0"><Link to={report.to} className="block p-4"><p className="text-sm font-medium text-fg">{report.title}</p><p className="mt-1 text-xs leading-normal text-fg-muted">{report.detail}</p></Link></Card>)}
              </div>
            </Section>
          </>
        )}
      </PageBody>
      <Dialog open={scheduleOpen} onOpenChange={setScheduleOpen}>
        <DialogContent
          title="Schedule this report"
          description="A CSV snapshot will be queued from the workspace event rollups."
          footer={<><Button variant="ghost" size="sm" onClick={() => setScheduleOpen(false)}>Cancel</Button><Button variant="primary" size="sm" loading={schedule.isPending} disabled={!recipient.trim()} onClick={() => void schedule.mutate(undefined).catch(() => {})}>Schedule report</Button></>}
        >
          <div className="space-y-4 pb-2">
            <Field label="Recipient" description="Use a single address for this schedule; add more recipients later through the API."><Input type="email" value={recipient} onChange={(event) => setRecipient(event.target.value)} placeholder="ops@example.com" autoFocus /></Field>
            <Field label="Cadence"><select className="h-9 w-full rounded-md border border-line bg-surface px-3 text-sm text-fg" value={cadence} onChange={(event) => setCadence(event.target.value)}><option value="daily">Daily</option><option value="weekly">Weekly</option><option value="monthly">Monthly</option></select></Field>
            {Boolean(schedule.error) && <p className="text-sm text-danger">Could not schedule this report. Check the recipient and try again.</p>}
          </div>
        </DialogContent>
      </Dialog>
    </Page>
  );
}

function toPoints(items: Rollup[]) {
  return items.map((item) => ({ t: item.bucket, v: item.value }));
}

function sum(points: { v: number }[]) {
  return points.reduce((total, point) => total + point.v, 0);
}

function splitChannels(items: Rollup[]) {
  const totals = new Map<string, number>();
  for (const item of items) {
    const channel = typeof item.dimensions.channel === "string" ? item.dimensions.channel : "other";
    totals.set(channel, (totals.get(channel) ?? 0) + item.value);
  }
  const tones = [1, 2, 3, 4, 5] as const;
  return [...totals.entries()].map(([key, value], index) => ({ key, label: key, value, tone: tones[index % tones.length] ?? 1 }));
}
