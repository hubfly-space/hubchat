import {
  Badge,
  ApiError,
  Button,
  Callout,
  Card,
  CardBody,
  ConfirmDialog,
  DataTable,
  EmptyState,
  Metric,
  Page,
  PageBody,
  PageHeader,
  Pagination,
  SegmentedControl,
  Section,
  Toolbar,
  Tooltip,
  formatRelativeShort,
  api,
  useMutation,
  useInfinite,
  useQuery,
  type BadgeTone,
  type Column,
  type Job,
  type Paginated,
} from "@hubchat/shared";
import { Activity, Ban, RotateCcw, Webhook } from "lucide-react";
import { useState } from "react";

const STATE: Record<Job["state"], { label: string; tone: BadgeTone }> = {
  pending: { label: "Queued", tone: "neutral" },
  running: { label: "Running", tone: "info" },
  succeeded: { label: "Succeeded", tone: "success" },
  failed: { label: "Failed", tone: "warning" },
  dead: { label: "Dead letter", tone: "danger" },
  cancelled: { label: "Cancelled", tone: "neutral" },
};

type JobSummary = {
  queue_depth: number;
  running: number;
  failed_24h: number;
  dead: number;
};

type OpsSummary = {
  jobs: JobSummary;
  webhooks: { pending: number; failed_24h: number; exhausted: number; auto_disabled: number };
  email: { mailboxes: number; delivery_errors_24h: number; bounces_24h: number; suppressions: number };
  storage: { backend: string; committed_files: number; bytes: number; pending_uploads: number };
  realtime: { connections: number };
  computed_at: string;
};

