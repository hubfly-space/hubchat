import {
  ApiError,
  Avatar,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  DetailRow,
  EmptyState,
  Page,
  PageBody,
  PageHeader,
  Section,
  Select,
  Textarea,
  api,
  formatDate,
  idempotencyKey,
  useMutation,
  useQuery,
  type FeedbackBoard,
  type FeedbackComment,
  type FeedbackItem,
} from "@hubchat/shared";
import { Bell, ChevronUp, Combine, Lightbulb, Link2, Send } from "lucide-react";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { STATUS_META } from "./BoardDetail";

export default function ItemDetail() {
  const { itemId } = useParams();
  const [status, setStatus] = useState<string | null>(null);
  const [update, setUpdate] = useState("");
  const item = useQuery<FeedbackItem>(["feedback-item", itemId], (signal) => api.get(`/feedback/items/${encodeURIComponent(itemId ?? "")}`, { signal }), { enabled: Boolean(itemId) });
  const board = useQuery<FeedbackBoard>(["feedback-item-board", item.data?.board_id], (signal) => api.get(`/feedback/boards/${encodeURIComponent(item.data?.board_id ?? "")}`, { signal }), { enabled: Boolean(item.data?.board_id) });
  const comments = useQuery<{ data: FeedbackComment[] }>(["feedback-comments", itemId], (signal) => api.get(`/feedback/items/${encodeURIComponent(itemId ?? "")}/comments`, { signal }), { enabled: Boolean(itemId) });
  const changeStatus = useMutation<{ status: string; note: string }, FeedbackItem>((input) => api.patch(`/feedback/items/${encodeURIComponent(itemId ?? "")}/status`, input), { invalidates: [["feedback-item", itemId], ["feedback-comments", itemId]] });
  const addComment = useMutation<{ body: string; is_official_update: boolean }, FeedbackComment>((input) => api.post(`/feedback/items/${encodeURIComponent(itemId ?? "")}/comments`, input, { idempotencyKey: idempotencyKey() }), { invalidates: [["feedback-item", itemId], ["feedback-comments", itemId]], onSuccess: () => setUpdate("") });

  if (item.isLoading) return <Page><PageBody><p className="text-sm text-fg-muted">Loading feedback item…</p></PageBody></Page>;
  if (item.error || !item.data) return <Page><PageBody><EmptyState icon={Lightbulb} size="lg" title="Feedback item not found" description={item.error instanceof ApiError ? item.error.message : "This item is unavailable."} /></PageBody></Page>;
  const current = item.data;
  const meta = STATUS_META[current.status] ?? STATUS_META.open!;
  const boardName = board.data?.name ?? "Feedback board";

  return <Page>
    <PageHeader breadcrumbs={[{ label: "Feedback", href: "/feedback" }, { label: boardName, href: `/feedback/boards/${current.board_id}` }, { label: current.title }]} title={current.title} meta={<Badge tone={meta.tone}>{meta.label}</Badge>} actions={<><Button variant="secondary" size="sm" leading={<Link2 />}>Link a conversation</Button><Button variant="secondary" size="sm" leading={<Combine />}>Merge</Button></>} />
    <PageBody width="full"><div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_300px]">
      <div className="min-w-0 space-y-5">
        <Card><CardBody className="flex gap-4"><div className="flex w-14 shrink-0 flex-col items-center gap-0.5 rounded-md border border-line px-2 py-2 text-fg-secondary"><ChevronUp className="size-4" /><span className="text-md font-semibold tabular">{current.vote_count}</span><span className="text-2xs text-fg-muted">votes</span></div><div className="min-w-0 flex-1"><p className="mb-2 text-xs text-fg-muted">Submitted {formatDate(current.created_at)}</p><p className="whitespace-pre-wrap text-sm leading-normal text-fg">{current.description || "No description was provided."}</p></div></CardBody></Card>
        <Section title="Post a status update"><Callout tone="info" className="mb-3">Status changes are recorded in the item history and can notify subscribers when delivery is enabled.</Callout><Card><CardBody><div className="mb-3 w-52"><Select size="sm" value={status ?? current.status} onValueChange={setStatus} aria-label="New status" options={Object.entries(STATUS_META).map(([value, valueMeta]) => ({ value, label: valueMeta.label }))} /></div><Textarea autoResize rows={3} value={update} onChange={(event) => setUpdate(event.target.value)} placeholder="Explain what changed and roughly when customers can expect it." aria-label="Status update message" /><div className="mt-2 flex justify-end"><Button variant="primary" size="sm" loading={changeStatus.isPending} disabled={!update.trim() && (status ?? current.status) === current.status} onClick={() => void changeStatus.mutate({ status: status ?? current.status, note: update }).catch(() => {})}>Post update</Button></div>{Boolean(changeStatus.error) && <p className="mt-2 text-xs text-danger">Could not update this item. Please try again.</p>}</CardBody></Card></Section>
        <Section title={`Comments (${current.comment_count})`}><Card><CardBody className="p-0">{comments.isLoading ? <p className="p-4 text-sm text-fg-muted">Loading comments…</p> : comments.error ? <p className="p-4 text-sm text-danger">Could not load comments.</p> : (comments.data?.data ?? []).length === 0 ? <p className="p-4 text-sm text-fg-muted">No comments yet.</p> : <ul className="divide-y divide-line-subtle">{(comments.data?.data ?? []).map((comment) => <li key={comment.id} className="flex gap-3 px-4 py-3"><Avatar name={comment.author_name} size="sm" /><div className="min-w-0 flex-1"><p className="flex items-center gap-2 text-xs"><span className="font-medium text-fg">{comment.author_name}</span>{comment.is_official_update && <Badge tone="accent">Team</Badge>}</p><p className="mt-1 whitespace-pre-wrap text-sm leading-normal text-fg-secondary">{comment.body}</p><p className="mt-1 text-2xs text-fg-muted">{formatDate(comment.created_at)}</p></div></li>)}</ul>}<div className="border-t border-line-subtle p-4"><Textarea rows={3} value={update} onChange={(event) => setUpdate(event.target.value)} placeholder="Add an internal or public update…" aria-label="Add a comment" /><div className="mt-2 flex justify-end"><Button variant="secondary" size="sm" loading={addComment.isPending} disabled={!update.trim()} leading={<Send />} onClick={() => void addComment.mutate({ body: update, is_official_update: true }).catch(() => {})}>Add comment</Button></div>{Boolean(addComment.error) && <p className="mt-2 text-xs text-danger">Could not add comment. Please try again.</p>}</div></CardBody></Card></Section>
      </div>
      <aside className="space-y-4"><Card><CardHeader title="Details" /><CardBody><dl><DetailRow label="Board"><Link className="text-accent-text hover:underline" to={`/feedback/boards/${current.board_id}`}>{boardName}</Link></DetailRow><DetailRow label="Type">{current.type.replace(/_/g, " ")}</DetailRow><DetailRow label="Visibility">{current.visibility}</DetailRow><DetailRow label="Votes">{current.vote_count}</DetailRow><DetailRow label="Subscribers">{current.subscriber_count}</DetailRow><DetailRow label="Created">{formatDate(current.created_at)}</DetailRow></dl></CardBody></Card><Card><CardHeader title="Subscribers" description="Notified on every status change when notifications are configured." /><CardBody><Button variant="secondary" size="sm" fullWidth leading={<Bell />}>Notify {current.subscriber_count} subscribers</Button></CardBody></Card></aside>
    </div></PageBody>
  </Page>;
}
