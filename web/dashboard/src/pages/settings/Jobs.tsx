import {
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
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
  type BadgeTone,
  type Column,
  type Job,
} from "@hubchat/shared";
import { Activity, RotateCcw, Trash2 } from "lucide-react";
import { useState } from "react";
import { NOW, jobs } from "../../data/fixtures";

const STATE: Record<Job["state"], { label: string; tone: BadgeTone }> = {
  pending: { label: "Queued", tone: "neutral" },
  running: { label: "Running", tone: "info" },
  succeeded: { label: "Succeeded", tone: "success" },
  failed: { label: "Failed", tone: "warning" },
  dead: { label: "Dead letter", tone: "danger" },
};

/** Background job inspection (§8.7). */
export default function Jobs() {
  const [filter, setFilter] = useState<"all" | Job["state"]>("all");

  const rows = jobs.filter((job) => filter === "all" || job.state === filter);
  const dead = jobs.filter((job) => job.state === "dead").length;

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
        <span className="text-xs text-fg-muted">{formatRelativeShort(job.scheduled_at, NOW)}</span>
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

        <Section>
          <Card>
            <CardBody className="grid gap-6 sm:grid-cols-4">
              <Metric
                label="Queue depth"
                value={jobs.filter((job) => job.state === "pending").length}
                higherIsBetter={false}
                definition="Jobs waiting to be leased by a worker. A steadily rising depth means the worker cannot keep up."
              />
              <Metric
                label="Running"
                value={jobs.filter((job) => job.state === "running").length}
                definition="Jobs currently leased and executing."
              />
              <Metric
                label="Failed (24h)"
                value={jobs.filter((job) => job.state === "failed").length}
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
        </Section>

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
              ]}
            />
          }
          trailing={
            <Button variant="secondary" size="sm" leading={<RotateCcw />}>
              Retry all dead
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
                job.state === "failed" || job.state === "dead" ? (
                  <div className="flex gap-0.5">
                    <Tooltip content="Retry now">
                      <Button variant="ghost" size="xs" iconOnly aria-label="Retry" leading={<RotateCcw />} />
                    </Tooltip>
                    <Tooltip content="Discard">
                      <Button variant="ghost" size="xs" iconOnly aria-label="Discard" leading={<Trash2 />} />
                    </Tooltip>
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

        <p className="mt-3 text-xs text-fg-muted">
          Jobs can also be inspected and retried from the CLI:{" "}
          <code className="font-mono text-fg-secondary">hubchat jobs list --state=dead</code>,{" "}
          <code className="font-mono text-fg-secondary">hubchat jobs retry &lt;id&gt;</code>
        </p>
      </PageBody>
    </Page>
  );
}
