import {
  AreaChart,
  BarChart,
  Button,
  Card,
  CardBody,
  CardHeader,
  DataTable,
  Metric,
  Page,
  PageBody,
  PageHeader,
  Progress,
  Section,
  SegmentedControl,
  formatCompact,
  formatDuration,
  formatPercent,
  type Column,
} from "@hubchat/shared";
import { Download } from "lucide-react";
import { useState } from "react";
import { analytics, inboxes, members, slaPolicies } from "../../data/fixtures";
import { RANGES } from "./ReportsOverview";

type AgentRow = {
  id: string;
  name: string;
  handled: number;
  firstResponse: number;
  resolution: number;
  csat: number;
  reopened: number;
};

const AGENT_ROWS: AgentRow[] = members.slice(0, 5).map((member, index) => ({
  id: member.id,
  name: member.name,
  handled: [148, 132, 119, 86, 41][index] ?? 0,
  firstResponse: [420, 610, 540, 900, 1_320][index] ?? 0,
  resolution: [9_400, 12_100, 10_800, 16_200, 21_000][index] ?? 0,
  csat: [0.96, 0.91, 0.94, 0.88, 0.82][index] ?? 0,
  reopened: [0.02, 0.05, 0.03, 0.08, 0.11][index] ?? 0,
}));

/** Support operations report (§6.18). */
export default function SupportReport() {
  const [range, setRange] = useState<string>("30d");

  const columns: Column<AgentRow>[] = [
    { key: "name", header: "Agent", cell: (row) => <span className="text-fg">{row.name}</span>, sortable: true },
    { key: "handled", header: "Handled", numeric: true, width: "92px", cell: (row) => row.handled, sortable: true },
    {
      key: "firstResponse",
      header: "First response",
      numeric: true,
      width: "128px",
      cell: (row) => formatDuration(row.firstResponse),
      sortable: true,
    },
    {
      key: "resolution",
      header: "Resolution",
      numeric: true,
      width: "116px",
      cell: (row) => formatDuration(row.resolution),
      sortable: true,
    },
    {
      key: "csat",
      header: "CSAT",
      numeric: true,
      width: "92px",
      cell: (row) => (
        <span className={row.csat >= 0.9 ? "text-success-text" : "text-warning-text"}>
          {formatPercent(row.csat)}
        </span>
      ),
      sortable: true,
    },
    {
      key: "reopened",
      header: "Reopened",
      numeric: true,
      width: "100px",
      hideBelow: "lg",
      cell: (row) => (
        <span className={row.reopened > 0.07 ? "text-danger-text" : undefined}>
          {formatPercent(row.reopened, 1)}
        </span>
      ),
      sortable: true,
    },
  ];

  return (
    <Page>
      <PageHeader
        title="Support operations"
        description="How fast the team responds, how much is waiting, and where load sits."
        actions={
          <>
            <SegmentedControl
              aria-label="Date range"
              value={range}
              onValueChange={setRange}
              options={RANGES.map((item) => ({ value: item.value, label: item.label }))}
            />
            <Button variant="secondary" size="sm" leading={<Download />}>
              Export
            </Button>
          </>
        }
      />

      <PageBody>
        <Section title="Response and resolution">
          <Card>
            <CardBody>
              <div className="mb-5 grid gap-6 sm:grid-cols-4">
                <Metric
                  label="Median first response"
                  value={formatDuration(analytics.firstResponse.at(-1)?.v ?? 0)}
                  delta={-0.114}
                  higherIsBetter={false}
                  definition="Business-hours time from the first inbound message to the first public reply."
                />
                <Metric
                  label="Median next response"
                  value={formatDuration(1_820)}
                  delta={-0.04}
                  higherIsBetter={false}
                  definition="Business-hours time between a customer reply and the following agent reply."
                />
                <Metric
                  label="Median resolution"
                  value={formatDuration(analytics.resolution.at(-1)?.v ?? 0)}
                  delta={-0.037}
                  higherIsBetter={false}
                  definition="Business-hours time from creation to the resolved state."
                />
                <Metric
                  label="Reopen rate"
                  value={formatPercent(0.043, 1)}
                  delta={0.006}
                  higherIsBetter={false}
                  definition="Share of resolved conversations reopened within 7 days."
                />
              </div>

              <AreaChart
                height={200}
                series={[
                  { key: "first", label: "First response (s)", points: analytics.firstResponse, tone: 1 },
                ]}
                formatValue={(value) => formatDuration(value)}
                formatLabel={(label) => label.slice(5)}
              />
            </CardBody>
          </Card>
        </Section>

        <Section title="SLA compliance">
          <Card>
            <CardBody className="space-y-4">
              {slaPolicies.map((policy) => (
                <div key={policy.id}>
                  <div className="mb-1.5 flex items-baseline justify-between gap-3">
                    <span className="text-sm text-fg">{policy.name}</span>
                    <span className="text-xs tabular text-fg-muted">
                      {policy.compliance_30d != null ? formatPercent(policy.compliance_30d, 1) : "—"}
                    </span>
                  </div>
                  <Progress
                    value={policy.compliance_30d ?? 0}
                    tone={(policy.compliance_30d ?? 0) >= 0.95 ? "success" : "warning"}
                    label={`${policy.name} compliance`}
                  />
                </div>
              ))}
            </CardBody>
          </Card>
        </Section>

        <div className="grid gap-4 lg:grid-cols-2">
          <Section title="Backlog">
            <Card>
              <CardHeader title="Open conversations over time" />
              <CardBody>
                <AreaChart
                  height={180}
                  series={[{ key: "backlog", label: "Backlog", points: analytics.backlog, tone: 5 }]}
                  formatLabel={(label) => label.slice(5)}
                />
              </CardBody>
            </Card>
          </Section>

          <Section title="Volume by inbox">
            <Card>
              <CardBody>
                <BarChart
                  horizontal
                  points={inboxes.map((inbox, index) => ({
                    t: inbox.name,
                    v: [1_420, 620, 380][index] ?? 100,
                  }))}
                  formatValue={(value) => formatCompact(value)}
                />
              </CardBody>
            </Card>
          </Section>
        </div>

        <Section
          title="Agent performance"
          description="Comparative, not evaluative — volume differences usually reflect routing, not effort."
        >
          <Card>
            <CardBody className="p-0">
              <DataTable
                aria-label="Agent performance"
                rows={AGENT_ROWS}
                columns={columns}
                rowKey={(row) => row.id}
              />
            </CardBody>
          </Card>
        </Section>
      </PageBody>
    </Page>
  );
}