/** Background job inspection (§8.7). */
export default function Jobs() {
  const [filter, setFilter] = useState<"all" | Job["state"]>("all");
  const jobs = useInfinite<Job>(
    ["jobs", filter],
    (cursor, signal) => {
      const params = new URLSearchParams({ limit: "50" });
      if (filter !== "all") params.set("state", filter);
      if (cursor) params.set("cursor", cursor);
      return api.get<Paginated<Job>>(`/jobs?${params.toString()}`, { signal });
    },
  );
  const summary = useQuery<JobSummary>(["jobs-summary"], (signal) => api.get("/jobs/summary", { signal }));
  const ops = useQuery<OpsSummary>(["ops-summary"], (signal) => api.get("/ops/summary", { signal }));
  const retry = useMutation<string, unknown>((id) => api.post(`/jobs/${encodeURIComponent(id)}/retry`), { invalidates: [["jobs"], ["jobs-summary"], ["ops-summary"]] });
  const cancel = useMutation<string, unknown>((id) => api.post(`/jobs/${encodeURIComponent(id)}/cancel`), { invalidates: [["jobs"], ["jobs-summary"], ["ops-summary"]] });
  const [cancelling, setCancelling] = useState<Job | null>(null);
  const rows = jobs.items;
  const dead = ops.data?.jobs.dead ?? summary.data?.dead ?? 0;

  const columns: Column<Job>[] = [
    {
      key: "type",
      header: "Job",
      cell: (job) => (
        <div className="min-w-0">
          <p className="truncate font-mono text-xs text-fg">{job.type}</p>
          <p className="truncate text-2xs text-fg-muted">{job.id}</p>
        </div>
      ),
    },
    {
      key: "queue",
      header: "Queue",
      width: "120px",
      cell: (job) => <Badge tone="neutral" variant="outline">{job.queue}</Badge>,
    },
    {
      key: "state",
      header: "State",
      width: "120px",
      cell: (job) => <Badge tone={STATE[job.state].tone}>{STATE[job.state].label}</Badge>,
    },
    {
      key: "attempt",
      header: "Attempt",
      width: "92px",
      numeric: true,
      cell: (job) => (
        <Tooltip content={`Retries with exponential backoff up to ${job.max_attempts} attempts`}>
          <span className={job.attempt > 1 ? "text-warning-text" : undefined}>
            {job.attempt}/{job.max_attempts}
          </span>
        </Tooltip>
      ),
    },
    {
      key: "scheduled_at",
      header: "Scheduled",
      width: "100px",
      numeric: true,
      cell: (job) => (
        <span className="text-xs text-fg-muted">{formatRelativeShort(job.scheduled_at)}</span>
      ),
      sortable: true,
    },
    {
      key: "error",
      header: "Error",
      hideBelow: "xl",
      cell: (job) =>
        job.error ? (
          <span className="truncate font-mono text-2xs text-danger-text">{job.error}</span>
        ) : (
          <span className="text-xs text-fg-disabled">—</span>
        ),
    },
  ];

  return (
    <Page>
      <PageHeader
        title="Background jobs"
        description="The embedded worker's queue. Durable in PostgreSQL — a restart does not lose work."
      />

      <PageBody width="full">
        {ops.error ? (
          <Callout tone="warning" className="mb-5" title="Operational health is unavailable">
            Queue controls are still available, but webhook, email, storage, and realtime health could not be loaded.{" "}
            <Button variant="ghost" size="xs" onClick={ops.refetch}>Try again</Button>
          </Callout>
        ) : null}

        {dead > 0 && (
          <Callout
            tone="danger"
            className="mb-5"
            title={`${dead} job${dead === 1 ? "" : "s"} in the dead-letter state`}
          >
            These exhausted every retry. They will not run again without a manual retry, and they are
            not blocking the rest of the queue.
          </Callout>
        )}

        {ops.data && <Section title="Operational health" description={"Workspace services as of " + new Date(ops.data.computed_at).toLocaleString() + ". Counts are scoped to this workspace."}>
          <Card>
            <CardBody className="grid gap-6 sm:grid-cols-2 lg:grid-cols-4">
              <Metric label="Realtime connections" value={ops.data.realtime.connections} definition="Currently connected dashboard, portal, and widget sessions on this process." />
              <Metric label="Webhook failures (24h)" value={ops.data.webhooks.failed_24h} higherIsBetter={false} definition="Webhook deliveries that failed in the last 24 hours." />
              <Metric label="Email errors (24h)" value={ops.data.email.delivery_errors_24h} higherIsBetter={false} definition="Provider delivery failures and bounces recorded in the last 24 hours." />
              <Metric label="Storage" value={ops.data.storage.bytes.toLocaleString() + " bytes"} definition={ops.data.storage.committed_files + " committed files on the " + ops.data.storage.backend + " backend."} />
            </CardBody>
            {(ops.data.webhooks.exhausted > 0 || ops.data.webhooks.auto_disabled > 0 || ops.data.storage.pending_uploads > 0) && <div className="border-t border-line px-5 py-3 text-xs text-fg-muted">
              {ops.data.webhooks.exhausted > 0 && <span className="mr-4"><Webhook className="mr-1 inline size-3.5" />{ops.data.webhooks.exhausted} exhausted webhook deliveries</span>}
              {ops.data.webhooks.auto_disabled > 0 && <span className="mr-4">{ops.data.webhooks.auto_disabled} webhook endpoint{ops.data.webhooks.auto_disabled === 1 ? "" : "s"} auto-disabled</span>}
              {ops.data.storage.pending_uploads > 0 && <span>{ops.data.storage.pending_uploads} pending upload{ops.data.storage.pending_uploads === 1 ? "" : "s"}</span>}
            </div>}
          </Card>
        </Section>}

        {jobs.error ? <EmptyState icon={Activity} title="Job queue unavailable" description={jobs.error instanceof ApiError ? jobs.error.message : "Try again in a moment."} action={<Button variant="secondary" size="sm" onClick={jobs.refetch}>Try again</Button>} /> : <Section>
          <Card>
            <CardBody className="grid gap-6 sm:grid-cols-4">
              <Metric
                label="Queue depth"
                value={summary.data?.queue_depth ?? "—"}
                higherIsBetter={false}
                definition="Jobs waiting to be leased by a worker. A steadily rising depth means the worker cannot keep up."
              />
              <Metric
                label="Running"
                value={summary.data?.running ?? "—"}
                definition="Jobs currently leased and executing."
              />
              <Metric
                label="Failed (24h)"
                value={summary.data?.failed_24h ?? "—"}
                higherIsBetter={false}
                definition="Jobs that errored and are awaiting a retry."
              />
              <Metric
                label="Dead letter"
                value={summary.data?.dead ?? "—"}
                higherIsBetter={false}
                definition="Jobs that exhausted every retry and stopped."
              />
            </CardBody>
          </Card>
        </Section>}

        <Toolbar
          className="rounded-t-lg border border-b-0 border-line"
          leading={
            <SegmentedControl
              aria-label="Filter by state"
              value={filter}
              onValueChange={setFilter}
              options={[
                { value: "all", label: "All" },
                { value: "pending", label: "Queued" },
                { value: "running", label: "Running" },
                { value: "failed", label: "Failed" },
                { value: "dead", label: "Dead" },
                { value: "cancelled", label: "Cancelled" },
              ]}
            />
          }
          trailing={
            <Button variant="secondary" size="sm" leading={<RotateCcw />} disabled={retry.isPending || rows.every((job) => job.state !== "dead")} onClick={() => { rows.filter((job) => job.state === "dead").forEach((job) => void retry.mutate(job.id)); }}>
              Retry loaded dead jobs
            </Button>
          }
        />

        <Card className="rounded-t-none">
          <CardBody className="p-0">
            {jobs.isLoading ? <p className="p-5 text-sm text-fg-muted">Loading jobs…</p> : <DataTable
              aria-label="Background jobs"
              rows={rows}
              columns={columns}
              rowKey={(job) => job.id}
              rowActions={(job) =>
                job.state === "pending" || job.state === "failed" || job.state === "dead" ? (
                  <div className="flex gap-0.5">
                    {(job.state === "failed" || job.state === "dead") && <Tooltip content="Retry now">
                      <Button variant="ghost" size="xs" iconOnly aria-label="Retry" leading={<RotateCcw />} onClick={() => void retry.mutate(job.id)} />
                    </Tooltip>}
                    {job.state === "pending" && <Tooltip content="Cancel pending job">
                      <Button variant="ghost" size="xs" iconOnly aria-label="Cancel pending job" leading={<Ban />} onClick={() => setCancelling(job)} />
                    </Tooltip>}
                  </div>
                ) : null
              }
              empty={
                <EmptyState
                  icon={Activity}
                  title="Queue is clear"
                  description="Nothing matches this filter — which, for the dead-letter view, is the goal."
                />
              }
            />}
          </CardBody>
        </Card>

        <Pagination
          hasPrevious={false}
          hasNext={jobs.hasMore}
          onPrevious={() => undefined}
          onNext={() => void jobs.fetchNext()}
          summary={`${rows.length} job${rows.length === 1 ? "" : "s"} loaded`}
        />

        <ConfirmDialog
          open={cancelling !== null}
          onOpenChange={(open) => !open && setCancelling(null)}
          title="Cancel this queued job?"
          description={cancelling ? `The pending ${cancelling.type} job will be marked cancelled and will not be leased by a worker. You can retry it later from this queue.` : "The pending job will be marked cancelled."}
          confirmLabel="Cancel job"
          destructive
          loading={cancel.isPending}
          onConfirm={() => {
            if (!cancelling) return;
            void cancel.mutate(cancelling.id).then(() => setCancelling(null)).catch(() => {});
          }}
        />

        <p className="mt-3 text-xs text-fg-muted">
          Jobs can also be inspected and retried from the CLI:{" "}
          <code className="font-mono text-fg-secondary">hubchat jobs list --state=dead</code>,{" "}
          <code className="font-mono text-fg-secondary">hubchat jobs retry &lt;id&gt;</code>
        </p>
      </PageBody>
    </Page>
  );
}
