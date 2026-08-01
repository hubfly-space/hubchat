import { ApiError, Avatar, Badge, BarChart, Button, Card, CardBody, CardHeader, EmptyState, Metric, Page, PageBody, PageHeader, Pagination, Section, Tabs, TabsContent, TabsList, api, formatDate, formatPercent, useInfinite, useQuery } from "@hubchat/shared";
import { Download, Star } from "lucide-react";
import { useState } from "react";
import { useParams } from "react-router-dom";
import type { Paginated } from "@hubchat/shared";

type Question = { id: string; prompt: string; type: string; options: string[]; required: boolean };
type LiveSurvey = { id: string; name: string; type: string; questions: Question[]; delivery: string[]; trigger: Record<string, unknown>; response_count: number; sent_count: number; average_score: number | null; response_rate: number | null; enabled: boolean };
type Response = { id: string; customer_id: string | null; score: number | null; answers: Record<string, unknown>; comment: string; submitted_at: string };
type Summary = { response_count: number; average_score: number | null; nps: number | null; comment_count: number; distribution: Record<string, number> };

export default function SurveyDetail() {
  const { surveyId } = useParams();
  const [tab, setTab] = useState("summary");
  const survey = useQuery<LiveSurvey>(["survey", surveyId], (signal) => api.get("/surveys/" + encodeURIComponent(surveyId ?? ""), { signal }), { enabled: Boolean(surveyId) });
  const responses = useInfinite<Response>(
    ["survey-responses", surveyId],
    (cursor, signal) => api.get<Paginated<Response>>("/surveys/" + encodeURIComponent(surveyId ?? "") + "/responses?limit=25" + (cursor ? "&cursor=" + encodeURIComponent(cursor) : ""), { signal }),
    { enabled: Boolean(surveyId) },
  );
  const summary = useQuery<Summary>(["survey-summary", surveyId], (signal) => api.get("/surveys/" + encodeURIComponent(surveyId ?? "") + "/summary", { signal }), { enabled: Boolean(surveyId) });

  if (survey.isLoading) return <Page><PageBody><p className="text-sm text-fg-muted">Loading survey…</p></PageBody></Page>;
  if (survey.error || !survey.data) return <Page><PageBody><EmptyState icon={Star} size="lg" title="Survey not found" description={survey.error instanceof ApiError ? survey.error.message : "This survey is unavailable."} /></PageBody></Page>;

  const current = survey.data;
  const distribution = Object.entries(summary.data?.distribution ?? {}).map(([score, value]) => ({ t: score, v: value, tone: (Number(score) >= 4 ? 4 : Number(score) === 3 ? 5 : 6) as 4 | 5 | 6 }));
  return <Page>
    <PageHeader
      breadcrumbs={[{ label: "Surveys", href: "/surveys" }, { label: current.name }]}
      title={current.name}
      meta={<Badge tone={current.enabled ? "success" : "warning"}>{current.enabled ? "Live" : "Paused"}</Badge>}
      description={"Delivered via " + (current.delivery.join(", ") || "manual link") + "."}
      actions={<Button variant="secondary" size="sm" leading={<Download />} onClick={() => window.open("/api/v1/surveys/" + encodeURIComponent(current.id) + "/responses.csv", "_blank")}>Export CSV</Button>}
      tabs={<Tabs value={tab} onValueChange={setTab}><TabsList items={[{ value: "summary", label: "Summary" }, { value: "responses", label: "Responses", count: responses.items.length }, { value: "questions", label: "Questions", count: current.questions.length }]} /></Tabs>}
    />
    <PageBody><Tabs value={tab} onValueChange={setTab}>
      <TabsContent value="summary"><Section><Card><CardBody className="grid gap-6 sm:grid-cols-2 xl:grid-cols-5"><Metric label="Responses" value={String(summary.data?.response_count ?? current.response_count)} definition="Completed submissions recorded for this survey." /><Metric label="Average score" value={summary.data?.average_score?.toFixed(2) ?? "—"} definition="Mean of the primary numeric score." /><Metric label="NPS" value={summary.data?.nps != null ? summary.data.nps.toFixed(0) : "—"} definition="Promoters minus detractors, on the standard -100 to 100 NPS scale." /><Metric label="Response rate" value={current.response_rate != null ? formatPercent(current.response_rate) : "—"} definition="Completed responses divided by surveys delivered." /><Metric label="With comments" value={String(summary.data?.comment_count ?? 0)} definition="Responses that included free text." /></CardBody></Card></Section><Section title="Score distribution"><Card><CardBody>{distribution.length ? <BarChart horizontal points={distribution} /> : <p className="py-8 text-center text-sm text-fg-muted">No scored responses yet.</p>}</CardBody></Card></Section></TabsContent>
      <TabsContent value="responses"><Card><CardBody className="p-0">{responses.isLoading ? <p className="p-4 text-sm text-fg-muted">Loading responses…</p> : responses.error ? <div className="p-4"><p className="text-sm text-danger">Could not load responses.</p><Button className="mt-2" variant="secondary" size="sm" onClick={responses.refetch}>Try again</Button></div> : responses.items.length === 0 ? <p className="p-8 text-center text-sm text-fg-muted">No responses yet.</p> : <><ul className="divide-y divide-line-subtle">{responses.items.map((response) => <li key={response.id} className="flex gap-3 px-4 py-3"><Avatar name={response.customer_id ?? "Anonymous"} seed={response.customer_id ?? response.id} size="sm" /><div className="min-w-0 flex-1"><p className="text-xs font-medium text-fg">{response.customer_id ?? "Anonymous response"}</p>{response.comment ? <p className="mt-1 text-sm leading-normal text-fg-secondary">{response.comment}</p> : <p className="mt-1 text-sm italic text-fg-disabled">No comment left</p>}<p className="mt-1 text-2xs text-fg-muted">{response.submitted_at ? formatDate(response.submitted_at) : "Unknown date"}</p></div><span className="text-sm font-semibold tabular text-fg">{response.score ?? "—"}</span></li>)}</ul><Pagination hasPrevious={false} hasNext={responses.hasMore} onPrevious={() => undefined} onNext={() => void responses.fetchNext()} summary={responses.items.length + " response" + (responses.items.length === 1 ? "" : "s") + " loaded"} /></>}</CardBody></Card></TabsContent>
      <TabsContent value="questions"><Card><CardHeader title="Questions" description="Shown in order. Required answers are validated by the public response endpoint." /><CardBody className="p-0"><ol className="divide-y divide-line-subtle">{current.questions.map((question, index) => <li key={question.id} className="flex items-start gap-3 px-4 py-3"><span className="grid size-5 shrink-0 place-items-center rounded-full bg-fill text-2xs font-semibold text-fg-muted">{index + 1}</span><div><p className="text-sm text-fg">{question.prompt}</p><p className="mt-0.5 text-xs text-fg-muted">{question.type.replace(/_/g, " ")}{question.required ? " · required" : " · optional"}</p></div></li>)}{current.questions.length === 0 && <li className="px-4 py-8 text-center text-xs text-fg-muted">No questions configured.</li>}</ol></CardBody></Card></TabsContent>
    </Tabs></PageBody>
  </Page>;
}
