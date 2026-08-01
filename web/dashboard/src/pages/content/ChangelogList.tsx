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
  Select,
  Textarea,
  api,
  formatDate,
  idempotencyKey,
  useInfinite,
  useMutation,
  type Paginated,
} from "@hubchat/shared";
import { Megaphone, Pencil, Plus, Send } from "lucide-react";
import { useState } from "react";

type Entry = {
  id: string;
  title: string;
  body: string;
  kind: string;
  published_at: string | null;
  created_at: string;
  updated_at: string;
};
type Draft = { id?: string; title: string; body: string; kind: string };
const EMPTY: Draft = { title: "", body: "", kind: "added" };
const KIND_LABEL: Record<string, string> = { added: "Added", improved: "Improved", fixed: "Fixed", removed: "Removed" };

export default function ChangelogList() {
  const query = useInfinite<Entry>(["changelog"], (cursor, signal) => {
    const params = new URLSearchParams({ limit: "50" });
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<Entry>>("/changelog?" + params.toString(), { signal });
  });
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<Draft>(EMPTY);
  const save = useMutation<Draft, Entry>(
    (input) => input.id
      ? api.patch("/changelog/" + encodeURIComponent(input.id), input)
      : api.post("/changelog", input, { idempotencyKey: idempotencyKey() }),
    { invalidates: [["changelog"]], onSuccess: () => { setOpen(false); setDraft(EMPTY); } },
  );
  const publish = useMutation<{ id: string }, Entry>(
    ({ id }) => api.post("/changelog/" + encodeURIComponent(id) + "/publish", {}, { idempotencyKey: idempotencyKey() }),
    { invalidates: [["changelog"]] },
  );
  const edit = (entry: Entry) => {
    setDraft({ id: entry.id, title: entry.title, body: entry.body, kind: entry.kind });
    setOpen(true);
  };

  return (
    <Page>
      <PageHeader
        title="Changelog"
        description="Publish product updates to the customer portal and opted-in subscribers."
        actions={
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild>
              <Button variant="primary" size="sm" leading={<Plus />} onClick={() => setDraft(EMPTY)}>
                New update
              </Button>
            </DialogTrigger>
            <DialogContent
              title={draft.id ? "Edit update" : "New update"}
              footer={
                <>
                  <Button variant="ghost" size="sm" onClick={() => setOpen(false)}>Cancel</Button>
                  <Button
                    variant="primary"
                    size="sm"
                    loading={save.isPending}
                    disabled={!draft.title.trim()}
                    onClick={() => void save.mutate({ ...draft, title: draft.title.trim(), body: draft.body.trim() }).catch(() => {})}
                  >
                    {draft.id ? "Save changes" : "Save draft"}
                  </Button>
                </>
              }
            >
              <div className="space-y-4">
                <Field label="Title" required>
                  <Input autoFocus value={draft.title} onChange={(event) => setDraft((current) => ({ ...current, title: event.target.value }))} placeholder="Faster ticket routing" />
                </Field>
                <Field label="Type">
                  <Select value={draft.kind} onValueChange={(value) => setDraft((current) => ({ ...current, kind: value }))} options={Object.entries(KIND_LABEL).map(([value, label]) => ({ value, label }))} />
                </Field>
                <Field label="What changed">
                  <Textarea rows={6} value={draft.body} onChange={(event) => setDraft((current) => ({ ...current, body: event.target.value }))} placeholder="Explain the customer-visible change in plain language." />
                </Field>
                {Boolean(save.error) && <p className="text-sm text-danger">{save.error instanceof ApiError ? save.error.message : "Could not save this update."}</p>}
              </div>
            </DialogContent>
          </Dialog>
        }
      />
      <PageBody>
        {query.isLoading ? (
          <p className="text-sm text-fg-muted">Loading changelog…</p>
        ) : query.error ? (
          <EmptyState
            icon={Megaphone}
            title="Changelog unavailable"
            description={query.error instanceof ApiError ? query.error.message : "Could not load changelog."}
            action={<Button variant="secondary" onClick={query.refetch}>Try again</Button>}
          />
        ) : query.items.length === 0 ? (
          <EmptyState
            icon={Megaphone}
            title="No updates yet"
            description="Publish the first customer-facing product update."
            action={<Button variant="primary" size="sm" leading={<Plus />} onClick={() => { setDraft(EMPTY); setOpen(true); }}>Write an update</Button>}
          />
        ) : (
          <>
            <div className="space-y-3">
              {query.items.map((entry) => (
                <Card key={entry.id}>
                  <CardBody className="flex flex-wrap items-start gap-4">
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <h2 className="text-sm font-semibold text-fg">{entry.title}</h2>
                        <Badge tone={entry.published_at ? "success" : "neutral"}>{entry.published_at ? "Published" : "Draft"}</Badge>
                        <Badge tone="neutral">{KIND_LABEL[entry.kind] ?? entry.kind}</Badge>
                      </div>
                      <p className="mt-1 whitespace-pre-wrap text-sm leading-relaxed text-fg-secondary">{entry.body || "No description yet."}</p>
                      <p className="mt-2 text-xs text-fg-muted">{entry.published_at ? "Published " + formatDate(entry.published_at) : "Updated " + formatDate(entry.updated_at)}</p>
                    </div>
                    <div className="flex shrink-0 gap-2">
                      <Button variant="ghost" size="sm" leading={<Pencil />} onClick={() => edit(entry)}>Edit</Button>
                      {!entry.published_at && <Button variant="primary" size="sm" leading={<Send />} loading={publish.isPending} onClick={() => void publish.mutate({ id: entry.id }).catch(() => {})}>Publish</Button>}
                    </div>
                  </CardBody>
                </Card>
              ))}
            </div>
            {query.hasMore && (
              <div className="flex justify-center pt-4">
                <Button variant="secondary" size="sm" loading={query.isFetching} onClick={() => void query.fetchNext()}>
                  Load older updates
                </Button>
              </div>
            )}
          </>
        )}
      </PageBody>
    </Page>
  );
}
