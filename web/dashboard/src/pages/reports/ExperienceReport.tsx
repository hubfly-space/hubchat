import { AreaChart, Button, Callout, Card, CardBody, EmptyState, Metric, Page, PageBody, PageHeader, Section, SegmentedControl, api, downloadCSV, formatCompact, useQuery } from "@hubchat/shared";
import { Download, Inbox } from "lucide-react";
import { useState } from "react";
import { RANGES } from "./ReportsOverview";

type Rollup = { bucket: string; value: number };

/** Customer experience report; scores remain blank until typed survey metrics are folded. */
export default function ExperienceReport() {
  const [range, setRange] = useState("30d");
  const responses = useQuery<{ data: Rollup[] }>(["experience-report", range], (signal) => api.get(`/analytics/rollups?metric=surveys.responses&grain=day&from=${encodeURIComponent(fromFor(range))}`, { signal }));
  const points = (responses.data?.data ?? []).map((item) => ({ t: item.bucket, v: item.value }));
  const exportReport = () => downloadCSV(`hubchat-experience-${range}.csv`, ["metric", "bucket", "value"], points.map((item) => ["surveys.responses", item.t, item.v]));
  return <Page><PageHeader title="Customer experience" description="What customers reported, without automated classification." actions={<><SegmentedControl aria-label="Date range" value={range} onValueChange={setRange} options={RANGES.map((item) => ({ value: item.value, label: item.label }))} /><Button variant="secondary" size="sm" leading={<Download />} disabled={!responses.data} onClick={exportReport}>Export CSV</Button></>} /><PageBody><Callout tone="info" className="mb-5">Survey responses are aggregated and shown verbatim. Hubchat does not classify or summarise free-text answers.</Callout><Section title="Scores"><Card><CardBody className="grid gap-6 sm:grid-cols-2 xl:grid-cols-4"><Metric label="Responses" value={formatCompact(sum(points))} definition="Survey responses recorded in the selected period." /><Metric label="CSAT" value="—" definition="Appears when typed CSAT answer rollups are available." /><Metric label="CES" value="—" definition="Appears when typed CES answer rollups are available." /><Metric label="NPS" value="—" definition="Appears when typed NPS answer rollups are available." /></CardBody></Card></Section><Section title="Survey responses"><Card><CardBody>{responses.isLoading ? <p className="text-sm text-fg-muted">Loading live survey rollups…</p> : responses.isError ? <EmptyState icon={Inbox} title="Experience data unavailable" description="Could not load survey response rollups." action={<Button variant="secondary" onClick={responses.refetch}>Try again</Button>} /> : points.length ? <AreaChart height={200} series={[{ key: "responses", label: "Responses", points, tone: 4 }]} formatLabel={(label) => label.slice(5)} /> : <EmptyState icon={Inbox} title="No survey responses yet" description="The chart will populate after the analytics worker folds survey events." />}</CardBody></Card></Section></PageBody></Page>;
}
function fromFor(range: string) { const days = range === "7d" ? 7 : range === "90d" ? 90 : 30; return new Date(Date.now() - days * 86400000).toISOString(); }
function sum(points: { v: number }[]) { return points.reduce((total, point) => total + point.v, 0); }
