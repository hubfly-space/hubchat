import {
  ApiError,
  Badge,
  Button,
  Card,
  CardBody,
  Dialog,
  DialogContent,
  DialogTrigger,
  EmptyState,
  Field,
  Input,
  Page,
  PageBody,
  PageHeader,
  Pagination,
  Section,
  Switch,
  api,
  idempotencyKey,
  useInfinite,
  useMutation,
} from "@hubchat/shared";
import { Plus, Star } from "lucide-react";
import { Link } from "react-router-dom";
import { useState } from "react";
import type { Paginated } from "@hubchat/shared";

type LiveSurvey = {
  id: string;
  name: string;
  type: string;
  delivery: string[];
  trigger: Record<string, unknown>;
  response_count: number;
  sent_count: number;
  average_score: number | null;
  response_rate: number | null;
  enabled: boolean;
  expires_at: string | null;
};

const TYPE_LABEL: Record<string, string> = { csat: "Satisfaction", ces: "Effort score", nps: "Recommendation", custom: "Custom" };

export default function SurveyList() {
  const query = useInfinite<LiveSurvey>(
    ["surveys"],
    (cursor, signal) => api.get<Paginated<LiveSurvey>>("/surveys?limit=25" + (cursor ? "&cursor=" + encodeURIComponent(cursor) : ""), { signal }),
  );
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [type, setType] = useState("csat");
  const create = useMutation<{ name: string; type: string; delivery: string[]; questions: Array<{ prompt: string; type: string; required: boolean }> }, LiveSurvey>(
    (input) => api.post("/surveys", input, { idempotencyKey: idempotencyKey() }),
    { invalidates: [["surveys"]], onSuccess: () => { setOpen(false); setName(""); } },
  );
  const toggle = useMutation<{ id: string; enabled: boolean }, LiveSurvey>(
    ({ id, enabled }) => api.patch("/surveys/" + encodeURIComponent(id), { enabled }),
    { invalidates: [["surveys"]] },
  );

  return (
    <Page>
      <PageHeader
        title="Surveys"
        description="Satisfaction, effort, and recommendation scores. Results are aggregated deterministically — no automated interpretation."
        actions={
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild><Button variant="primary" size="sm" leading={<Plus />}>New survey</Button></DialogTrigger>
            <DialogContent
              title="Create survey"
              footer={<><Button variant="ghost" size="sm" onClick={() => setOpen(false)}>Cancel</Button><Button variant="primary" size="sm" loading={create.isPending} disabled={!name.trim()} onClick={() => void create.mutate({ name: name.trim(), type, delivery: ["email", "portal"], questions: [{ prompt: type === "nps" ? "How likely are you to recommend us?" : "How would you rate your experience?", type: type === "nps" ? "number" : "star", required: true }] }).catch(() => {})}>Create survey</Button></>}
            >
              <div className="space-y-4">
                <Field label="Name"><Input autoFocus value={name} onChange={(event) => setName(event.target.value)} placeholder="Post-resolution satisfaction" /></Field>
                <Field label="Type"><select className="h-9 w-full rounded-md border border-line bg-surface px-3 text-sm text-fg" value={type} onChange={(event) => setType(event.target.value)}><option value="csat">Satisfaction (CSAT)</option><option value="ces">Effort (CES)</option><option value="nps">Recommendation (NPS)</option><option value="custom">Custom</option></select></Field>
                {Boolean(create.error) && <p className="text-sm text-danger">Could not create survey.</p>}
              </div>
            </DialogContent>
          </Dialog>
        }
      />
      <PageBody>
        <Section>
          {query.isLoading ? <p className="text-sm text-fg-muted">Loading surveys…</p> : query.error ? <EmptyState icon={Star} title="Surveys unavailable" description={query.error instanceof ApiError ? query.error.message : "Try again in a moment."} action={<Button variant="secondary" size="sm" onClick={query.refetch}>Try again</Button>} /> : query.items.length === 0 ? <EmptyState icon={Star} title="No surveys yet" description="Ask one question after resolution and learn directly from customers." /> : <div className="space-y-3">{query.items.map((survey) => <Card key={survey.id}><CardBody className="flex flex-wrap items-center gap-4"><div className="min-w-0 flex-1"><div className="flex items-center gap-2"><Link to={"/surveys/" + survey.id} className="truncate text-sm font-medium text-fg hover:underline">{survey.name}</Link><Badge tone="neutral">{TYPE_LABEL[survey.type] ?? survey.type}</Badge>{!survey.enabled && <Badge tone="warning">Paused</Badge>}</div><p className="mt-1 text-xs text-fg-muted">Delivered via {survey.delivery.join(", ") || "not configured"} · {Object.keys(survey.trigger).length ? "triggered" : "after resolution"}</p></div><dl className="flex gap-6"><div><dt className="text-2xs text-fg-muted">Responses</dt><dd className="text-sm font-semibold tabular text-fg">{survey.response_count}</dd></div><div><dt className="text-2xs text-fg-muted">Average</dt><dd className="text-sm font-semibold tabular text-fg">{survey.average_score?.toFixed(2) ?? "—"}</dd></div><div><dt className="text-2xs text-fg-muted">Response rate</dt><dd className="text-sm font-semibold tabular text-fg">{survey.response_rate != null ? Math.round(survey.response_rate * 100) + "%" : "—"}</dd></div></dl><Switch checked={survey.enabled} onCheckedChange={(enabled) => void toggle.mutate({ id: survey.id, enabled }).catch(() => {})} aria-label={(survey.enabled ? "Disable " : "Enable ") + survey.name} /></CardBody></Card>)}</div>}
          <Pagination hasPrevious={false} hasNext={query.hasMore} onPrevious={() => undefined} onNext={() => void query.fetchNext()} summary={query.items.length + " survey" + (query.items.length === 1 ? "" : "s") + " loaded"} />
        </Section>
      </PageBody>
    </Page>
  );
}
