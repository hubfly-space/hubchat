import {
  ApiError,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  EmptyState,
  Page,
  PageBody,
  PageHeader,
  Section,
  UsageMeter,
  api,
  formatDateTime,
  useQuery,
} from "@hubchat/shared";
import { BarChart3, Info } from "lucide-react";
import { useWorkspace, workspaceFormatOptions } from "../../app/workspace-context";

type UsageMetric = {
  key: string;
  label: string;
  used: number | null;
  limit?: number | null;
  unit?: string;
  period?: string;
  measured: boolean;
};
type UsageLimit = { key: string; label: string; value: number; unit?: string };
type UsageSnapshot = { computed_at: string; metrics: UsageMetric[]; request_limits: UsageLimit[] };

const GROUPS = [
  { title: "People", keys: ["workspace_members", "teams"] },
  { title: "Surfaces", keys: ["inboxes", "widgets", "portals", "feedback_boards", "knowledge_bases"] },
  { title: "Volume", keys: ["monthly_active_contacts", "conversations_month", "events_month", "api_requests_day"] },
];

function displayLimit(limit: UsageLimit) {
  if (limit.unit === "bytes") return `${(limit.value / 1024 / 1024).toFixed(limit.value >= 1024 * 1024 ? 0 : 2)} MB`;
  return limit.value.toLocaleString();
}

function Metric({ metric }: { metric: UsageMetric }) {
  if (!metric.measured || metric.used == null) {
    return <div className="flex items-baseline justify-between gap-3 border-b border-line-subtle py-2 last:border-b-0"><span className="text-sm text-fg-secondary">{metric.label}</span><span className="text-xs text-fg-muted">Not recorded</span></div>;
  }
  return <UsageMeter label={metric.label} used={metric.used} limit={metric.limit ?? null} unit={metric.unit === "bytes" ? "bytes" : undefined} />;
}

/** Live usage and deployment ceilings (§23). */
export default function Limits() {
  const { workspace } = useWorkspace();
  const dateFormat = workspaceFormatOptions(workspace);
  const query = useQuery<UsageSnapshot>(["workspace-usage"], (signal) => api.get("/workspace/usage", { signal }));
  const metrics = new Map((query.data?.metrics ?? []).map((metric) => [metric.key, metric]));

  return (
    <Page>
      <PageHeader title="Usage & limits" description="Measured workspace consumption and the request ceilings configured for this deployment." />
      <PageBody width="narrow">
        <Callout tone="info" className="mb-5" icon={<Info />}>
          Usage is calculated from workspace-owned records. A counter marked “Not recorded” means the deployment has not enabled that meter; it is never displayed as a fabricated zero.
        </Callout>

        {query.isLoading ? <p className="text-sm text-fg-muted">Loading live usage…</p> : query.error ? <EmptyState icon={BarChart3} title="Usage unavailable" description={query.error instanceof ApiError ? query.error.message : "Could not load workspace usage."} action={<Button variant="secondary" onClick={query.refetch}>Try again</Button>} /> : (
          <>
            {GROUPS.map((group) => <Section key={group.title} title={group.title}><Card><CardBody className="space-y-4">{group.keys.map((key) => { const metric = metrics.get(key); return metric ? <Metric key={key} metric={metric} /> : <p key={key} className="text-sm text-fg-muted">{key.replaceAll("_", " ")} is not available.</p>; })}</CardBody></Card></Section>)}

            <Section title="Storage"><Card><CardBody>{metrics.get("storage_bytes") ? <Metric metric={metrics.get("storage_bytes")!} /> : <p className="text-sm text-fg-muted">Storage usage is not available.</p>}<p className="mt-3 text-xs text-fg-muted">Computed {query.data?.computed_at ? formatDateTime(query.data.computed_at, dateFormat) : "—"}.</p></CardBody></Card></Section>

            <Section title="Per-request ceilings"><Card><CardHeader title="Configured hard limits" description="These values come from the running deployment configuration. Requests exceeding them are rejected rather than silently truncated." /><CardBody><dl className="space-y-2 text-xs">{(query.data?.request_limits ?? []).map((limit) => <div key={limit.key} className="flex items-baseline justify-between gap-3"><dt className="text-fg-muted">{limit.label}</dt><dd className="shrink-0 tabular text-fg-secondary">{displayLimit(limit)}{limit.unit === "count" ? "" : ""}</dd></div>)}</dl></CardBody></Card></Section>

            <Section title="Edition"><Card><CardBody className="flex items-center justify-between gap-4"><div><p className="text-sm text-fg">Self-hosted, open source</p><p className="mt-0.5 text-xs text-fg-muted">No entitlement checks are active. Every installed module is available.</p></div><Badge tone="success">All features</Badge></CardBody></Card></Section>
          </>
        )}
      </PageBody>
    </Page>
  );
}
