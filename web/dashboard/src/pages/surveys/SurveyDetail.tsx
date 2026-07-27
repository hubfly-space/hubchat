import {
  Avatar,
  BarChart,
  Badge,
  Button,
  Card,
  CardBody,
  CardHeader,
  DonutChart,
  EmptyState,
  Metric,
  Page,
  PageBody,
  PageHeader,
  Section,
  Tabs,
  TabsContent,
  TabsList,
  cn,
  formatCompact,
  formatPercent,
} from "@hubchat/shared";
import { Download, Star } from "lucide-react";
import { useState } from "react";
import { useParams } from "react-router-dom";
import { surveys } from "../../data/fixtures";

const RESPONSES = [
  { id: "r1", name: "Mariana Costa", score: 5, comment: "Ada found the root cause in under an hour. Exactly what we needed.", agent: "Ada Mwangi" },
  { id: "r2", name: "Hannah Weiss", score: 4, comment: "Quick and clear, though I had to explain the setup twice.", agent: "Sara Lindqvist" },
  { id: "r3", name: "Luca Bianchi", score: 2, comment: "Took two days to get an answer on a trial-blocking issue.", agent: "Unassigned" },
  { id: "r4", name: "Daniel Osei", score: 5, comment: "", agent: "Rui Ferreira" },
  { id: "r5", name: "Yuki Tanaka", score: 3, comment: "The answer was right but the docs should have covered it.", agent: "Rui Ferreira" },
];

export default function SurveyDetail() {
  const { surveyId } = useParams();
  const [tab, setTab] = useState("summary");

  const survey = surveys.find((item) => item.id === surveyId);

  if (!survey) {
    return (
      <Page>
        <EmptyState icon={Star} size="lg" title="Survey not found" />
      </Page>
    );
  }

  const distribution = [5, 4, 3, 2, 1].map((score) => ({
    t: `${score}★`,
    v: RESPONSES.filter((response) => response.score === score).length * 90 + score * 12,
    tone: (score >= 4 ? 4 : score === 3 ? 5 : 6) as 4 | 5 | 6,
  }));

  return (
    <Page>
      <PageHeader
        breadcrumbs={[{ label: "Surveys", href: "/surveys" }, { label: survey.name }]}
        title={survey.name}
        meta={<Badge tone={survey.enabled ? "success" : "warning"}>{survey.enabled ? "Live" : "Paused"}</Badge>}
        description={`Sent ${survey.trigger.replace(/_/g, " ")} via ${survey.delivery.join(", ")}.`}
        actions={
          <Button variant="secondary" size="sm" leading={<Download />}>
            Export CSV
          </Button>
        }
        tabs={
          <Tabs value={tab} onValueChange={setTab}>
            <TabsList
              items={[
                { value: "summary", label: "Summary" },
                { value: "responses", label: "Responses", count: RESPONSES.length },
                { value: "questions", label: "Questions", count: survey.questions.length },
              ]}
            />
          </Tabs>
        }
      />

      <PageBody>
        <Tabs value={tab} onValueChange={setTab}>
          <TabsContent value="summary">
            <Section>
              <Card>
                <CardBody className="grid gap-6 sm:grid-cols-4">
                  <Metric
                    label="Responses"
                    value={formatCompact(survey.response_count)}
                    delta={0.11}
                    definition="Completed submissions in the selected period."
                  />
                  <Metric
                    label="Average score"
                    value={survey.average_score ?? "—"}
                    delta={0.02}
                    definition="Mean of the primary rating question."
                  />
                  <Metric
                    label="Response rate"
                    value={survey.response_rate != null ? formatPercent(survey.response_rate) : "—"}
                    delta={-0.008}
                    definition="Completed responses divided by surveys delivered."
                  />
                  <Metric
                    label="With comments"
                    value={formatPercent(0.42)}
                    definition="Share of responses that included free text."
                  />
                </CardBody>
              </Card>
            </Section>

            <div className="grid gap-4 lg:grid-cols-2">
              <Section title="Score distribution">
                <Card>
                  <CardBody>
                    <BarChart horizontal points={distribution} />
                  </CardBody>
                </Card>
              </Section>

              <Section title="By channel">
                <Card>
                  <CardBody>
                    <DonutChart
                      segments={[
                        { key: "widget", label: "Widget", value: 720, tone: 1 },
                        { key: "email", label: "Email", value: 402, tone: 2 },
                        { key: "portal", label: "Portal", value: 162, tone: 3 },
                      ]}
                      centerValue={formatCompact(1284)}
                      centerLabel="responses"
                    />
                  </CardBody>
                </Card>
              </Section>
            </div>
          </TabsContent>

          <TabsContent value="responses">
            <Card>
              <CardBody className="p-0">
                <ul className="divide-y divide-line-subtle">
                  {RESPONSES.map((response) => (
                    <li key={response.id} className="flex gap-3 px-4 py-3">
                      <Avatar name={response.name} size="sm" />
                      <div className="min-w-0 flex-1">
                        <p className="flex items-center gap-2 text-xs">
                          <span className="font-medium text-fg">{response.name}</span>
                          <span className="text-fg-muted">· handled by {response.agent}</span>
                        </p>
                        {response.comment ? (
                          <p className="mt-1 text-sm leading-normal text-fg-secondary">
                            {response.comment}
                          </p>
                        ) : (
                          <p className="mt-1 text-sm italic text-fg-disabled">No comment left</p>
                        )}
                      </div>
                      <span className="flex shrink-0 gap-0.5" aria-label={`${response.score} out of 5`}>
                        {[1, 2, 3, 4, 5].map((star) => (
                          <Star
                            key={star}
                            aria-hidden="true"
                            className={cn(
                              "size-3.5",
                              star <= response.score
                                ? "fill-warning text-warning"
                                : "text-line-loud",
                            )}
                          />
                        ))}
                      </span>
                    </li>
                  ))}
                </ul>
              </CardBody>
            </Card>
          </TabsContent>

          <TabsContent value="questions">
            <Card>
              <CardHeader title="Questions" description="Shown in order. Conditional questions appear only when their condition passes." />
              <CardBody className="p-0">
                <ol className="divide-y divide-line-subtle">
                  {survey.questions.map((question, index) => (
                    <li key={question.id} className="flex items-start gap-3 px-4 py-3">
                      <span className="grid size-5 shrink-0 place-items-center rounded-full bg-fill text-2xs font-semibold text-fg-muted">
                        {index + 1}
                      </span>
                      <div className="min-w-0 flex-1">
                        <p className="text-sm text-fg">{question.prompt}</p>
                        <p className="mt-0.5 text-xs text-fg-muted">
                          {question.type.replace(/_/g, " ")}
                          {question.required ? " · required" : " · optional"}
                        </p>
                      </div>
                    </li>
                  ))}
                  {survey.questions.length === 0 && (
                    <li className="px-4 py-8 text-center text-xs text-fg-muted">
                      No questions configured yet.
                    </li>
                  )}
                </ol>
              </CardBody>
            </Card>
          </TabsContent>
        </Tabs>
      </PageBody>
    </Page>
  );
}
