import {
  AreaChart,
  BarChart,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  Metric,
  Page,
  PageBody,
  PageHeader,
  Section,
  SegmentedControl,
  formatPercent,
} from "@hubchat/shared";
import { Download } from "lucide-react";
import { useState } from "react";
import { analytics } from "../../data/fixtures";
import { RANGES } from "./ReportsOverview";

/** Customer-experience report (§6.18). */
export default function ExperienceReport() {
  const [range, setRange] = useState<string>("30d");

  return (
    <Page>
      <PageHeader
        title="Customer experience"
        description="What customers reported, and how often they had to come back."
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
        <Callout tone="info" className="mb-5">
          Survey responses are aggregated and shown verbatim. Hubchat does not classify, summarise,
          or score free-text answers automatically — read them, or filter them.
        </Callout>

        <Section title="Scores">
          <Card>
            <CardBody className="grid gap-6 sm:grid-cols-2 xl:grid-cols-4">
              <Metric
                label="Satisfaction (CSAT)"
                value={formatPercent(0.92)}
                delta={0.015}
                sparkline={analytics.csat}
                definition="Share of responses rating 4 or 5 out of 5, on the post-resolution survey."
              />
              <Metric
                label="Effort (CES)"
                value="2.1"
                suffix="/ 7"
                delta={-0.06}
                higherIsBetter={false}
                definition="Mean answer to 'how much effort did you have to put in?' — lower is better."
              />
              <Metric
                label="Recommendation (NPS)"
                value="41"
                delta={0.09}
                definition="Promoters minus detractors, as a percentage of respondents."
              />
              <Metric
                label="Response rate"
                value={formatPercent(0.32)}
                delta={-0.008}
                definition="Completed survey responses divided by surveys delivered."
              />
            </CardBody>
          </Card>
        </Section>

        <Section title="Satisfaction over time">
          <Card>
            <CardBody>
              <AreaChart
                height={200}
                series={[{ key: "csat", label: "CSAT %", points: analytics.csat, tone: 4 }]}
                formatLabel={(label) => label.slice(5)}
                formatValue={(value) => `${value}%`}
              />
            </CardBody>
          </Card>
        </Section>

        <div className="grid gap-4 lg:grid-cols-2">
          <Section title="Contact friction">
            <Card>
              <CardBody className="grid gap-6 sm:grid-cols-2">
                <Metric
                  label="Repeat contact rate"
                  value={formatPercent(0.118, 1)}
                  delta={-0.014}
                  higherIsBetter={false}
                  definition="Share of customers who opened another conversation within 7 days of a resolution."
                />
                <Metric
                  label="Unresolved contacts"
                  value={formatPercent(0.031, 1)}
                  delta={0.004}
                  higherIsBetter={false}
                  definition="Conversations closed without ever reaching the resolved state."
                />
              </CardBody>
            </Card>
          </Section>

          <Section title="Article helpfulness">
            <Card>
              <CardHeader
                title="Most viewed"
                description="Views do not imply usefulness — pair with the helpfulness column in Articles."
              />
              <CardBody>
                <BarChart horizontal points={analytics.topArticles} />
              </CardBody>
            </Card>
          </Section>
        </div>
      </PageBody>
    </Page>
  );
}
