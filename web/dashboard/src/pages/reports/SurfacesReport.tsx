import { AreaChart, Button, Card, CardBody, EmptyState, Metric, Page, PageBody, PageHeader, Section, SegmentedControl, api, downloadCSV, formatCompact, useQuery } from "@hubchat/shared";
import { Download, Inbox } from "lucide-react";
import { useState } from "react";
import { RANGES } from "./ReportsOverview";

type Rollup = { bucket: string; value: number };

/** Widget and portal report backed by the durable visitor event rollups. */
export default function SurfacesReport() {
  const [range, setRange] = useState("30d");
  const widget = useQuery<{ data: Rollup[] }>(["surfaces-report", "widget", range], (signal) => api.get(`/analytics/rollups?metric=surfaces.widget.conversations_started&grain=day&from=${encodeURIComponent(fromFor(range))}`, { signal }));
  const portal = useQuery<{ data: Rollup[] }>(["surfaces-report", "portal", range], (signal) => api.get(`/analytics/rollups?metric=surfaces.portal.conversations_started&grain=day&from=${encodeURIComponent(fromFor(range))}`, { signal }));
  const impressions = useQuery<{ data: Rollup[] }>(["surfaces-report", "impressions", range], (signal) => api.get(`/analytics/rollups?metric=surfaces.widget.impressions&grain=day&from=${encodeURIComponent(fromFor(range))}`, { signal }));
  const opens = useQuery<{ data: Rollup[] }>(["surfaces-report", "opens", range], (signal) => api.get(`/analytics/rollups?metric=surfaces.widget.opens&grain=day&from=${encodeURIComponent(fromFor(range))}`, { signal }));
  const articleViews = useQuery<{ data: Rollup[] }>(["surfaces-report", "article-views", range], (signal) => api.get(`/analytics/rollups?metric=surfaces.widget.articles_viewed&grain=day&from=${encodeURIComponent(fromFor(range))}`, { signal }));
  const widgetPoints = points(widget.data?.data);
  const portalPoints = points(portal.data?.data);
  const impressionPoints = points(impressions.data?.data);
  const openPoints = points(opens.data?.data);
  const articleViewPoints = points(articleViews.data?.data);
  const allQueries = [widget, portal, impressions, opens, articleViews];
  const failed = allQueries.some((query) => query.isError);
  const exportReport = () => downloadCSV(`hubchat-surfaces-${range}.csv`, ["metric", "bucket", "value"], [
    ...rows("surfaces.widget.conversations_started", widgetPoints),
    ...rows("surfaces.portal.conversations_started", portalPoints),
    ...rows("surfaces.widget.impressions", impressionPoints),
    ...rows("surfaces.widget.opens", openPoints),
    ...rows("surfaces.widget.articles_viewed", articleViewPoints),
  ]);
  return <Page><PageHeader title="Widget & portal" description="Customer-facing surface usage from durable visitor events." actions={<><SegmentedControl aria-label="Date range" value={range} onValueChange={setRange} options={RANGES.map((item) => ({ value: item.value, label: item.label }))} /><Button variant="secondary" size="sm" leading={<Download />} disabled={allQueries.every((query) => !query.data)} onClick={exportReport}>Export CSV</Button></>} /><PageBody><Section title="Recorded funnel"><Card><CardBody className="grid gap-6 sm:grid-cols-2 xl:grid-cols-5"><Metric label="Widget impressions" value={formatCompact(sum(impressionPoints))} definition="Widget mounts recorded by the visitor event channel in the selected period." /><Metric label="Widget opens" value={formatCompact(sum(openPoints))} definition="Widget panels opened by visitors in the selected period." /><Metric label="Widget starts" value={formatCompact(sum(widgetPoints))} definition="Widget conversations created in the selected period." /><Metric label="Portal starts" value={formatCompact(sum(portalPoints))} definition="Portal conversations created in the selected period." /><Metric label="Article views" value={formatCompact(sum(articleViewPoints))} definition="Knowledge-base articles opened inside the widget in the selected period." /></CardBody></Card></Section><Section title="Conversation starts"><Card><CardBody>{failed ? <EmptyState icon={Inbox} title="Surface data unavailable" description="Could not load one or more live surface rollups." action={<Button variant="secondary" onClick={() => allQueries.forEach((query) => query.refetch())}>Try again</Button>} /> : widgetPoints.length || portalPoints.length ? <AreaChart height={200} series={[{ key: "widget", label: "Widget", points: widgetPoints, tone: 1 }, { key: "portal", label: "Portal", points: portalPoints, tone: 2 }]} formatLabel={(label) => label.slice(5)} /> : <EmptyState icon={Inbox} title="No surface events yet" description="The chart will populate after widget or portal conversations are created." />}</CardBody></Card></Section></PageBody></Page>;
}
function fromFor(range: string) { const days = range === "7d" ? 7 : range === "90d" ? 90 : 30; return new Date(Date.now() - days * 86400000).toISOString(); }
function points(items: Rollup[] | undefined) { return (items ?? []).map((item) => ({ t: item.bucket, v: item.value })); }
function rows(metric: string, items: { t: string; v: number }[]) { return items.map((item) => [metric, item.t, item.v] as (string | number | null)[]); }
function sum(points: { v: number }[]) { return points.reduce((total, point) => total + point.v, 0); }
