import {
  Badge,
  Button,
  Card,
  CardBody,
  DataTable,
  EmptyState,
  HoverCard,
  Page,
  PageBody,
  PageHeader,
  SegmentedControl,
  Toolbar,
  Tooltip,
  formatRelativeShort,
  type AutomationExecution,
  type BadgeTone,
  type Column,
} from "@hubchat/shared";
import { Download, ScrollText } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { NOW, automationExecutions, automationRules } from "../../data/fixtures";

const OUTCOME: Record<AutomationExecution["outcome"], { label: string; tone: BadgeTone }> = {
  matched: { label: "Matched", tone: "success" },
  skipped: { label: "Skipped", tone: "neutral" },
  failed: { label: "Failed", tone: "danger" },
  dry_run: { label: "Dry run", tone: "system" },
};

/**
 * Execution log (§6.13 rule safety).
 *
 * This is the debugger for the automation engine. Every row answers: which
 * rule, against what, what happened, how deep in the causation chain, and how
 * long it took — because "the rule didn't fire" is the most common support
 * question about support software.
 */
export default function ExecutionLog() {
  const [filter, setFilter] = useState<"all" | AutomationExecution["outcome"]>("all");

  const rows = automationExecutions.filter(
    (execution) => filter === "all" || execution.outcome === filter,
  );

  const columns: Column<AutomationExecution>[] = [
    {
      key: "occurred_at",
      header: "When",
      width: "88px",
      numeric: true,
      cell: (execution) => (
        <Tooltip content={execution.occurred_at}>
          <span className="text-xs text-fg-muted">
            {formatRelativeShort(execution.occurred_at, NOW)}
          </span>
        </Tooltip>
      ),
      sortable: true,
    },
    {
      key: "rule",
      header: "Rule",
      cell: (execution) => {
        const rule = automationRules.find((item) => item.id === execution.rule_id);
        return (
          <HoverCard
            trigger={
              <Link
                to={`/automation/rules/${execution.rule_id}`}
                className="truncate text-sm text-fg hover:underline"
              >
                {rule?.name ?? execution.rule_id}
              </Link>
            }
          >
            <p className="text-sm font-medium text-fg">{rule?.name}</p>
            <p className="mt-1 text-xs text-fg-muted">{rule?.description ?? "No description."}</p>
            <p className="mt-2 text-2xs text-fg-disabled">
              Trigger: {rule?.trigger} · version {execution.rule_version}
            </p>
          </HoverCard>
        );
      },
    },
    {
      key: "subject",
      header: "Subject",
      width: "180px",
      hideBelow: "lg",
      cell: (execution) => (
        <span className="font-mono text-xs text-fg-muted">
          {execution.subject_type}/{execution.subject_id}
        </span>
      ),
    },
    {
      key: "outcome",
      header: "Outcome",
      width: "104px",
      cell: (execution) => (
        <Badge tone={OUTCOME[execution.outcome].tone}>{OUTCOME[execution.outcome].label}</Badge>
      ),
    },
    {
      key: "actions_applied",
      header: "Actions",
      width: "82px",
      numeric: true,
      cell: (execution) => execution.actions_applied,
    },
    {
      key: "depth",
      header: "Depth",
      width: "72px",
      numeric: true,
      hideBelow: "xl",
      cell: (execution) => (
        <Tooltip content="How many rule generations this originating event has caused so far">
          <span className={execution.depth > 2 ? "text-warning-text" : undefined}>
            {execution.depth}
          </span>
        </Tooltip>
      ),
    },
    {
      key: "duration_ms",
      header: "Duration",
      width: "88px",
      numeric: true,
      hideBelow: "md",
      cell: (execution) => <span className="text-xs text-fg-muted">{execution.duration_ms}ms</span>,
      sortable: true,
    },
  ];

  return (
    <Page>
      <PageHeader
        title="Execution log"
        description="Every rule evaluation, including the ones that skipped. Retained for 30 days."
        actions={
          <Button variant="secondary" size="sm" leading={<Download />}>
            Export
          </Button>
        }
      />

      <Toolbar
        leading={
          <SegmentedControl
            aria-label="Filter by outcome"
            value={filter}
            onValueChange={setFilter}
            options={[
              { value: "all", label: "All" },
              { value: "matched", label: "Matched" },
              { value: "skipped", label: "Skipped" },
              { value: "failed", label: "Failed" },
            ]}
          />
        }
      />

      <PageBody width="full">
        <Card>
          <CardBody className="p-0">
            <DataTable
              aria-label="Rule executions"
              rows={rows}
              columns={columns}
              rowKey={(execution) => execution.id}
              empty={
                <EmptyState
                  icon={ScrollText}
                  title="Nothing to show"
                  description="No executions match this filter in the retention window."
                />
              }
            />
          </CardBody>
        </Card>

        {rows.some((execution) => execution.error) && (
          <Card className="mt-4 border-danger-border">
            <CardBody>
              <p className="mb-2 text-sm font-medium text-fg">Recent errors</p>
              <ul className="space-y-1.5">
                {rows
                  .filter((execution) => execution.error)
                  .map((execution) => (
                    <li key={execution.id} className="font-mono text-xs text-danger-text">
                      {execution.error}
                    </li>
                  ))}
              </ul>
            </CardBody>
          </Card>
        )}
      </PageBody>
    </Page>
  );
}
