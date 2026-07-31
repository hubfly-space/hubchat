import { AreaChart, Button, Card, CardBody, EmptyState, Metric, Page, PageBody, PageHeader, Section, SegmentedControl, api, formatCompact, useQuery } from "@hubchat/shared";
import { Download, Inbox } from "lucide-react";
import { useState } from "react";
import { RANGES } from "./ReportsOverview";

type Rollup = { bucket: string; value: number };

/** Widget and portal report. Surface-specific counters stay explicit until their event adapters are enabled. */
export default function SurfacesReport() {
  const [range, setRange] = useState("30d");
  const conversations = useQuery<{ data: Rollup[] }>(["surfaces-report", range], (signal) => api.get(`/analytics/rollups?metric=conversations.created&grain=day&from=${encodeURIComponent(fromFor(range))}`, { signal }));
  const points = (conversations.data?.data ?? []).map((item) => ({ t: item.bucket, v: item.value }));
  return <Page><PageHeader title="Widget & portal" description="Customer-facing surface usage from recorded events." actions={<><SegmentedControl aria-label="Date range" value={range} onValueChange={setRange} options={RANGES.map((item) => ({ value: item.value, label: item.label }))} /><Button variant="secondary" size="sm" leading={<Download />}>Export</Button></>} /><PageBody><Section title="Recorded funnel"><Card><CardBody className="grid gap-6 sm:grid-cols-2 xl:grid-cols-4"><Metric label="Conversation starts" value={formatCompact(sum(points))} definition="All conversation-created events in the selected period; surface attribution appears when widget/portal event adapters are enabled." /><Metric label="Impressions" value="—" definition="Widget impression events are not yet recorded by the current runtime." /><Metric label="Portal sign-ins" value="—" definition="Portal session rollups are not yet recorded by the current runtime." /><Metric label="Deflection" value="—" definition="Deflection requires paired article-view and conversation events." /></CardBody></Card></Section><Section title="Conversation starts"><Card><CardBody>{conversations.isError ? <EmptyState icon={Inbox} title="Surface data unavailable" description="Could not load live conversation rollups." action={<Button variant="secondary" onClick={conversations.refetch}>Try again</Button>} /> : points.length ? <AreaChart height={200} series={[{ key: "starts", label: "Conversation starts", points, tone: 1 }]} formatLabel={(label) => label.slice(5)} /> : <EmptyState icon={Inbox} title="No surface events yet" description="The chart will populate after conversation events are folded." />}</CardBody></Card></Section></PageBody></Page>;
}
function fromFor(range: string) { const days = range === "7d" ? 7 : range === "90d" ? 90 : 30; return new Date(Date.now() - days * 86400000).toISOString(); }
function sum(points: { v: number }[]) { return points.reduce((total, point) => total + point.v, 0); }
