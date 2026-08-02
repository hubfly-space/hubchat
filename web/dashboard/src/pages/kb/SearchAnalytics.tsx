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
  formatDateTime,
  useInfinite,
  type Paginated,
} from "@hubchat/shared";
import { FileWarning, Inbox, Plus } from "lucide-react";
import { useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { latestComputedAt, useAnalyticsRollups } from "../../lib/analytics";
import { useWorkspace, workspaceFormatOptions } from "../../app/workspace-context";

type SearchTerm = {
  query: string;
  count: number;
  last_occurred_at: string;
};

/** Search analytics reads the same durable rollup contract as reports. */
export default function SearchAnalytics() {
  const navigate = useNavigate();
  const { workspace } = useWorkspace();
  const dateFormat = workspaceFormatOptions(workspace);
  const { from, to } = useMemo(() => {
    const to = new Date();
    return { from: new Date(to.getTime() - 90 * 86400000).toISOString(), to: to.toISOString() };
  }, []);
  const views = useAnalyticsRollups(["kb", "search-views"], "knowledgebase.article_views", from, to);
  const searches = useAnalyticsRollups(["kb", "searches"], "knowledgebase.searches", from, to);
  const noResults = useAnalyticsRollups(["kb", "search-no-results"], "knowledgebase.search_no_results", from, to);
  const noResultTerms = useInfinite<SearchTerm>(["kb", "search-no-result-terms"], (cursor, signal) => {
    const params = new URLSearchParams({ from, to, limit: "25" });
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<SearchTerm>>(`/analytics/searches/no-results?${params}`, { signal });
  });
  const viewPoints = views.items.map((item) => ({ t: item.bucket, v: item.value }));
  const searchCount = searches.items.reduce((total, item) => total + item.value, 0);
  const noResultCount = noResults.items.reduce((total, item) => total + item.value, 0);
  const freshness = latestComputedAt(views.items, searches.items, noResults.items);

  return <Page>
    <PageHeader title="Search analytics" description="What customers look for in the help centre, and where they come up empty." />
    <PageBody>
      <p className="mb-5 text-xs text-fg-muted">Rollup data last computed {freshness ? formatDateTime(freshness, dateFormat) : "not yet"}.</p>
      <Section><Card><CardBody className="grid gap-6 sm:grid-cols-4"><Metric label="Searches" value={formatCompact(searchCount)} definition="Total help-centre and widget searches in the last 90 days." /><Metric label="Zero-result searches" value={formatCompact(noResultCount)} definition="Searches that returned no article." /><Metric label="Article views" value={formatCompact(viewPoints.reduce((sum, point) => sum + point.v, 0))} definition="Article-view events folded by the analytics worker." /><Metric label="Click-through" value="—" definition="This ratio appears when search result click events are recorded." /></CardBody></Card></Section>
      <Section title="Article views"><Card><CardBody>{views.isFetching ? <p className="text-sm text-fg-muted">Loading live search rollups…</p> : views.isError ? <EmptyState icon={Inbox} title="Search data unavailable" description="Could not load article-view rollups." action={<Button variant="secondary" onClick={views.refetch}>Try again</Button>} /> : viewPoints.length === 0 ? <EmptyState icon={Inbox} title="No article-view events yet" description="This chart will populate when portal and widget article views are folded." /> : <AreaChart height={200} series={[{ key: "views", label: "Article views", points: viewPoints, tone: 1 }]} formatLabel={(label) => label.slice(5)} />}</CardBody></Card></Section>
      <div className="grid gap-4 lg:grid-cols-2"><Section title="Most viewed articles"><Card><CardBody>{viewPoints.length === 0 ? <EmptyState icon={Inbox} title="No article data yet" description="Article ranking will appear after view events are available." /> : <BarChart horizontal points={viewPoints.map((point) => ({ t: point.t.slice(0, 10), v: point.v }))} />}</CardBody></Card></Section><Section title="Searches with no result"><Callout tone="warning" className="mb-3" icon={<FileWarning />}>No-result queries are retained as operational analytics, without automated classification.</Callout><Card><CardHeader title="Top failed queries" actions={<Button variant="ghost" size="xs" leading={<Plus />} onClick={() => navigate("/kb/articles/new")}>Write article</Button>} /><CardBody>{noResultTerms.isLoading ? <p className="text-sm text-fg-muted">Loading no-result queries…</p> : noResultTerms.error ? <EmptyState icon={Inbox} title="No-result data unavailable" description="Could not load exact no-result queries." action={<Button variant="secondary" onClick={noResultTerms.refetch}>Try again</Button>} /> : noResultTerms.items.length === 0 ? <EmptyState icon={Inbox} title="No no-result queries" description="Exact query text will appear here once search events are recorded." /> : <><div className="divide-y divide-border">{noResultTerms.items.map((term) => <div className="flex items-center justify-between gap-4 py-3 first:pt-0 last:pb-0" key={`${term.query}-${term.last_occurred_at}`}><span className="min-w-0 truncate text-sm text-fg">{term.query}</span><span className="shrink-0 text-sm text-fg-muted">{formatCompact(term.count)}</span></div>)}</div>{noResultTerms.hasMore ? <div className="mt-4 flex justify-center"><Button variant="secondary" size="sm" onClick={() => void noResultTerms.fetchNext()} disabled={noResultTerms.isFetching}>{noResultTerms.isFetching ? "Loading…" : "Load more"}</Button></div> : null}</>}</CardBody></Card></Section></div>
    </PageBody>
  </Page>;
}
