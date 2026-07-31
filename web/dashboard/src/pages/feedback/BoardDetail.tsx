import {
  ApiError,
  Badge,
  Button,
  Card,
  CardBody,
  EmptyState,
  Page,
  PageBody,
  PageHeader,
  Pagination,
  SearchInput,
  Select,
  Toolbar,
  api,
  useInfinite,
  useMutation,
  useQuery,
  type FeedbackBoard,
  type FeedbackItem,
  type Paginated,
} from "@hubchat/shared";
import { ChevronUp, Lightbulb, MessageSquare, MoreHorizontal } from "lucide-react";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";

export const STATUS_META: Record<string, { label: string; tone: "neutral" | "info" | "accent" | "warning" | "success" | "danger" }> = {
  open: { label: "Open", tone: "neutral" },
  reviewing: { label: "Reviewing", tone: "info" },
  planned: { label: "Planned", tone: "accent" },
  in_progress: { label: "In progress", tone: "warning" },
  completed: { label: "Completed", tone: "success" },
  declined: { label: "Declined", tone: "danger" },
  held: { label: "Held", tone: "warning" },
};

export default function BoardDetail() {
  const { boardId } = useParams();
  const [sort, setSort] = useState("votes");
  const [query, setQuery] = useState("");
  const [statusItem, setStatusItem] = useState<FeedbackItem | null>(null);
  const [status, setStatusValue] = useState("open");
  const board = useQuery<FeedbackBoard>(["feedback-board", boardId], (signal) => api.get(`/feedback/boards/${encodeURIComponent(boardId ?? "")}`, { signal }), { enabled: Boolean(boardId) });
  const items = useInfinite<FeedbackItem>(["feedback-items", boardId, sort, query], (cursor, signal) => {
    const params = new URLSearchParams({ sort: sort === "votes" ? "" : "recent", q: query, limit: "50" });
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<FeedbackItem>>(`/feedback/boards/${encodeURIComponent(boardId ?? "")}/items?${params.toString()}`, { signal });
  }, { enabled: Boolean(boardId) });
  const setStatus = useMutation<{ status: string; note: string }, FeedbackItem>(({ status: nextStatus, note }) => api.patch(`/feedback/items/${encodeURIComponent(statusItem?.id ?? "")}/status`, { status: nextStatus, note }), { invalidates: [["feedback-items", boardId, sort, query], ["feedback-items", boardId]] });

  if (board.isLoading) return <Page><PageBody><p className="text-sm text-fg-muted">Loading board…</p></PageBody></Page>;
  if (board.error || !board.data) return <Page><PageBody><EmptyState icon={Lightbulb} title="Board not found" description={board.error instanceof ApiError ? board.error.message : "This board is unavailable."} /></PageBody></Page>;
  const current = board.data;
  return <Page>
    <PageHeader breadcrumbs={[{ label: "Feedback", href: "/feedback" }, { label: current.name }]} title={current.name} description={current.description || undefined} meta={<Badge tone={current.visibility === "public" ? "success" : "neutral"}>{current.visibility}</Badge>} />
    <Toolbar leading={<div className="w-64"><SearchInput inputSize="sm" value={query} onChange={(event) => setQuery(event.target.value)} onClear={() => setQuery("")} placeholder="Search this board" /></div>} trailing={<Select size="sm" value={sort} onValueChange={setSort} options={[{ value: "votes", label: "Top voted" }, { value: "recent", label: "Newest" }]} />} />
    <PageBody>
      {items.isLoading ? <p className="text-sm text-fg-muted">Loading feedback…</p> : items.error ? <EmptyState icon={Lightbulb} title="Feedback unavailable" description={items.error instanceof ApiError ? items.error.message : "Could not load feedback items."} action={<Button variant="secondary" onClick={items.refetch}>Try again</Button>} /> : <div className="space-y-2">{items.items.map((item) => { const meta = STATUS_META[item.status] ?? STATUS_META.open!; return <Card key={item.id} className="p-0"><CardBody className="flex items-start gap-4"><div className="flex w-12 shrink-0 flex-col items-center gap-0.5 rounded-md border border-line px-2 py-1.5 text-fg-secondary"><ChevronUp className="size-4" /><span className="text-sm font-semibold tabular">{item.vote_count}</span></div><div className="min-w-0 flex-1"><div className="flex items-start justify-between gap-3"><Link to={`/feedback/items/${item.id}`} className="min-w-0 text-sm font-medium text-fg hover:underline">{item.title}</Link><Badge tone={meta.tone}>{meta.label}</Badge></div>{item.description && <p className="mt-1 line-clamp-2 text-xs text-fg-muted">{item.description}</p>}<div className="mt-2 flex items-center gap-3 text-2xs text-fg-muted"><span className="flex items-center gap-1"><MessageSquare className="size-3" />{item.comment_count}</span><span>{item.subscriber_count} following</span></div></div><Button variant="ghost" size="xs" iconOnly aria-label="Change status" leading={<MoreHorizontal />} onClick={() => { setStatusItem(item); setStatusValue(item.status); }} /></CardBody></Card>; })}{items.items.length === 0 && <EmptyState icon={Lightbulb} title="Nothing on this board yet" description="Items arrive from the portal, widget, API, or an agent." />}</div>}
    </PageBody>
    <Pagination hasPrevious={false} hasNext={items.hasMore} onPrevious={() => undefined} onNext={() => void items.fetchNext()} summary={`${items.items.length} item${items.items.length === 1 ? "" : "s"} loaded`} />
    {statusItem && <div className="fixed inset-0 z-50 grid place-items-center bg-black/30 p-4" role="dialog" aria-modal="true"><div className="w-full max-w-sm rounded-lg border border-line bg-surface p-5 shadow-xl"><h2 className="text-sm font-semibold text-fg">Change status</h2><div className="mt-4"><Select value={status} onValueChange={setStatusValue} options={Object.entries(STATUS_META).map(([value, meta]) => ({ value, label: meta.label }))} /></div><div className="mt-4 flex justify-end gap-2"><Button variant="ghost" size="sm" onClick={() => setStatusItem(null)}>Cancel</Button><Button variant="primary" size="sm" loading={setStatus.isPending} onClick={() => void setStatus.mutate({ status, note: "" }).then(() => setStatusItem(null)).catch(() => {})}>Save</Button></div>{Boolean(setStatus.error) && <p className="mt-2 text-xs text-danger">Could not update this item.</p>}</div></div>}
  </Page>;
}
