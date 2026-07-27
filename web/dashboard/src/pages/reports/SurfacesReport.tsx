import {
  AreaChart,
  BarChart,
  Button,
  Card,
  CardBody,
  CardHeader,
  DonutChart,
  Metric,
  Page,
  PageBody,
  PageHeader,
  Section,
  SegmentedControl,
  formatCompact,
  formatPercent,
} from "@hubchat/shared";
import { Download } from "lucide-react";
import { useState } from "react";
import { analytics } from "../../data/fixtures";
import { RANGES } from "./ReportsOverview";

/** Widget and portal report (§6.18). */
export default function SurfacesReport() {
  const [range, setRange] = useState<string>("30d");

  return (
    <Page>
      <PageHeader
        title="Widget & portal"
        description="How your customer-facing surfaces are used, and how much they deflect."
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
        <Section title="Widget funnel">
          <Card>
            <CardBody className="grid gap-6 sm:grid-cols-2 xl:grid-cols-4">
              <Metric
                label="Impressions"
                value={formatCompact(184_200)}
                delta={0.041}
                definition="Page loads on which the widget loader ran and the launcher was eligible to show."
              />
              <Metric
                label="Opens"
                value={formatCompact(analytics.widgetOpens.at(-1)?.v ?? 0)}
                delta={0.078}
                sparkline={analytics.widgetOpens}
                definition="Times a visitor opened the widget panel."
              />
              <Metric
                label="Conversation starts"
                value={formatCompact(2_140)}
                delta={0.021}
                definition="Widget sessions in which the visitor sent at least one message."
              />
              <Metric
                label="Self-service deflection"
                value={formatPercent(0.284)}
                delta={0.034}
                definition="Sessions that opened an article and closed without starting a conversation."
              />
            </CardBody>
          </Card>
        </Section>

        <Section title="Opens over time">
          <Card>
            <CardBody>
              <AreaChart
                height={200}
                series={[
                  { key: "opens", label: "Widget opens", points: analytics.widgetOpens, tone: 1 },
                  { key: "articles", label: "Article views", points: analytics.articleViews, tone: 2 },
                ]}
                formatLabel={(label) => label.slice(5)}
              />
            </CardBody>
          </Card>
        </Section>

        <div className="grid gap-4 lg:grid-cols-2">
          <Section title="Identified vs anonymous">
            <Card>
              <CardHeader
                title="Who is using the widget"
                description="A high anonymous share usually means the identity token is not being issued on authenticated pages."
              />
              <CardBody>
                <DonutChart
                  segments={[
                    { key: "verified", label: "Verified identity", value: 1_420, tone: 1 },
                    { key: "unverified", label: "Self-reported", value: 380, tone: 2 },
                    { key: "anonymous", label: "Anonymous", value: 340, tone: 3 },
                  ]}
                  centerValue={formatCompact(2_140)}
                  centerLabel="sessions"
                />
              </CardBody>
            </Card>
          </Section>

          <Section title="Top entry pages">
            <Card>
              <CardBody>
                <BarChart
                  horizontal
                  points={[
                    { t: "/billing/invoices", v: 612 },
                    { t: "/settings/team", v: 448 },
                    { t: "/pricing", v: 390 },
                    { t: "/dashboard", v: 284 },
                    { t: "/integrations", v: 176 },
                  ]}
                />
              </CardBody>
            </Card>
          </Section>
        </div>

        <Section title="Portal">
          <Card>
            <CardBody className="grid gap-6 sm:grid-cols-4">
              <Metric
                label="Portal sign-ins"
                value={formatCompact(3_840)}
                delta={0.062}
                definition="Successful customer authentications on any portal."
              />
              <Metric
                label="Tickets submitted"
                value={formatCompact(604)}
                delta={0.018}
                definition="Tickets created through a portal form."
              />
              <Metric
                label="Form submissions"
                value={formatCompact(429)}
                delta={0.033}
                definition="Submissions across every standalone and embedded form."
              />
              <Metric
                label="Article views"
                value={formatCompact(analytics.articleViews.reduce((s, p) => s + p.v, 0))}
                delta={0.052}
                definition="Article page views across the portal and the widget."
              />
            </CardBody>
          </Card>
        </Section>
      </PageBody>
    </Page>
  );
}
