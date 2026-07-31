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
  Dialog,
  DialogContent,
  EmptyState,
  Field,
  Input,
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
  type FeedbackLink,
  type FeedbackItem,
  type Conversation,
  type Ticket,
} from "@hubchat/shared";
import { ChevronUp, Combine, Lightbulb, Link2, Send } from "lucide-react";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { STATUS_META } from "./BoardDetail";

export default function ItemDetail() {
  const { itemId } = useParams();
  const [status, setStatus] = useState<string | null>(null);
  const [update, setUpdate] = useState("");
  const [linkOpen, setLinkOpen] = useState(false);
  const [mergeOpen, setMergeOpen] = useState(false);
  const item = useQuery<FeedbackItem>(["feedback-item", itemId], (signal) => api.get(`/feedback/items/${encodeURIComponent(itemId ?? "")}`, { signal }), { enabled: Boolean(itemId) });
  const board = useQuery<FeedbackBoard>(["feedback-item-board", item.data?.board_id], (signal) => api.get(`/feedback/boards/${encodeURIComponent(item.data?.board_id ?? "")}`, { signal }), { enabled: Boolean(item.data?.board_id) });
  const comments = useQuery<{ data: FeedbackComment[] }>(["feedback-comments", itemId], (signal) => api.get(`/feedback/items/${encodeURIComponent(itemId ?? "")}/comments`, { signal }), { enabled: Boolean(itemId) });
  const links = useQuery<{ data: FeedbackLink[] }>(["feedback-links", itemId], (signal) => api.get(`/feedback/items/${encodeURIComponent(itemId ?? "")}/links`, { signal }), { enabled: Boolean(itemId) });
  const changeStatus = useMutation<{ status: string; note: string }, FeedbackItem>((input) => api.patch(`/feedback/items/${encodeURIComponent(itemId ?? "")}/status`, input), { invalidates: [["feedback-item", itemId], ["feedback-comments", itemId]] });
  const addComment = useMutation<{ body: string; is_official_update: boolean }, FeedbackComment>((input) => api.post(`/feedback/items/${encodeURIComponent(itemId ?? "")}/comments`, input, { idempotencyKey: idempotencyKey() }), { invalidates: [["feedback-item", itemId], ["feedback-comments", itemId]], onSuccess: () => setUpdate("") });

  if (item.isLoading) return <Page><PageBody><p className="text-sm text-fg-muted">Loading feedback item…</p></PageBody></Page>;
  if (item.error || !item.data) return <Page><PageBody><EmptyState icon={Lightbulb} size="lg" title="Feedback item not found" description={item.error instanceof ApiError ? item.error.message : "This item is unavailable."} /></PageBody></Page>;
  const current = item.data;
  const meta = STATUS_META[current.status] ?? STATUS_META.open!;
  const boardName = board.data?.name ?? "Feedback board";

  return <Page>
    <PageHeader breadcrumbs={[{ label: "Feedback", href: "/feedback" }, { label: boardName, href: `/feedback/boards/${current.board_id}` }, { label: current.title }]} title={current.title} meta={<Badge tone={meta.tone}>{meta.label}</Badge>} actions={<><Button variant="secondary" size="sm" leading={<Link2 />} onClick={() => setLinkOpen(true)}>Link support work</Button><Button variant="secondary" size="sm" leading={<Combine />} onClick={() => setMergeOpen(true)}>Merge duplicate</Button></>} />
    <PageBody width="full"><div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_300px]">
      <div className="min-w-0 space-y-5">
        <Card><CardBody className="flex gap-4"><div className="flex w-14 shrink-0 flex-col items-center gap-0.5 rounded-md border border-line px-2 py-2 text-fg-secondary"><ChevronUp className="size-4" /><span className="text-md font-semibold tabular">{current.vote_count}</span><span className="text-2xs text-fg-muted">votes</span></div><div className="min-w-0 flex-1"><p className="mb-2 text-xs text-fg-muted">Submitted {formatDate(current.created_at)}</p><p className="whitespace-pre-wrap text-sm leading-normal text-fg">{current.description || "No description was provided."}</p></div></CardBody></Card>
        <Section title="Post a status update"><Callout tone="info" className="mb-3">Status changes are recorded in the item history and can notify subscribers when delivery is enabled.</Callout><Card><CardBody><div className="mb-3 w-52"><Select size="sm" value={status ?? current.status} onValueChange={setStatus} aria-label="New status" options={Object.entries(STATUS_META).map(([value, valueMeta]) => ({ value, label: valueMeta.label }))} /></div><Textarea autoResize rows={3} value={update} onChange={(event) => setUpdate(event.target.value)} placeholder="Explain what changed and roughly when customers can expect it." aria-label="Status update message" /><div className="mt-2 flex justify-end"><Button variant="primary" size="sm" loading={changeStatus.isPending} disabled={!update.trim() && (status ?? current.status) === current.status} onClick={() => void changeStatus.mutate({ status: status ?? current.status, note: update }).catch(() => {})}>Post update</Button></div>{Boolean(changeStatus.error) && <p className="mt-2 text-xs text-danger">Could not update this item. Please try again.</p>}</CardBody></Card></Section>
        <Section title={`Comments (${current.comment_count})`}><Card><CardBody className="p-0">{comments.isLoading ? <p className="p-4 text-sm text-fg-muted">Loading comments…</p> : comments.error ? <p className="p-4 text-sm text-danger">Could not load comments.</p> : (comments.data?.data ?? []).length === 0 ? <p className="p-4 text-sm text-fg-muted">No comments yet.</p> : <ul className="divide-y divide-line-subtle">{(comments.data?.data ?? []).map((comment) => <li key={comment.id} className="flex gap-3 px-4 py-3"><Avatar name={comment.author_name} size="sm" /><div className="min-w-0 flex-1"><p className="flex items-center gap-2 text-xs"><span className="font-medium text-fg">{comment.author_name}</span>{comment.is_official_update && <Badge tone="accent">Team</Badge>}</p><p className="mt-1 whitespace-pre-wrap text-sm leading-normal text-fg-secondary">{comment.body}</p><p className="mt-1 text-2xs text-fg-muted">{formatDate(comment.created_at)}</p></div></li>)}</ul>}<div className="border-t border-line-subtle p-4"><Textarea rows={3} value={update} onChange={(event) => setUpdate(event.target.value)} placeholder="Add an internal or public update…" aria-label="Add a comment" /><div className="mt-2 flex justify-end"><Button variant="secondary" size="sm" loading={addComment.isPending} disabled={!update.trim()} leading={<Send />} onClick={() => void addComment.mutate({ body: update, is_official_update: true }).catch(() => {})}>Add comment</Button></div>{Boolean(addComment.error) && <p className="mt-2 text-xs text-danger">Could not add comment. Please try again.</p>}</div></CardBody></Card></Section>
      </div>
      <aside className="space-y-4"><Card><CardHeader title="Details" /><CardBody><dl><DetailRow label="Board"><Link className="text-accent-text hover:underline" to={`/feedback/boards/${current.board_id}`}>{boardName}</Link></DetailRow><DetailRow label="Type">{current.type.replace(/_/g, " ")}</DetailRow><DetailRow label="Visibility">{current.visibility}</DetailRow><DetailRow label="Votes">{current.vote_count}</DetailRow><DetailRow label="Subscribers">{current.subscriber_count}</DetailRow><DetailRow label="Created">{formatDate(current.created_at)}</DetailRow></dl></CardBody></Card><Card><CardHeader title="Linked support work" description="Connect the request to the conversation or ticket where it is being handled." /><CardBody>{(links.data?.data ?? []).length === 0 ? <p className="text-sm text-fg-muted">Nothing linked yet.</p> : <ul className="space-y-2">{(links.data?.data ?? []).map((link) => <li key={link.id} className="flex items-center justify-between gap-2 text-xs"><span className="truncate text-fg-secondary">{link.conversation_id ? `Conversation · ${link.conversation_id}` : `Ticket · ${link.ticket_id}`}</span><Button variant="ghost" size="xs" onClick={() => void api.delete(`/feedback/items/${current.id}/links/${link.id}`).then(() => links.refetch()).catch(() => {})}>Remove</Button></li>)}</ul>}</CardBody></Card><Card><CardHeader title="Subscribers" description="Status changes are delivered to subscribed customers through the configured email worker." /><CardBody><p className="text-sm text-fg-secondary">{current.subscriber_count === 0 ? "No customers are following this item yet." : `${current.subscriber_count} customer${current.subscriber_count === 1 ? " is" : "s are"} following status changes.`}</p></CardBody></Card></aside>
    </div></PageBody>
    {linkOpen && <FeedbackLinkDialog item={current} onClose={() => setLinkOpen(false)} onLinked={() => { setLinkOpen(false); void links.refetch(); }} />}
    {mergeOpen && <FeedbackMergeDialog item={current} onClose={() => setMergeOpen(false)} />}
  </Page>;
}

