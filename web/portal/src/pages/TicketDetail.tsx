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
import { useEffect, useState } from "react";
import { Link, useLocation, useParams } from "react-router-dom";
import { portalErrorMessage, usePortal } from "../portal-context";
import { portalText } from "../i18n";

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
  attachments?: { id: string; name: string; url: string }[];
};

type Detail = { ticket: Ticket; messages: Message[]; has_more: boolean; next_cursor: string | null };

export default function TicketDetail() {
  const { number: id } = useParams();
  const location = useLocation();
  const toast = useToast();
  const { data: portalData } = usePortal();
  const t = (key: string, fallback: string, values?: Record<string, string | number>) => portalText(portalData, key, fallback, values);
  const [reply, setReply] = useState("");
  const [attachments, setAttachments] = useState<{ id: string; name: string; url: string }[]>([]);
  const [uploading, setUploading] = useState(false);
  const [olderMessages, setOlderMessages] = useState<Message[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const query = useQuery<Detail>(
    portalData?.viewer && id ? ["portal", "ticket", id] : null,
    (signal) => api.get(`/portal/tickets/${encodeURIComponent(id!)}`, { signal }),
  );
  const sendReply = useMutation(({ body, key, fileIDs }: { body: string; key: string; fileIDs: string[] }) =>
    api.post(`/portal/tickets/${encodeURIComponent(id!)}/replies`, { body, client_id: key, file_ids: fileIDs }, { idempotencyKey: key }),
  );

  useEffect(() => {
    if (!query.data) return;
    setOlderMessages([]);
    setNextCursor(query.data.next_cursor ?? null);
  }, [query.data]);

  if (!portalData?.viewer) {
    return <EmptyState icon={TicketCheck} title={t("sign_in_view_request", "Sign in to view this request")} description={t("request_customer_only", "This request is available only to its customer.")} action={<Button variant="primary" size="sm" asChild><Link to={`/sign-in?portal=${encodeURIComponent(portalData?.portal.id ?? "")}&next=${encodeURIComponent(location.pathname + location.search)}`}>{t("sign_in", "Sign in")}</Link></Button>} />;
  }
  if (query.isLoading) return <div className="py-12 text-center text-sm text-fg-muted">{t("loading_request", "Loading request…")}</div>;
  if (query.isError || !query.data) {
    return <div className="py-12 text-center text-sm text-danger">{portalErrorMessage(query.error)}<div><Button className="mt-4" variant="secondary" size="sm" asChild><Link to="/tickets">{t("back_to_requests", "Back to requests")}</Link></Button></div></div>;
  }

  const { ticket, messages } = query.data;
  const viewer = portalData.viewer;
  const status = STATUS[ticket.status] ?? { label: "Open", tone: "accent" as BadgeTone };
  const visibleMessages = [...olderMessages, ...messages];
  const loadOlder = async () => {
    if (!nextCursor || loadingOlder) return;
    setLoadingOlder(true);
    try {
      const page = await api.get<Detail>(`/portal/tickets/${encodeURIComponent(id!)}?limit=100&cursor=${encodeURIComponent(nextCursor)}`);
      setOlderMessages((current) => [...page.messages, ...current]);
      setNextCursor(page.next_cursor ?? null);
    } catch (error) {
      toast.error({ title: t("older_messages_error", "Could not load older messages"), description: portalErrorMessage(error) });
    } finally {
      setLoadingOlder(false);
    }
  };
  const submitReply = async () => {
    const body = reply.trim();
    if (!body) return;
    const key = idempotencyKey();
    try {
      await sendReply.mutate({ body, key, fileIDs: attachments.map((attachment) => attachment.id) });
      setReply("");
      setAttachments([]);
      query.refetch();
      toast.success({ title: t("reply_sent", "Reply sent"), description: t("team_notified", "The team has been notified.") });
    } catch {
      // useMutation exposes the error state; the composer remains intact for retry.
    }
  };

  const chooseFiles = async (files: FileList | null) => {
    if (!files || files.length === 0) return;
    setUploading(true);
    try {
      for (const selected of Array.from(files)) {
        const body = new FormData();
        body.append("file", selected);
        const uploaded = await api.post<{ id: string; name: string; url: string }>(`/portal/tickets/${encodeURIComponent(id!)}/files`, body, { idempotencyKey: idempotencyKey() });
        setAttachments((current) => [...current, uploaded]);
      }
    } catch {
      toast.error({ title: t("attach_error", "Could not attach file"), description: t("upload_again", "Try the upload again.") });
    } finally {
      setUploading(false);
    }
  };

  return (
    <div className="mx-auto max-w-3xl">
      <Breadcrumbs className="mb-4" items={[{ label: t("your_requests", "Your requests"), href: "/tickets" }, { label: `${ticket.prefix}-${ticket.number}` }]} />
      <header className="mb-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <h1 className="text-2xl font-semibold tracking-tighter text-fg">{ticket.title}</h1>
          <Badge tone={status.tone}>{status.label}</Badge>
        </div>
        <p className="mt-2 font-mono text-xs text-fg-muted">{ticket.prefix}-{ticket.number} · opened {formatDateTime(ticket.created_at)}</p>
      </header>

      {ticket.status === "resolved" && <Callout tone="success" className="mb-5">{t("resolved_reopen", "This request was resolved. Replying below reopens it — no need to start a new one.")}</Callout>}

      {nextCursor && <div className="mb-4 flex justify-center"><Button variant="secondary" size="sm" loading={loadingOlder} onClick={() => void loadOlder()}>{t("load_older", "Load older messages")}</Button></div>}

      <ol className="space-y-4">
        {visibleMessages.map((message) => {
          const mine = message.author_type === "customer";
          return <li key={message.id} className={cn("flex gap-3", mine && "flex-row-reverse")}>
            <Avatar name={message.author_name} seed={mine ? viewer.email : message.author_name} size="sm" className="mt-0.5 shrink-0" />
            <div className={cn("min-w-0 max-w-[85%]", mine && "text-right")}>
              <p className="mb-1 text-2xs text-fg-muted"><span className="text-fg-secondary">{mine ? t("you", "You") : message.author_name}</span>{" · "}{formatDateTime(message.created_at)}</p>
              <div className={cn("rounded-lg border px-3.5 py-2.5 text-left", mine ? "border-accent-border bg-accent-subtle" : "border-line bg-surface")}>
                <p className="whitespace-pre-wrap text-sm leading-normal text-fg">{message.body}</p>
                {message.attachments && message.attachments.length > 0 && <div className="mt-2 flex flex-wrap gap-1.5">{message.attachments.map((attachment) => <a key={attachment.id} href={attachment.url} className="inline-flex max-w-full items-center gap-1 rounded-md border border-line px-2 py-1 text-2xs text-accent-text hover:underline"><Paperclip aria-hidden="true" className="size-3" /><span className="max-w-48 truncate">{attachment.name}</span></a>)}</div>}
              </div>
            </div>
          </li>;
        })}
      </ol>

      <Card className="mt-8"><CardBody>
        <Textarea autoResize rows={4} value={reply} onChange={(event) => setReply(event.target.value)} placeholder={ticket.status === "resolved" ? t("reply_reopen", "Reply to reopen this request…") : t("add_request", "Add to this request…")} aria-label={t("your_reply", "Your reply")} />
        {attachments.length > 0 && <div className="mt-3 flex flex-wrap gap-1.5">{attachments.map((attachment) => <span key={attachment.id} className="inline-flex max-w-full items-center gap-1 rounded-md bg-fill px-2 py-1 text-2xs text-fg-secondary"><Paperclip aria-hidden="true" className="size-3" /><span className="max-w-48 truncate">{attachment.name}</span><button type="button" aria-label={`Remove ${attachment.name}`} onClick={() => setAttachments((current) => current.filter((item) => item.id !== attachment.id))}>×</button></span>)}</div>}
        <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
          <><Button variant="ghost" size="sm" leading={<Paperclip />} loading={uploading} onClick={() => document.getElementById("portal-file-input")?.click()}>{t("attach_file", "Attach a file")}</Button><input id="portal-file-input" type="file" multiple className="sr-only" onChange={(event) => void chooseFiles(event.target.files)} /></>
          <Button variant="primary" size="sm" disabled={!reply.trim() || sendReply.isPending} onClick={() => void submitReply()}>{sendReply.isPending ? t("sending", "Sending…") : ticket.status === "resolved" ? t("reply_reopen_button", "Reply and reopen") : t("send_reply", "Send reply")}</Button>
        </div>
        {Boolean(sendReply.error) && <p className="mt-2 text-xs text-danger">{t("reply_error", "Could not send the reply. Try again; your text is still here.")}</p>}
      </CardBody></Card>
      <p className="mt-4 text-center text-2xs text-fg-muted">{t("signed_in_as", "You are signed in as")} {viewer.email} · <Link to="/account" className="text-accent-text hover:underline">{t("manage_notifications", "Manage notifications")}</Link></p>
    </div>
  );
}
