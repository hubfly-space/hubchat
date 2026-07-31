import {
  AreaChart,
  BarChart,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  EmptyState,
  Metric,
  Page,
  PageBody,
  PageHeader,
  Section,
  api,
  formatCompact,
  useQuery,
} from "@hubchat/shared";
import { FileWarning, Inbox, Plus } from "lucide-react";

type Rollup = { bucket: string; value: number; dimensions: Record<string, unknown> };

/** Search analytics reads the same durable rollup contract as reports. */
export default function SearchAnalytics() {
  const views = useQuery<{ data: Rollup[] }>(["kb", "search-views"], (signal) => api.get("/analytics/rollups?metric=articles.viewed&grain=day", { signal }));
  const searches = useQuery<{ data: Rollup[] }>(["kb", "searches"], (signal) => api.get("/analytics/rollups?metric=articles.searched&grain=day", { signal }));
  const noResults = useQuery<{ data: Rollup[] }>(["kb", "search-no-results"], (signal) => api.get("/analytics/rollups?metric=articles.search_no_result&grain=day", { signal }));
  const viewPoints = (views.data?.data ?? []).map((item) => ({ t: item.bucket, v: item.value }));
  const searchCount = (searches.data?.data ?? []).reduce((total, item) => total + item.value, 0);
  const noResultCount = (noResults.data?.data ?? []).reduce((total, item) => total + item.value, 0);

  return <Page>
    <PageHeader title="Search analytics" description="What customers look for in the help centre, and where they come up empty." />
    <PageBody>
      <Section><Card><CardBody className="grid gap-6 sm:grid-cols-4"><Metric label="Searches" value={formatCompact(searchCount)} definition="Total help-centre and widget searches in the available rollup period." /><Metric label="Zero-result searches" value={formatCompact(noResultCount)} definition="Searches that returned no article." /><Metric label="Article views" value={formatCompact(viewPoints.reduce((sum, point) => sum + point.v, 0))} definition="Article-view events folded by the analytics worker." /><Metric label="Click-through" value="—" definition="This ratio appears when search result click events are recorded." /></CardBody></Card></Section>
      <Section title="Article views"><Card><CardBody>{views.isLoading ? <p className="text-sm text-fg-muted">Loading live search rollups…</p> : views.isError ? <EmptyState icon={Inbox} title="Search data unavailable" description="Could not load article-view rollups." action={<Button variant="secondary" onClick={views.refetch}>Try again</Button>} /> : viewPoints.length === 0 ? <EmptyState icon={Inbox} title="No article-view events yet" description="This chart will populate when portal and widget article views are folded." /> : <AreaChart height={200} series={[{ key: "views", label: "Article views", points: viewPoints, tone: 1 }]} formatLabel={(label) => label.slice(5)} />}</CardBody></Card></Section>
      <div className="grid gap-4 lg:grid-cols-2"><Section title="Most viewed articles"><Card><CardBody>{viewPoints.length === 0 ? <EmptyState icon={Inbox} title="No article data yet" description="Article ranking will appear after view events are available." /> : <BarChart horizontal points={viewPoints.map((point) => ({ t: point.t.slice(0, 10), v: point.v }))} />}</CardBody></Card></Section><Section title="Searches with no result"><Callout tone="warning" className="mb-3" icon={<FileWarning />}>No-result queries are retained as operational analytics, without automated classification.</Callout><Card><CardHeader title="Top failed queries" actions={<Button variant="ghost" size="xs" leading={<Plus />}>Write article</Button>} /><CardBody><EmptyState icon={Inbox} title="No no-result queries" description="The dashboard will list exact query text here once search events are recorded." /></CardBody></Card></Section></div>
    </PageBody>
  </Page>;
}