function FeedbackLinkDialog({ item, onClose, onLinked }: { item: FeedbackItem; onClose: () => void; onLinked: () => void }) {
  const [kind, setKind] = useState<"conversation" | "ticket">("conversation");
  const [query, setQuery] = useState("");
  const conversations = useQuery<{ data: Conversation[] }>(["feedback-link-conversations"], (signal) => api.get("/conversations?state=new,open,pending,waiting_for_customer,waiting_for_support&limit=100", { signal }), { enabled: kind === "conversation" });
  const tickets = useQuery<{ data: Ticket[] }>(["feedback-link-tickets"], (signal) => api.get("/tickets?status=new,open,pending,on_hold&limit=100", { signal }), { enabled: kind === "ticket" });
  const link = useMutation<{ conversation_id: string; ticket_id: string }, FeedbackLink>((input) => api.post(`/feedback/items/${item.id}/links`, input, { idempotencyKey: idempotencyKey() }), { onSuccess: onLinked });
  const normalized = query.trim().toLowerCase();
  const conversationCandidates = (conversations.data?.data ?? []).filter((candidate) => !normalized || `${candidate.subject ?? ""} ${candidate.id}`.toLowerCase().includes(normalized));
  const ticketCandidates = (tickets.data?.data ?? []).filter((candidate) => !normalized || `${candidate.title} ${candidate.prefix}-${candidate.number}`.toLowerCase().includes(normalized));

  return <Dialog open onOpenChange={(open) => !open && onClose()}><DialogContent title="Link support work" description="Connect this feedback request to an existing conversation or ticket."><div className="space-y-3"><Field label="Type"><Select size="sm" value={kind} onValueChange={(value) => { setKind(value as "conversation" | "ticket"); setQuery(""); }} options={[{ value: "conversation", label: "Conversation" }, { value: "ticket", label: "Ticket" }]} /></Field><Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={kind === "conversation" ? "Search subject or id…" : "Search title or ticket number…"} autoFocus /><ul className="flex max-h-64 flex-col gap-1 overflow-y-auto">{kind === "conversation" ? conversationCandidates.map((candidate) => <li key={candidate.id}><button type="button" className="w-full rounded-md px-2 py-2 text-left text-sm hover:bg-inset" onClick={() => void link.mutate({ conversation_id: candidate.id, ticket_id: "" }).catch(() => {})}><span className="block truncate text-fg">{candidate.subject ?? "Untitled conversation"}</span><span className="block truncate text-2xs text-fg-muted">{candidate.id}</span></button></li>) : ticketCandidates.map((candidate) => <li key={candidate.id}><button type="button" className="w-full rounded-md px-2 py-2 text-left text-sm hover:bg-inset" onClick={() => void link.mutate({ conversation_id: "", ticket_id: candidate.id }).catch(() => {})}><span className="block truncate text-fg">{candidate.prefix}-{candidate.number} · {candidate.title}</span></button></li>)}{((kind === "conversation" && conversationCandidates.length === 0) || (kind === "ticket" && ticketCandidates.length === 0)) && <li><EmptyState size="sm" title="No matching support work" /></li>}</ul>{Boolean(link.error) && <p className="text-sm text-danger">Could not create this link.</p>}</div></DialogContent></Dialog>;
}

