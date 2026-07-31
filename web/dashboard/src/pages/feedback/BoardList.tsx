import {
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
  api,
  idempotencyKey,
  useInfinite,
  useMutation,
  type FeedbackBoard,
  type Paginated,
} from "@hubchat/shared";
import { Eye, EyeOff, Lightbulb, Lock, Plus } from "lucide-react";
import { Link } from "react-router-dom";
import { useState } from "react";

const VISIBILITY = {
  public: { label: "Public", tone: "success" as const, icon: Eye },
  private: { label: "Private", tone: "neutral" as const, icon: Lock },
  invite_only: { label: "Invite only", tone: "warning" as const, icon: EyeOff },
};

export default function BoardList() {
  const query = useInfinite<FeedbackBoard>(
    ["feedback-boards"],
    (cursor, signal) => {
      const params = new URLSearchParams({ limit: "50" });
      if (cursor) params.set("cursor", cursor);
      return api.get<Paginated<FeedbackBoard>>(`/feedback/boards?${params.toString()}`, { signal });
    },
  );
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [description, setDescription] = useState("");
  const create = useMutation<{ name: string; slug: string; description: string }, FeedbackBoard>(
    (input) => api.post("/feedback/boards", input, { idempotencyKey: idempotencyKey() }),
    {
      invalidates: [["feedback-boards"]],
      onSuccess: () => {
        setOpen(false);
        setName("");
        setSlug("");
        setDescription("");
      },
    },
  );
  const boards = query.items;

  return (
    <Page>
      <PageHeader
        title="Feedback boards"
        description="Structured product feedback with voting, moderation, and status updates."
        actions={
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild>
              <Button variant="primary" size="sm" leading={<Plus />}>New board</Button>
            </DialogTrigger>
            <DialogContent
              title="Create feedback board"
              footer={
                <>
                  <Button variant="ghost" size="sm" onClick={() => setOpen(false)}>Cancel</Button>
                  <Button
                    variant="primary"
                    size="sm"
                    loading={create.isPending}
                    disabled={!name.trim() || !slug.trim()}
                    onClick={() => void create.mutate({ name: name.trim(), slug: slug.trim().toLowerCase(), description }).catch(() => {})}
                  >
                    Create board
                  </Button>
                </>
              }
            >
              <div className="space-y-4">
                <Field label="Name"><Input autoFocus value={name} onChange={(event) => { setName(event.target.value); if (!slug) setSlug(event.target.value.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")); }} /></Field>
                <Field label="Slug"><Input mono value={slug} onChange={(event) => setSlug(event.target.value)} /></Field>
                <Field label="Description"><Input value={description} onChange={(event) => setDescription(event.target.value)} /></Field>
                {Boolean(create.error) && <p className="text-sm text-danger">Could not create board.</p>}
              </div>
            </DialogContent>
          </Dialog>
        }
      />
      <PageBody>
        <Section>
          {query.isLoading ? <p className="text-sm text-fg-muted">Loading boards…</p> : query.error ? (
            <EmptyState icon={Lightbulb} title="Boards unavailable" description="Could not load feedback boards." action={<Button variant="secondary" onClick={query.refetch}>Try again</Button>} />
          ) : boards.length === 0 ? (
            <EmptyState icon={Lightbulb} title="No boards yet" description="Create a board to collect requests on one theme." />
          ) : (
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
              {boards.map((board) => {
                const visibility = VISIBILITY[board.visibility];
                const Icon = visibility.icon;
                return <Card key={board.id} interactive className="p-0"><Link to={`/feedback/boards/${board.id}`} className="block"><CardBody><div className="flex items-start justify-between gap-2"><h3 className="text-sm font-semibold text-fg">{board.name}</h3><Badge tone={visibility.tone} leading={<Icon />}>{visibility.label}</Badge></div><p className="mt-1 line-clamp-2 text-xs leading-normal text-fg-muted">{board.description || "No description"}</p><div className="mt-3 flex items-center gap-3 text-2xs tabular text-fg-muted"><span>{board.item_count} items</span>{board.allow_voting && <span>voting</span>}{board.allow_comments && <span>comments</span>}</div></CardBody></Link></Card>;
              })}
            </div>
          )}
        </Section>
      </PageBody>
      <Pagination hasPrevious={false} hasNext={query.hasMore} onPrevious={() => undefined} onNext={() => void query.fetchNext()} summary={`${boards.length} board${boards.length === 1 ? "" : "s"} loaded`} />
    </Page>
  );
}
