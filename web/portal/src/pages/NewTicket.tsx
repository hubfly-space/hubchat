import {
  ApiError,
  Breadcrumbs,
  Button,
  Callout,
  Card,
  CardBody,
  EmptyState,
  Field,
  Input,
  Select,
  Textarea,
  api,
  idempotencyKey,
  useMutation,
  useQuery,
  cn,
} from "@hubchat/shared";
import { CheckCircle2, Paperclip, TicketCheck, UploadCloud } from "lucide-react";
import { useState } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { usePortal } from "../portal-context";
import { portalText } from "../i18n";

type CreatedTicket = { ticket: { id: string; prefix: string; number: number; conversation_id?: string }; opening_message_id?: string };
type SearchResponse = { data: Array<{ article: { id: string; slug: string; title: string; excerpt: string } }> };

export default function NewTicket() {
  const navigate = useNavigate();
  const location = useLocation();
  const { data: portalData } = usePortal();
  const t = (key: string, fallback: string, values?: Record<string, string | number>) => portalText(portalData, key, fallback, values);
  const [summary, setSummary] = useState("");
  const [description, setDescription] = useState("");
  const [severity, setSeverity] = useState("major");
  const [created, setCreated] = useState<CreatedTicket | null>(null);
  const [files, setFiles] = useState<File[]>([]);
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState("");
  const suggestionsQuery = useQuery<SearchResponse>(
    ["portal-ticket-suggestions", portalData?.portal.workspace_id ?? "", summary.trim()],
    (signal) => api.get(`/public/knowledge-bases/${encodeURIComponent(portalData?.portal.workspace_id ?? "")}/search?q=${encodeURIComponent(summary.trim())}&surface=portal`, { signal }),
    { enabled: Boolean(portalData?.portal.workspace_id && summary.trim().length >= 4) },
  );
  const create = useMutation(({ title, body, key }: { title: string; body: string; key: string }) =>
    api.post<CreatedTicket>("/portal/tickets", { title, description: body, priority: severity }, { idempotencyKey: key }),
  );

  if (!portalData?.viewer) {
    return <EmptyState icon={TicketCheck} title={t("sign_in_before_request", "Sign in before sending a request")} description={t("session_request_history", "A portal session keeps your request history and replies connected to you.")} action={<Button variant="primary" size="sm" asChild><Link to={`/sign-in?portal=${encodeURIComponent(portalData?.portal.id ?? "")}&next=${encodeURIComponent(location.pathname + location.search)}`}>{t("sign_in", "Sign in")}</Link></Button>} />;
  }

  const suggestions = (suggestionsQuery.data?.data ?? []).map((item) => item.article).slice(0, 3);

  if (created) {
    return <div className="mx-auto max-w-lg py-10 text-center"><div className="mx-auto mb-4 grid size-12 place-items-center rounded-xl border border-success-border bg-success-subtle"><CheckCircle2 aria-hidden="true" className="size-6 text-success-text" /></div><h1 className="text-xl font-semibold tracking-tight text-fg">{t("thanks_on_it", "Thanks — we're on it")}</h1><p className="mt-2 text-sm leading-normal text-fg-muted">{t("request_created", "Your request is")} <span className="font-mono text-fg">{created.ticket.prefix}-{created.ticket.number}</span>. {t("follow_requests", "You can follow it in your requests.")}</p><div className="mt-6 flex justify-center gap-2"><Button variant="primary" size="sm" asChild><Link to={`/tickets/${created.ticket.id}`}>{t("view_request", "View request")}</Link></Button><Button variant="ghost" size="sm" onClick={() => navigate("/")}>{t("back_help_centre", "Back to help centre")}</Button></div></div>;
  }

  const submit = async () => {
    const title = summary.trim();
    const body = description.trim();
    if (!title || !body) return;
    try {
      const result = await create.mutate({ title, body, key: idempotencyKey() });
      if (files.length > 0 && result.opening_message_id) {
        setUploading(true);
        setUploadError("");
        try {
          for (const file of files) {
            const form = new FormData();
            form.append("file", file);
            form.append("message_id", result.opening_message_id);
            await api.post(`/portal/tickets/${encodeURIComponent(result.ticket.id)}/files`, form, { idempotencyKey: idempotencyKey() });
          }
        } catch {
          setUploadError(t("attachment_upload_warning", "The request was created, but one or more attachments could not be uploaded. You can add them from the request page."));
        } finally {
          setUploading(false);
        }
      }
      setCreated(result);
    } catch {
      // Keep the draft visible so the customer can retry without retyping.
    }
  };

  return <div><Breadcrumbs className="mb-4" items={[{ label: t("your_requests", "Your requests"), href: "/tickets" }, { label: t("new_request", "New request") }]} /><header className="mb-6"><h1 className="text-2xl font-semibold tracking-tighter text-fg">{t("send_request", "Send a request")}</h1><p className="mt-1.5 text-sm text-fg-muted">{t("specific_help", "The more specific you are, the faster we can help.")}</p></header><div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_280px]"><Card className="min-w-0"><CardBody className="space-y-5"><Field label={t("help_subject", "What do you need help with?")} required description={t("one_line", "One line. You can add detail below.")}><Input inputSize="lg" value={summary} onChange={(event) => setSummary(event.target.value)} placeholder="Webhooks stopped arriving after we upgraded" autoFocus /></Field><Field label={t("tell_more", "Tell us more")} required description={t("expected_happened", "What you expected, what happened instead, and roughly when.")}><Textarea rows={7} value={description} onChange={(event) => setDescription(event.target.value)} placeholder="Include steps, error messages, and timestamps if you have them." /></Field><div className="grid gap-4 sm:grid-cols-2"><Field label={t("urgency", "How urgent is this?")} required><Select value={severity} onValueChange={setSeverity} aria-label={t("severity", "Severity")} options={[{ value: "blocking", label: t("blocking", "Blocking"), description: t("cannot_work", "We cannot work at all.") }, { value: "major", label: t("major", "Major"), description: t("core_broken", "A core feature is broken.") }, { value: "minor", label: t("minor", "Minor"), description: t("annoying_continue", "Annoying, but we can continue.") }, { value: "question", label: t("question", "Just a question") }]} /></Field><Field label={t("your_email", "Your email")} description={t("reply_here", "We reply here.")}><Input value={portalData.viewer.email} readOnly /></Field></div><Field label={t("attachments", "Attachments")} description={t("attachment_description", "Screenshots, logs, or a screen recording. Up to 25 MB each.")}><label className={cn("flex cursor-pointer flex-col items-center gap-1.5 rounded-lg border border-dashed border-line px-4 py-7 text-center", "transition-colors hover:border-line-strong hover:bg-fill")}><UploadCloud aria-hidden="true" className="size-5 text-fg-muted" /><span className="text-xs text-fg-secondary">{files.length ? files.map((file) => file.name).join(", ") : t("choose_attachments", "Choose screenshots or logs")}</span><input type="file" multiple className="sr-only" onChange={(event) => setFiles(Array.from(event.target.files ?? []))} /></label></Field><div className="flex items-center justify-between gap-3 border-t border-line pt-4"><Button variant="ghost" size="sm" leading={<Paperclip />} asChild><Link to="/kb">{t("browse_guides_instead", "Browse guides instead")}</Link></Button><Button variant="primary" size="md" disabled={!summary.trim() || !description.trim() || create.isPending || uploading} onClick={() => void submit()}>{uploading ? t("uploading", "Uploading…") : create.isPending ? t("sending", "Sending…") : t("send_request", "Send request")}</Button></div>{Boolean(create.error) && <p className="text-sm text-danger">{create.error instanceof ApiError ? create.error.message : t("request_error", "Could not send your request. Your draft is still here.")}</p>}{uploadError && <p className="text-sm text-warning-text">{uploadError}</p>}</CardBody></Card><aside className="min-w-0"><div className="lg:sticky lg:top-20">{suggestions.length > 0 ? <div className="animate-fade-up rounded-lg border border-accent-border bg-accent-subtle p-4"><p className="text-xs font-medium text-accent-text">{t("suggested_guides", "These might answer it already")}</p><ul className="mt-2.5 space-y-2">{suggestions.map((article) => <li key={article.slug}><Link to={`/kb/article/${article.slug}`} className="block rounded-md bg-surface p-2.5 transition-colors hover:bg-surface-hover"><p className="text-xs font-medium leading-snug text-fg">{article.title}</p><p className="mt-1 line-clamp-2 text-2xs leading-normal text-fg-muted">{article.excerpt}</p></Link></li>)}</ul></div> : <Callout tone="info">{t("live_suggestions", "As you describe the problem, we will show any guides that look relevant here.")}</Callout>}<div className="mt-4 rounded-lg border border-line bg-surface p-4"><p className="text-xs font-medium text-fg">{t("what_next", "What happens next")}</p><ol className="mt-2 space-y-2 text-2xs leading-normal text-fg-muted"><li>1. {t("confirmation_email", "You get a confirmation email with a ticket number.")}</li><li>2. {t("business_hours", "A person reads it during business hours.")}</li><li>3. {t("replies_here", "Replies appear in your requests here.")}</li></ol></div></div></aside></div></div>;
}