function FeedbackMergeDialog({ item, onClose }: { item: FeedbackItem; onClose: () => void }) {
  const [query, setQuery] = useState("");
  const items = useQuery<{ data: FeedbackItem[] }>(["feedback-merge-candidates", item.board_id], (signal) => api.get(`/feedback/boards/${item.board_id}/items?sort=recent&limit=100`, { signal }));
  const merge = useMutation<{ target_id: string }, FeedbackItem>((input) => api.post(`/feedback/items/${item.id}/merge`, input, { idempotencyKey: idempotencyKey() }), { onSuccess: onClose });
  const candidates = (items.data?.data ?? []).filter((candidate) => candidate.id !== item.id && (!query.trim() || `${candidate.title} ${candidate.description}`.toLowerCase().includes(query.trim().toLowerCase())));
  return <Dialog open onOpenChange={(open) => !open && onClose()}><DialogContent title="Merge duplicate feedback" description="Votes, comments, subscribers, and support links move to the selected item. The current item remains as a redirect."><Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search feedback items…" autoFocus /><ul className="mt-3 flex max-h-64 flex-col gap-1 overflow-y-auto">{candidates.map((candidate) => <li key={candidate.id}><button type="button" className="w-full rounded-md px-2 py-2 text-left hover:bg-inset" onClick={() => void merge.mutate({ target_id: candidate.id }).catch(() => {})}><span className="block truncate text-sm text-fg">{candidate.title}</span><span className="block truncate text-2xs text-fg-muted">{candidate.description || "No description"}</span></button></li>)}{candidates.length === 0 && <EmptyState size="sm" title="No duplicate candidates" />}</ul>{Boolean(merge.error) && <p className="mt-3 text-sm text-danger">Could not merge this item.</p>}</DialogContent></Dialog>;
}
