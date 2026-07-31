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
  SegmentedControl,
  Section,
  Toolbar,
  Tooltip,
  formatRelativeShort,
  api,
  useMutation,
  useQuery,
  type BadgeTone,
  type Column,
  type Job,
} from "@hubchat/shared";
import { Activity, Ban, RotateCcw } from "lucide-react";
import { useState } from "react";

const STATE: Record<Job["state"], { label: string; tone: BadgeTone }> = {
  pending: { label: "Queued", tone: "neutral" },
  running: { label: "Running", tone: "info" },
  succeeded: { label: "Succeeded", tone: "success" },
  failed: { label: "Failed", tone: "warning" },
  dead: { label: "Dead letter", tone: "danger" },
  cancelled: { label: "Cancelled", tone: "neutral" },
};

/** Background job inspection (§8.7). */
export default function Jobs() {
  const [filter, setFilter] = useState<"all" | Job["state"]>("all");
  const query = useQuery<{ data: Job[] }>(["jobs", "all"], (signal) => api.get("/jobs", { signal }));
  const retry = useMutation<string, unknown>((id) => api.post(`/jobs/${encodeURIComponent(id)}/retry`), { invalidates: [["jobs", filter], ["jobs", "all"]] });
  const cancel = useMutation<string, unknown>((id) => api.post(`/jobs/${encodeURIComponent(id)}/cancel`), { invalidates: [["jobs", filter], ["jobs", "all"]] });
  const [cancelling, setCancelling] = useState<Job | null>(null);
  const allRows = query.data?.data ?? [];
  const rows = allRows.filter((job) => filter === "all" || job.state === filter);
  const dead = allRows.filter((job) => job.state === "dead").length;

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

        {query.isError ? <EmptyState icon={Activity} title="Job queue unavailable" description={query.error instanceof ApiError ? query.error.message : "Try again in a moment."} action={<Button variant="secondary" size="sm" onClick={query.refetch}>Try again</Button>} /> : <Section>
          <Card>
            <CardBody className="grid gap-6 sm:grid-cols-4">
              <Metric
                label="Queue depth"
                value={allRows.filter((job) => job.state === "pending").length}
                higherIsBetter={false}
                definition="Jobs waiting to be leased by a worker. A steadily rising depth means the worker cannot keep up."
              />
              <Metric
                label="Running"
                value={allRows.filter((job) => job.state === "running").length}
                definition="Jobs currently leased and executing."
              />
              <Metric
                label="Failed (24h)"
                value={allRows.filter((job) => job.state === "failed").length}
                higherIsBetter={false}
                definition="Jobs that errored and are awaiting a retry."
              />
              <Metric
                label="Dead letter"
                value={dead}
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
            <Button variant="secondary" size="sm" leading={<RotateCcw />} disabled={retry.isPending} onClick={() => { rows.filter((job) => job.state === "dead").forEach((job) => void retry.mutate(job.id)); }}>
              Retry dead jobs
            </Button>
          }
        />

        <Card className="rounded-t-none">
          <CardBody className="p-0">
            <DataTable
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
            />
          </CardBody>
        </Card>

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
