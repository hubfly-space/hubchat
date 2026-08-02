import { ApiError, Button, Card, CardBody, EmptyState, Field, Textarea, api, formConditionApplies, idempotencyKey, useMutation, useQuery } from "@hubchat/shared";
import { CheckCircle2, FileQuestion, Star } from "lucide-react";
import { useState } from "react";
import { useParams, useSearchParams } from "react-router-dom";
import { portalText } from "../i18n";
import { usePortal } from "../portal-context";

type Question = { id: string; prompt: string; type: string; options?: string[]; required: boolean; condition?: Record<string, unknown> };
type SurveyRecord = { id: string; name: string; type: string; questions: Question[]; completion: Record<string, unknown>; enabled: boolean };

function questionVisible(question: Question, index: number, questions: Question[], answers: Record<string, string>) {
  const condition = question.condition;
  if (!condition || Object.keys(condition).length === 0) return true;
  const rawIndex = condition.question_index ?? condition.question;
  if (rawIndex !== undefined) {
    const reference = typeof rawIndex === "number" ? rawIndex : Number(rawIndex);
    if (!Number.isInteger(reference) || reference < 0 || reference >= index || !questions[reference]) return false;
    return formConditionApplies({ ...condition, field: questions[reference].id }, answers as Record<string, unknown>);
  }
  return formConditionApplies(condition, answers as Record<string, unknown>);
}

function answerValue(value: string, type: string): string | number | boolean {
  if (type === "number" || type === "star" || type === "stars") return Number(value);
  if (type === "boolean") return value === "true";
  return value;
}

export default function Survey() {
  const { workspaceID, surveyID } = useParams();
  const [params] = useSearchParams();
  const token = params.get("token") ?? "";
  const { data: portal } = usePortal();
  const t = (key: string, fallback: string) => portalText(portal, key, fallback);
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const [submitted, setSubmitted] = useState(false);
  const query = useQuery<SurveyRecord>(
    ["public-survey", workspaceID ?? "", surveyID ?? ""],
    (signal) => api.get(`/public/surveys/${encodeURIComponent(workspaceID ?? "")}/${encodeURIComponent(surveyID ?? "")}`, { signal }),
    { enabled: Boolean(workspaceID && surveyID) },
  );
  const submit = useMutation<{ answers: Record<string, unknown>; token: string }, unknown>(
    (input) => api.post(`/public/surveys/${encodeURIComponent(workspaceID ?? "")}/${encodeURIComponent(surveyID ?? "")}/responses`, input, { idempotencyKey: idempotencyKey() }),
    { onSuccess: () => setSubmitted(true) },
  );
  const survey = query.data;

  if (query.isLoading) return <p className="mx-auto max-w-lg py-16 text-center text-sm text-fg-muted">{t("loading_survey", "Loading survey…")}</p>;
  if (query.error || !survey) return <EmptyState icon={FileQuestion} size="lg" title={t("survey_unavailable", "Survey unavailable")} description={query.error instanceof ApiError ? query.error.message : t("survey_expired", "This survey may have expired or been removed.")} />;
  if (submitted) {
    const message = typeof survey.completion?.message === "string" ? survey.completion.message : t("survey_thanks", "Thank you for sharing your feedback. Your response has been recorded.");
    return <div className="mx-auto max-w-lg py-16 text-center"><div className="mx-auto mb-4 grid size-12 place-items-center rounded-xl border border-success-border bg-success-subtle"><CheckCircle2 aria-hidden="true" className="size-6 text-success-text" /></div><h1 className="text-2xl font-semibold tracking-tight text-fg">{t("thank_you", "Thank you")}</h1><p className="mt-2 text-sm leading-relaxed text-fg-muted">{message}</p></div>;
  }

  return <div className="mx-auto max-w-lg py-8 sm:py-14"><header className="mb-7 text-center"><div className="mx-auto mb-3 grid size-10 place-items-center rounded-xl border border-accent-border bg-accent-subtle"><Star aria-hidden="true" className="size-5 text-accent-text" /></div><h1 className="text-2xl font-semibold tracking-tight text-fg">{survey.name}</h1><p className="mt-2 text-sm text-fg-muted">{t("survey_intro", "A short question about your support experience.")}</p></header><Card><CardBody className="space-y-6"><form className="space-y-6" onSubmit={(event) => { event.preventDefault(); const payload = Object.fromEntries(survey.questions.flatMap((question, index) => !questionVisible(question, index, survey.questions, answers) || answers[question.id] === undefined ? [] : [[question.id, answerValue(answers[question.id]!, question.type)]])); void submit.mutate({ answers: payload, token }).catch(() => {}); }}>
    {survey.questions.filter((question, index) => questionVisible(question, index, survey.questions, answers)).map((question) => <Field key={question.id} label={question.prompt} required={question.required}>
      {question.type === "text" ? <Textarea rows={4} value={answers[question.id] ?? ""} onChange={(event) => setAnswers((current) => ({ ...current, [question.id]: event.target.value }))} /> : question.type === "choice" ? <select className="h-10 w-full rounded-md border border-line bg-surface px-3 text-sm text-fg" required={question.required} value={answers[question.id] ?? ""} onChange={(event) => setAnswers((current) => ({ ...current, [question.id]: event.target.value }))}><option value="">{t("choose_one", "Choose one…")}</option>{(question.options ?? []).map((option) => <option key={option} value={option}>{option}</option>)}</select> : question.type === "boolean" ? <select className="h-10 w-full rounded-md border border-line bg-surface px-3 text-sm text-fg" required={question.required} value={answers[question.id] ?? ""} onChange={(event) => setAnswers((current) => ({ ...current, [question.id]: event.target.value }))}><option value="">{t("choose_one", "Choose one…")}</option><option value="true">{t("yes", "Yes")}</option><option value="false">{t("no", "No")}</option></select> : <div className="flex flex-wrap gap-2">{Array.from({ length: question.type === "number" ? 11 : 5 }, (_, index) => { const value = question.type === "number" ? index : index + 1; return <button key={value} type="button" aria-label={`${value}`} aria-pressed={answers[question.id] === String(value)} onClick={() => setAnswers((current) => ({ ...current, [question.id]: String(value) }))} className={`grid size-10 place-items-center rounded-md border text-sm transition-colors ${answers[question.id] === String(value) ? "border-accent bg-accent text-accent-fg" : "border-line text-fg-secondary hover:border-line-strong hover:bg-fill"}`}>{value}</button>; })}</div>}
    </Field>)}
    {Boolean(submit.error) && <p className="text-sm text-danger">{submit.error instanceof ApiError ? submit.error.message : t("survey_submit_error", "Could not record your response. Please try again.")}</p>}
    <Button type="submit" variant="primary" size="lg" fullWidth loading={submit.isPending}>{t("submit_response", "Submit response")}</Button>
  </form></CardBody></Card></div>;
}
