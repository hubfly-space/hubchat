import { AreaChart, Button, Card, CardBody, EmptyState, Metric, Page, PageBody, PageHeader, Section, SegmentedControl, api, formatCompact, useQuery } from "@hubchat/shared";
import { Download, Inbox } from "lucide-react";
import { useState } from "react";
import { RANGES } from "./ReportsOverview";

type Rollup = { bucket: string; value: number };

/** Widget and portal report. Surface-specific counters stay explicit until their event adapters are enabled. */
export default function SurfacesReport() {
  const [range, setRange] = useState("30d");
  const widget = useQuery<{ data: Rollup[] }>(["surfaces-report", "widget", range], (signal) => api.get(`/analytics/rollups?metric=surfaces.widget.conversations_started&grain=day&from=${encodeURIComponent(fromFor(range))}`, { signal }));
  const portal = useQuery<{ data: Rollup[] }>(["surfaces-report", "portal", range], (signal) => api.get(`/analytics/rollups?metric=surfaces.portal.conversations_started&grain=day&from=${encodeURIComponent(fromFor(range))}`, { signal }));
  const widgetPoints = (widget.data?.data ?? []).map((item) => ({ t: item.bucket, v: item.value }));
  const portalPoints = (portal.data?.data ?? []).map((item) => ({ t: item.bucket, v: item.value }));
  const points = [...widgetPoints, ...portalPoints];
  return <Page><PageHeader title="Widget & portal" description="Customer-facing surface usage from recorded events." actions={<><SegmentedControl aria-label="Date range" value={range} onValueChange={setRange} options={RANGES.map((item) => ({ value: item.value, label: item.label }))} /><Button variant="secondary" size="sm" leading={<Download />}>Export</Button></>} /><PageBody><Section title="Recorded funnel"><Card><CardBody className="grid gap-6 sm:grid-cols-2 xl:grid-cols-4"><Metric label="Widget starts" value={formatCompact(sum(widgetPoints))} definition="Widget conversations created in the selected period." /><Metric label="Portal starts" value={formatCompact(sum(portalPoints))} definition="Portal conversations created in the selected period." /><Metric label="Impressions" value="—" definition="Impression events are not yet recorded by the current runtime." /><Metric label="Deflection" value="—" definition="Deflection requires paired article-view and conversation events." /></CardBody></Card></Section><Section title="Conversation starts"><Card><CardBody>{widget.isError || portal.isError ? <EmptyState icon={Inbox} title="Surface data unavailable" description="Could not load live surface rollups." action={<Button variant="secondary" onClick={() => { widget.refetch(); portal.refetch(); }}>Try again</Button>} /> : points.length ? <AreaChart height={200} series={[{ key: "widget", label: "Widget", points: widgetPoints, tone: 1 }, { key: "portal", label: "Portal", points: portalPoints, tone: 2 }]} formatLabel={(label) => label.slice(5)} /> : <EmptyState icon={Inbox} title="No surface events yet" description="The chart will populate after widget or portal conversations are created." />}</CardBody></Card></Section></PageBody></Page>;
}
function fromFor(range: string) { const days = range === "7d" ? 7 : range === "90d" ? 90 : 30; return new Date(Date.now() - days * 86400000).toISOString(); }
function sum(points: { v: number }[]) { return points.reduce((total, point) => total + point.v, 0); }
