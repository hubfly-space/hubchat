import {
  AreaChart,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  DonutChart,
  HeatMap,
  Metric,
  Page,
  PageBody,
  PageHeader,
  Section,
  SegmentedControl,
  formatCompact,
  formatDuration,
  formatPercent,
} from "@hubchat/shared";
import { CalendarClock, Download, Info } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { analytics } from "../../data/fixtures";

export const RANGES = [
  { value: "7d", label: "7 days" },
  { value: "30d", label: "30 days" },
  { value: "90d", label: "90 days" },
] as const;

const WEEKDAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const HOURS = ["00", "02", "04", "06", "08", "10", "12", "14", "16", "18", "20", "22"];

/** Reporting overview (§6.18). */
export default function ReportsOverview() {
  const [range, setRange] = useState<string>("30d");
  const days = range === "7d" ? 7 : range === "30d" ? 30 : 30;
  const slice = <T,>(points: T[]) => points.slice(-days);

  return (
    <Page>
      <PageHeader
        title="Reports"
        description="Deterministic aggregates over stored events. Every metric shows its definition."
        actions={
          <>
            <SegmentedControl
              aria-label="Date range"
              value={range}
              onValueChange={setRange}
              options={RANGES.map((item) => ({ value: item.value, label: item.label }))}
            />
            <Button variant="secondary" size="sm" leading={<CalendarClock />}>
              Schedule
            </Button>
            <Button variant="secondary" size="sm" leading={<Download />}>
              Export CSV
            </Button>
          </>
        }
      />

      <PageBody>
        <Callout tone="info" icon={<Info />} className="mb-5">
          All figures are computed in the workspace timezone (Europe/Lisbon) and, where the metric
          is duration-based, counted against business hours rather than wall-clock time.
        </Callout>

        <Section title="Headline">
          <Card>
            <CardBody className="grid gap-6 sm:grid-cols-2 xl:grid-cols-4">
              <Metric
                label="Conversations"
                value={formatCompact(slice(analytics.conversations).reduce((s, p) => s + p.v, 0))}
                delta={0.082}
                sparkline={slice(analytics.conversations)}
                definition="Conversations created in the period, across every channel."
              />
              <Metric
                label="Median first response"
                value={formatDuration(analytics.firstResponse.at(-1)?.v ?? 0)}
                delta={-0.114}
                higherIsBetter={false}
                definition="Time from the first customer message to the first public agent reply, in business hours."
              />
              <Metric
                label="SLA compliance"
                value={formatPercent(0.912, 1)}
                delta={0.024}
                definition="Share of conversations that met every SLA target that applied to them."
              />
              <Metric
                label="Satisfaction"
                value={formatPercent((analytics.csat.at(-1)?.v ?? 0) / 100)}
                delta={0.015}
                sparkline={slice(analytics.csat)}
                definition="Share of CSAT responses rating 4 or 5 out of 5."
              />
            </CardBody>
          </Card>
        </Section>

        <Section title="Volume and backlog">
          <Card>
            <CardBody>
              <AreaChart
                height={220}
                series={[
                  { key: "conversations", label: "Conversations", points: slice(analytics.conversations), tone: 1 },
                  { key: "tickets", label: "Tickets", points: slice(analytics.tickets), tone: 2 },
                  { key: "backlog", label: "Backlog", points: slice(analytics.backlog), tone: 3, reference: true },
                ]}
                formatLabel={(label) => label.slice(5)}
              />
            </CardBody>
          </Card>
        </Section>

        <div className="grid gap-4 lg:grid-cols-2">
          <Section title="When contacts arrive">
            <Card>
              <CardHeader
                title="Volume by weekday and hour"
                description="Workspace timezone. Use this to place shift boundaries."
              />
              <CardBody>
                <HeatMap
                  data={analytics.heatmap}
                  rowLabels={WEEKDAYS}
                  columnLabels={HOURS}
                />
              </CardBody>
            </Card>
          </Section>

          <Section title="Channel mix">
            <Card>
              <CardHeader title="Where contacts arrive" />
              <CardBody>
                <DonutChart
                  segments={analytics.channelSplit}
                  centerValue={formatCompact(
                    analytics.channelSplit.reduce((sum, segment) => sum + segment.value, 0),
                  )}
                  centerLabel="contacts"
                />
              </CardBody>
            </Card>
          </Section>
        </div>

        <Section title="Go deeper">
          <div className="grid gap-3 sm:grid-cols-3">
            {[
              { to: "/reports/support", title: "Support operations", detail: "Response and resolution times, backlog, SLA compliance, agent workload." },
              { to: "/reports/experience", title: "Customer experience", detail: "Satisfaction, effort, recommendation, repeat contacts, article helpfulness." },
              { to: "/reports/surfaces", title: "Widget & portal", detail: "Impressions, opens, conversation starts, form submissions, deflection." },
            ].map((report) => (
              <Card key={report.to} interactive className="p-0">
                <Link to={report.to} className="block p-4">
                  <p className="text-sm font-medium text-fg">{report.title}</p>
                  <p className="mt-1 text-xs leading-normal text-fg-muted">{report.detail}</p>
                </Link>
              </Card>
            ))}
          </div>
        </Section>
      </PageBody>
    </Page>
  );
}
