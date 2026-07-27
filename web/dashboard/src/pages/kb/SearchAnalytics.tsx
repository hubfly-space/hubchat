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
  formatCompact,
  formatPercent,
} from "@hubchat/shared";
import { FileWarning, Plus } from "lucide-react";
import { analytics } from "../../data/fixtures";

/**
 * Search analytics (§6.8).
 *
 * The most valuable table here is the failed-search list: it is a queue of
 * articles that should exist, ranked by demand. Deterministic, no AI — the
 * insight is simply "people asked this and got nothing".
 */
export default function SearchAnalytics() {
  return (
    <Page>
      <PageHeader
        title="Search analytics"
        description="What customers look for in the help centre, and where they come up empty."
      />

      <PageBody>
        <Section>
          <Card>
            <CardBody className="grid gap-6 sm:grid-cols-4">
              <Metric
                label="Searches"
                value={formatCompact(18_402)}
                delta={0.062}
                definition="Total help-centre and widget searches in the period."
              />
              <Metric
                label="Zero-result rate"
                value={formatPercent(0.084, 1)}
                delta={-0.021}
                higherIsBetter={false}
                definition="Share of searches that returned no article. Lower is better."
              />
              <Metric
                label="Click-through"
                value={formatPercent(0.612, 0)}
                delta={0.018}
                definition="Share of searches where the customer opened a result."
              />
              <Metric
                label="Deflection"
                value={formatPercent(0.284, 0)}
                delta={0.034}
                definition="Share of widget sessions that searched and then left without starting a conversation."
              />
            </CardBody>
          </Card>
        </Section>

        <Section title="Search volume">
          <Card>
            <CardBody>
              <AreaChart
                height={200}
                series={[{ key: "views", label: "Article views", points: analytics.articleViews, tone: 1 }]}
                formatLabel={(label) => label.slice(5)}
              />
            </CardBody>
          </Card>
        </Section>

        <div className="grid gap-4 lg:grid-cols-2">
          <Section title="Most viewed articles">
            <Card>
              <CardBody>
                <BarChart horizontal points={analytics.topArticles} />
              </CardBody>
            </Card>
          </Section>

          <Section title="Searches with no result">
            <Callout tone="warning" className="mb-3" icon={<FileWarning />}>
              Each of these is a customer who asked and got nothing back. Writing the top entry is
              usually the highest-leverage hour in a support week.
            </Callout>
            <Card>
              <CardHeader
                title="Top failed queries"
                actions={
                  <Button variant="ghost" size="xs" leading={<Plus />}>
                    Write article
                  </Button>
                }
              />
              <CardBody className="p-0">
                <ul className="divide-y divide-line-subtle">
                  {analytics.noResultSearches.map((entry) => (
                    <li key={entry.t} className="flex items-center gap-3 px-4 py-2.5">
                      <span className="min-w-0 flex-1 truncate font-mono text-xs text-fg-secondary">
                        “{entry.t}”
                      </span>
                      <span className="shrink-0 text-xs tabular text-fg-muted">{entry.v}×</span>
                      <Button variant="ghost" size="xs">
                        Write
                      </Button>
                    </li>
                  ))}
                </ul>
              </CardBody>
            </Card>
          </Section>
        </div>
      </PageBody>
    </Page>
  );
}
