import {
  AreaChart,
  Button,
  Callout,
  Card,
  CardBody,
  ConfirmDialog,
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
  Switch,
  api,
  downloadFile,
  formatCompact,
  formatDuration,
  formatPercent,
  formatDateTime,
  idempotencyKey,
  useInfinite,
  useMutation,
  useQuery,
} from "@hubchat/shared";
import { CalendarClock, Download, Inbox, Info, Trash2 } from "lucide-react";
import type { Paginated } from "@hubchat/shared";
import { useState } from "react";
import { Link } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";
import { useAnalyticsRollups, type AnalyticsRollup } from "../../lib/analytics";

export const RANGES = [
  { value: "7d", label: "7 days" },
  { value: "30d", label: "30 days" },
  { value: "90d", label: "90 days" },
] as const;

type Summary = { first_response_seconds: number; sla_compliance_percent: number; sla_instances: number; backlog_conversations: number; backlog_tickets: number };
type SavedReport = { id: string; name: string; description?: string; date_range: string; timezone?: string; created_at: string };
type ReportSchedule = { id: string; report_id: string; cadence: string; recipients: string[]; format: string; enabled: boolean; next_run_at?: string; last_sent_at?: string; created_at: string; options: Record<string, unknown> };

/** Reporting overview backed by the durable event rollups. */
export default function ReportsOverview() {
  const { workspace } = useWorkspace();
  const [range, setRange] = useState<string>("30d");
  const [scheduleOpen, setScheduleOpen] = useState(false);
  const [recipient, setRecipient] = useState("");
  const [cadence, setCadence] = useState("weekly");
  const [selectedReportID, setSelectedReportID] = useState("");
  const [deletingReport, setDeletingReport] = useState<SavedReport | null>(null);
  const [deletingSchedule, setDeletingSchedule] = useState<ReportSchedule | null>(null);
  const days = range === "7d" ? 7 : range === "90d" ? 90 : 30;
  const from = new Date(Date.now() - days * 24 * 60 * 60 * 1000).toISOString();
  const conversations = useAnalyticsRollups(["reports", "conversations", range], "conversations.created", from);
  const tickets = useAnalyticsRollups(["reports", "tickets", range], "tickets.created", from);
  const reports = useInfinite<SavedReport>(["saved-reports"], (cursor, signal) => {
    const params = new URLSearchParams({ limit: "25" });
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<SavedReport>>(`/reports?${params.toString()}`, { signal });
  });
  const schedules = useInfinite<ReportSchedule>(
    selectedReportID ? ["report-schedules", selectedReportID] : null,
    (cursor, signal) => {
      if (!selectedReportID) return Promise.resolve({ data: [], next_cursor: null, has_more: false });
      const params = new URLSearchParams({ limit: "25" });
      if (cursor) params.set("cursor", cursor);
      return api.get<Paginated<ReportSchedule>>(`/reports/${encodeURIComponent(selectedReportID)}/schedules?${params.toString()}`, { signal });
    },
  );
  const summary = useQuery<Summary>(["reports", "summary", range], (signal) => api.get(`/analytics/summary?from=${encodeURIComponent(new Date(Date.now() - days * 86400000).toISOString())}`, { signal }));

  const conversationPoints = toPoints(conversations.items);
  const ticketPoints = toPoints(tickets.items);
  const channelSplit = splitChannels(conversations.items);
  const loading = conversations.isFetching || tickets.isFetching || summary.isLoading;
  const failed = conversations.isError || tickets.isError || summary.isError;
  const schedule = useMutation<void, ReportSchedule>(async () => {
    const report = await api.post<{ id: string }>("/reports", {
      name: `Support overview (${range})`,
      definition: { metrics: ["conversations.created", "tickets.created"] },
      date_range: range === "7d" ? "last_7_days" : range === "90d" ? "last_90_days" : "last_30_days",
      timezone: "UTC",
    }, { idempotencyKey: idempotencyKey() });
    return api.post<ReportSchedule>(`/reports/${encodeURIComponent(report.id)}/schedules`, {
      cadence,
      recipients: [recipient.trim()],
      format: "csv",
      options: { hour: 9, minute: 0, timezone: "UTC" },
    }, { idempotencyKey: idempotencyKey() });
  }, { invalidates: [["saved-reports"]], onSuccess: (created) => { setSelectedReportID(created.report_id); setScheduleOpen(false); setRecipient(""); } });
  const toggleSchedule = useMutation<{ schedule: ReportSchedule; enabled: boolean }, ReportSchedule>(({ schedule: item, enabled }) => api.patch("/reports/" + encodeURIComponent(item.report_id) + "/schedules/" + encodeURIComponent(item.id), {
    report_id: item.report_id,
    cadence: item.cadence,
    recipients: item.recipients,
    format: item.format,
    options: item.options,
    enabled,
  }, { idempotencyKey: idempotencyKey() }), { invalidates: [["report-schedules", selectedReportID]] });
  const deleteSchedule = useMutation<ReportSchedule, void>((item) => api.delete("/reports/" + encodeURIComponent(item.report_id) + "/schedules/" + encodeURIComponent(item.id), { idempotencyKey: idempotencyKey() }), {
    invalidates: [["report-schedules", selectedReportID]],
    onSuccess: () => setDeletingSchedule(null),
  });
  const deleteReport = useMutation<SavedReport, void>((item) => api.delete(`/reports/${encodeURIComponent(item.id)}`, { idempotencyKey: idempotencyKey() }), {
    invalidates: [["saved-reports"]],
    onSuccess: () => {
      if (deletingReport?.id === selectedReportID) setSelectedReportID("");
      setDeletingReport(null);
    },
  });
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

            <Section title="Saved reports">
              {reports.isLoading ? (
                <p className="text-sm text-fg-muted">Loading saved reports…</p>
              ) : reports.error ? (
                <EmptyState icon={CalendarClock} title="Saved reports unavailable" description="Could not load saved report schedules." action={<Button variant="secondary" size="sm" onClick={reports.refetch}>Try again</Button>} />
              ) : reports.items.length === 0 ? (
                <Card><CardBody><EmptyState icon={CalendarClock} title="No saved reports" description="Schedule a report to create a reusable saved report and delivery schedule." /></CardBody></Card>
              ) : (
                <>
                  <div className="grid gap-3 lg:grid-cols-2">
                    {reports.items.map((report) => (
                      <Card key={report.id} className={selectedReportID === report.id ? "ring-1 ring-accent" : ""}>
                        <CardBody>
                          <div className="flex items-start gap-3">
                            <div className="min-w-0 flex-1">
                              <button type="button" className="text-left text-sm font-medium text-fg hover:underline" onClick={() => setSelectedReportID(report.id)}>{report.name}</button>
                              <p className="mt-1 text-xs text-fg-muted">{report.date_range.replaceAll("_", " ")} · {report.timezone || "UTC"}</p>
                            </div>
                            <div className="flex shrink-0 gap-1"><Button variant="secondary" size="xs" onClick={() => setSelectedReportID(report.id)}>{selectedReportID === report.id ? "Selected" : "Manage"}</Button><Button variant="ghost" size="xs" iconOnly leading={<Trash2 />} aria-label={`Delete ${report.name}`} onClick={() => setDeletingReport(report)} /></div>
                          </div>
                          {selectedReportID === report.id && (
                            <div className="mt-4 border-t border-line-subtle pt-3">
                              {schedules.isLoading ? (
                                <p className="text-xs text-fg-muted">Loading schedules…</p>
                              ) : schedules.error ? (
                                <p className="text-xs text-danger">Could not load schedules.</p>
                              ) : schedules.items.length === 0 ? (
                                <p className="text-xs text-fg-muted">No delivery schedules for this report.</p>
                              ) : (
                                <>
                                  <ul className="divide-y divide-line-subtle">
                                    {schedules.items.map((item) => (
                                      <li key={item.id} className="flex flex-wrap items-center gap-3 py-2.5">
                                        <div className="min-w-0 flex-1">
                                          <p className="text-xs font-medium capitalize text-fg">{item.cadence} CSV · {item.recipients.join(", ")}</p>
                                          <p className="mt-0.5 text-2xs text-fg-muted">{item.enabled ? "Next run " + (item.next_run_at ? formatDateTime(item.next_run_at, { timeZone: report.timezone || "UTC" }) : "pending") : "Disabled"}{item.last_sent_at ? " · last sent " + formatDateTime(item.last_sent_at, { timeZone: report.timezone || "UTC" }) : ""}</p>
                                        </div>
                                        <Switch checked={item.enabled} onCheckedChange={(enabled) => void toggleSchedule.mutate({ schedule: item, enabled }).catch(() => {})} aria-label={(item.enabled ? "Disable " : "Enable ") + item.cadence + " schedule"} />
                                        <Button variant="ghost" size="xs" iconOnly leading={<Trash2 />} aria-label={"Delete " + item.cadence + " schedule"} onClick={() => setDeletingSchedule(item)} />
                                      </li>
                                    ))}
                                  </ul>
                                  {schedules.hasMore && <div className="flex justify-center border-t border-line-subtle p-3"><Button variant="secondary" size="xs" loading={schedules.isFetching} onClick={() => void schedules.fetchNext()}>Load more schedules</Button></div>}
                                </>
                              )}
                            </div>
                          )}
                        </CardBody>
                      </Card>
                    ))}
                  </div>
                  {reports.hasMore && <div className="flex justify-center pt-4"><Button variant="secondary" size="sm" loading={reports.isFetching} onClick={() => void reports.fetchNext()}>Load more saved reports</Button></div>}
                </>
              )}
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
      <ConfirmDialog
        open={Boolean(deletingReport)}
        onOpenChange={(open) => { if (!open) setDeletingReport(null); }}
        title="Delete saved report"
        description={deletingReport ? `This removes the saved report “${deletingReport.name}” and its delivery schedules. Historical rollups remain available.` : "This removes the saved report and its delivery schedules."}
        confirmLabel="Delete report"
        destructive
        loading={deleteReport.isPending}
        onConfirm={() => { if (deletingReport) void deleteReport.mutate(deletingReport).catch(() => {}) }}
      />
      <ConfirmDialog
        open={Boolean(deletingSchedule)}
        onOpenChange={(open) => { if (!open) setDeletingSchedule(null); }}
        title="Delete scheduled report"
        description="This removes the selected delivery schedule. The saved report and its historical analytics remain available."
        confirmLabel="Delete schedule"
        destructive
        loading={deleteSchedule.isPending}
        onConfirm={() => { if (deletingSchedule) void deleteSchedule.mutate(deletingSchedule).catch(() => {}); }}
      />
    </Page>
  );
}

function toPoints(items: AnalyticsRollup[]) {
  return items.map((item) => ({ t: item.bucket, v: item.value }));
}

function sum(points: { v: number }[]) {
  return points.reduce((total, point) => total + point.v, 0);
}

function splitChannels(items: AnalyticsRollup[]) {
  const totals = new Map<string, number>();
  for (const item of items) {
    const channel = typeof item.dimensions.channel === "string" ? item.dimensions.channel : "other";
    totals.set(channel, (totals.get(channel) ?? 0) + item.value);
  }
  const tones = [1, 2, 3, 4, 5] as const;
  return [...totals.entries()].map(([key, value], index) => ({ key, label: key, value, tone: tones[index % tones.length] ?? 1 }));
}
