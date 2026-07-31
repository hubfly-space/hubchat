import {
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CopyField,
  Dialog,
  DialogContent,
  EmptyState,
  Field,
  Input,
  Page,
  PageBody,
  PageHeader,
  Pagination,
  Section,
  api,
  idempotencyKey,
  formatCompact,
  formatRelativeShort,
  useMutation,
  useInfinite,
  type Paginated,
  ApiError,
} from "@hubchat/shared";
import { ClipboardList, Plus, Settings2 } from "lucide-react";
import { Link } from "react-router-dom";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";

type LiveForm = {
  id: string;
  name: string;
  slug: string;
  purpose: string;
  fields: unknown[];
  access: string;
  submission_count: number;
  enabled: boolean;
  updated_at: string;
};

type CreateForm = { name: string; slug: string; purpose: string; access: string; enabled: boolean; fields: unknown[] };

/** Intake forms (§6.11). */
export default function FormList() {
  const navigate = useNavigate();
  const { workspace } = useWorkspace();
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const query = useInfinite<LiveForm>(
    ["forms"],
    (cursor, signal) => {
      const params = new URLSearchParams({ limit: "50" });
      if (cursor) params.set("cursor", cursor);
      return api.get<Paginated<LiveForm>>(`/forms?${params.toString()}`, { signal });
    },
  );
  const create = useMutation<CreateForm, LiveForm>(
    (input) => api.post<LiveForm>("/forms", input, { idempotencyKey: idempotencyKey() }),
    {
      invalidates: [["forms"]],
      onSuccess: (form) => navigate(`/forms/${form.id}`),
    },
  );
  const forms = query.items;

  return (
    <Page>
      <PageHeader
        title="Forms"
        description="Reusable intake for bug reports, refunds, access requests, and anything else with a fixed shape."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />} onClick={() => setCreating(true)}>
            New form
          </Button>
        }
      />

      <PageBody>
        <Section>
          {query.isLoading ? <p className="py-12 text-center text-sm text-fg-muted">Loading forms…</p> : query.error ? (
            <div className="py-12 text-center text-sm text-danger">{query.error instanceof ApiError ? query.error.message : "Could not load forms."}<div><Button className="mt-4" variant="secondary" size="sm" onClick={query.refetch}>Try again</Button></div></div>
          ) : forms.length === 0 ? (
            <EmptyState
              icon={ClipboardList}
              title="No forms yet"
              description="A form turns a vague message into a structured ticket with the fields you actually need."
            />
          ) : (
            <div className="space-y-3">
              {forms.map((form) => (
                <Card key={form.id}>
                  <CardBody>
                    <div className="flex flex-wrap items-start gap-4">
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <Link
                            to={`/forms/${form.id}`}
                            className="truncate text-sm font-medium text-fg hover:underline"
                          >
                            {form.name}
                          </Link>
                          <Badge tone={form.access === "public" ? "success" : "neutral"}>
                            {form.access}
                          </Badge>
                          {!form.enabled && <Badge tone="warning">Disabled</Badge>}
                        </div>
                        <p className="mt-1 text-xs text-fg-muted">
                          Creates a {form.purpose} · {form.fields.length} fields ·{" "}
                          {formatCompact(form.submission_count)} submissions · updated{" "}
                          {formatRelativeShort(form.updated_at)} ago
                        </p>
                      </div>

                      <Button variant="secondary" size="sm" leading={<Settings2 />} asChild>
                        <Link to={`/forms/${form.id}`}>Edit</Link>
                      </Button>
                    </div>

                    <div className="mt-3 max-w-lg">
                      <CopyField
                        label="Public form endpoint"
                        value={`${window.location.origin}/api/v1/public/forms/${workspace.id}/${form.slug}`}
                      />
                    </div>
                  </CardBody>
                </Card>
              ))}
            </div>
          )}
        </Section>
      </PageBody>
      <Pagination
        hasPrevious={false}
        hasNext={query.hasMore}
        onPrevious={() => undefined}
        onNext={() => void query.fetchNext()}
        summary={`${forms.length} form${forms.length === 1 ? "" : "s"} loaded`}
      />
      <Dialog open={creating} onOpenChange={setCreating}>
        <DialogContent title="New form" description="Create the intake shell, then add fields and routing in the builder." footer={<Button variant="primary" size="sm" loading={create.isPending} disabled={!name.trim() || !slug.trim()} onClick={() => void create.mutate({ name: name.trim(), slug: slug.trim().toLowerCase(), purpose: "ticket", access: "public", enabled: true, fields: [] }).catch(() => {})}>Create form</Button>}>
          {Boolean(create.error) && <Callout tone="danger" className="mb-3">{create.error instanceof Error ? create.error.message : "Could not create this form."}</Callout>}
          <div className="flex flex-col gap-3 pb-4"><Field label="Name"><Input value={name} onChange={(event) => setName(event.target.value)} placeholder="Bug report" autoFocus /></Field><Field label="Slug" description="Lowercase letters, numbers, and hyphens."><Input value={slug} onChange={(event) => setSlug(event.target.value.replace(/[^a-zA-Z0-9-]/g, "-"))} placeholder="bug-report" /></Field></div>
        </DialogContent>
      </Dialog>
    </Page>
  );
}
