import {
  Avatar,
  Badge,
  Breadcrumbs,
  Button,
  Callout,
  Card,
  CardBody,
  EmptyState,
  Textarea,
  api,
  idempotencyKey,
  useMutation,
  useQuery,
  cn,
  formatDateTime,
  useToast,
  type BadgeTone,
} from "@hubchat/shared";
import { Paperclip, TicketCheck } from "lucide-react";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { portalErrorMessage, usePortal } from "../portal-context";

const STATUS: Record<string, { label: string; tone: BadgeTone }> = {
  new: { label: "New", tone: "accent" },
  open: { label: "Open", tone: "accent" },
  pending: { label: "Pending", tone: "neutral" },
  on_hold: { label: "On hold", tone: "neutral" },
  resolved: { label: "Resolved", tone: "success" },
  closed: { label: "Closed", tone: "neutral" },
};

type Ticket = {
  id: string;
  number: number;
  prefix: string;
  title: string;
  description: string;
  status: string;
  created_at: string;
  updated_at: string;
};

type Message = {
  id: string;
  author_type: "customer" | "agent";
  author_id: string | null;
  author_name: string;
  body: string;
  created_at: string;
};

type Detail = { ticket: Ticket; messages: Message[] };

export default function TicketDetail() {
  const { number: id } = useParams();
  const toast = useToast();
  const { data: portalData } = usePortal();
  const [reply, setReply] = useState("");
  const query = useQuery<Detail>(
    portalData?.viewer && id ? ["portal", "ticket", id] : null,
    (signal) => api.get(`/portal/tickets/${encodeURIComponent(id!)}`, { signal }),
  );
  const sendReply = useMutation(({ body, key }: { body: string; key: string }) =>
    api.post(`/portal/tickets/${encodeURIComponent(id!)}/replies`, { body, client_id: key }, { idempotencyKey: key }),
  );

  if (!portalData?.viewer) {
    return <EmptyState icon={TicketCheck} title="Sign in to view this request" description="This request is available only to its customer." action={<Button variant="primary" size="sm" asChild><Link to="/sign-in">Sign in</Link></Button>} />;
  }
  if (query.isLoading) return <div className="py-12 text-center text-sm text-fg-muted">Loading request…</div>;
  if (query.isError || !query.data) {
    return <div className="py-12 text-center text-sm text-danger">{portalErrorMessage(query.error)}<div><Button className="mt-4" variant="secondary" size="sm" asChild><Link to="/tickets">Back to requests</Link></Button></div></div>;
  }

  const { ticket, messages } = query.data;
  const viewer = portalData.viewer;
  const status = STATUS[ticket.status] ?? { label: "Open", tone: "accent" as BadgeTone };
  const submitReply = async () => {
    const body = reply.trim();
    if (!body) return;
    const key = idempotencyKey();
    try {
      await sendReply.mutate({ body, key });
      setReply("");
      query.refetch();
      toast.success({ title: "Reply sent", description: "The team has been notified." });
    } catch {
      // useMutation exposes the error state; the composer remains intact for retry.
    }
  };

  return (
    <div className="mx-auto max-w-3xl">
      <Breadcrumbs className="mb-4" items={[{ label: "Your requests", href: "/tickets" }, { label: `${ticket.prefix}-${ticket.number}` }]} />
      <header className="mb-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <h1 className="text-2xl font-semibold tracking-tighter text-fg">{ticket.title}</h1>
          <Badge tone={status.tone}>{status.label}</Badge>
        </div>
        <p className="mt-2 font-mono text-xs text-fg-muted">{ticket.prefix}-{ticket.number} · opened {formatDateTime(ticket.created_at)}</p>
      </header>

      {ticket.status === "resolved" && <Callout tone="success" className="mb-5">This request was resolved. Replying below reopens it — no need to start a new one.</Callout>}

      <ol className="space-y-4">
        {messages.map((message) => {
          const mine = message.author_type === "customer";
          return <li key={message.id} className={cn("flex gap-3", mine && "flex-row-reverse")}>
            <Avatar name={message.author_name} seed={mine ? viewer.email : message.author_name} size="sm" className="mt-0.5 shrink-0" />
            <div className={cn("min-w-0 max-w-[85%]", mine && "text-right")}>
              <p className="mb-1 text-2xs text-fg-muted"><span className="text-fg-secondary">{mine ? "You" : message.author_name}</span>{" · "}{formatDateTime(message.created_at)}</p>
              <div className={cn("rounded-lg border px-3.5 py-2.5 text-left", mine ? "border-accent-border bg-accent-subtle" : "border-line bg-surface")}>
                <p className="whitespace-pre-wrap text-sm leading-normal text-fg">{message.body}</p>
              </div>
            </div>
          </li>;
        })}
      </ol>

      <Card className="mt-8"><CardBody>
        <Textarea autoResize rows={4} value={reply} onChange={(event) => setReply(event.target.value)} placeholder={ticket.status === "resolved" ? "Reply to reopen this request…" : "Add to this request…"} aria-label="Your reply" />
        <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
          <Button variant="ghost" size="sm" leading={<Paperclip />} disabled>Attach a file</Button>
          <Button variant="primary" size="sm" disabled={!reply.trim() || sendReply.isPending} onClick={() => void submitReply()}>{sendReply.isPending ? "Sending…" : ticket.status === "resolved" ? "Reply and reopen" : "Send reply"}</Button>
        </div>
        {Boolean(sendReply.error) && <p className="mt-2 text-xs text-danger">Could not send the reply. Try again; your text is still here.</p>}
      </CardBody></Card>
      <p className="mt-4 text-center text-2xs text-fg-muted">You are signed in as {viewer.email} · <Link to="/account" className="text-accent-text hover:underline">Manage notifications</Link></p>
    </div>
  );
}
