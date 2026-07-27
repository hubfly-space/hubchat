import {
  Badge,
  Button,
  Card,
  CardBody,
  EmptyState,
  Page,
  PageBody,
  PageHeader,
  Section,
  Switch,
  formatCompact,
  formatPercent,
} from "@hubchat/shared";
import { Plus, Star } from "lucide-react";
import { Link } from "react-router-dom";
import { surveys } from "../../data/fixtures";

const TYPE_LABEL = {
  csat: "Satisfaction",
  ces: "Effort score",
  nps: "Recommendation",
  custom: "Custom",
} as const;

/** Surveys (§6.7). */
export default function SurveyList() {
  return (
    <Page>
      <PageHeader
        title="Surveys"
        description="Satisfaction, effort, and recommendation scores. Results are aggregated deterministically — no automated interpretation."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />}>
            New survey
          </Button>
        }
      />

      <PageBody>
        <Section>
          {surveys.length === 0 ? (
            <EmptyState icon={Star} title="No surveys yet" description="Ask one question after resolution and you will learn more than from ten dashboards." />
          ) : (
            <div className="space-y-3">
              {surveys.map((survey) => (
                <Card key={survey.id}>
                  <CardBody className="flex flex-wrap items-center gap-4">
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <Link
                          to={`/surveys/${survey.id}`}
                          className="truncate text-sm font-medium text-fg hover:underline"
                        >
                          {survey.name}
                        </Link>
                        <Badge tone="neutral">{TYPE_LABEL[survey.type]}</Badge>
                        {!survey.enabled && <Badge tone="warning">Paused</Badge>}
                      </div>
                      <p className="mt-1 text-xs text-fg-muted">
                        Delivered via {survey.delivery.join(", ")} ·{" "}
                        {survey.trigger.replace(/_/g, " ")}
                      </p>
                    </div>

                    <dl className="flex gap-6">
                      <div>
                        <dt className="text-2xs text-fg-muted">Responses</dt>
                        <dd className="text-sm font-semibold tabular text-fg">
                          {formatCompact(survey.response_count)}
                        </dd>
                      </div>
                      <div>
                        <dt className="text-2xs text-fg-muted">
                          {survey.type === "nps" ? "NPS" : "Average"}
                        </dt>
                        <dd className="text-sm font-semibold tabular text-fg">
                          {survey.average_score ?? "—"}
                        </dd>
                      </div>
                      <div>
                        <dt className="text-2xs text-fg-muted">Response rate</dt>
                        <dd className="text-sm font-semibold tabular text-fg">
                          {survey.response_rate != null ? formatPercent(survey.response_rate) : "—"}
                        </dd>
                      </div>
                    </dl>

                    <Switch defaultChecked={survey.enabled} aria-label={`Enable ${survey.name}`} />
                  </CardBody>
                </Card>
              ))}
            </div>
          )}
        </Section>
      </PageBody>
    </Page>
  );
}
