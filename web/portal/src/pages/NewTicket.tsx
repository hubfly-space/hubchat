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
  cn,
} from "@hubchat/shared";
import { CheckCircle2, Paperclip, TicketCheck, UploadCloud } from "lucide-react";
import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { articles } from "../data";
import { usePortal } from "../portal-context";

type CreatedTicket = { ticket: { id: string; prefix: string; number: number } };

export default function NewTicket() {
  const navigate = useNavigate();
  const { data: portalData } = usePortal();
  const [summary, setSummary] = useState("");
  const [description, setDescription] = useState("");
  const [severity, setSeverity] = useState("major");
  const [created, setCreated] = useState<CreatedTicket | null>(null);
  const create = useMutation(({ title, body, key }: { title: string; body: string; key: string }) =>
    api.post<CreatedTicket>("/portal/tickets", { title, description: body, priority: severity }, { idempotencyKey: key }),
  );

  if (!portalData?.viewer) {
    return <EmptyState icon={TicketCheck} title="Sign in before sending a request" description="A portal session keeps your request history and replies connected to you." action={<Button variant="primary" size="sm" asChild><Link to="/sign-in">Sign in</Link></Button>} />;
  }

  const suggestions = summary.trim().length >= 4 ? articles.filter((article) => {
    const words = summary.toLowerCase().split(/\s+/).filter((word) => word.length > 3);
    const haystack = `${article.title} ${article.excerpt}`.toLowerCase();
    return words.some((word) => haystack.includes(word));
  }).slice(0, 3) : [];

  if (created) {
    return <div className="mx-auto max-w-lg py-10 text-center"><div className="mx-auto mb-4 grid size-12 place-items-center rounded-xl border border-success-border bg-success-subtle"><CheckCircle2 aria-hidden="true" className="size-6 text-success-text" /></div><h1 className="text-xl font-semibold tracking-tight text-fg">Thanks — we're on it</h1><p className="mt-2 text-sm leading-normal text-fg-muted">Your request is <span className="font-mono text-fg">{created.ticket.prefix}-{created.ticket.number}</span>. You can follow it in your requests.</p><div className="mt-6 flex justify-center gap-2"><Button variant="primary" size="sm" asChild><Link to={`/tickets/${created.ticket.id}`}>View request</Link></Button><Button variant="ghost" size="sm" onClick={() => navigate("/")}>Back to help centre</Button></div></div>;
  }

  const submit = async () => {
    const title = summary.trim();
    const body = description.trim();
    if (!title || !body) return;
    try {
      const result = await create.mutate({ title, body, key: idempotencyKey() });
      setCreated(result);
    } catch {
      // Keep the draft visible so the customer can retry without retyping.
    }
  };

  return <div><Breadcrumbs className="mb-4" items={[{ label: "Your requests", href: "/tickets" }, { label: "New request" }]} /><header className="mb-6"><h1 className="text-2xl font-semibold tracking-tighter text-fg">Send a request</h1><p className="mt-1.5 text-sm text-fg-muted">The more specific you are, the faster we can help.</p></header><div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_280px]"><Card className="min-w-0"><CardBody className="space-y-5"><Field label="What do you need help with?" required description="One line. You can add detail below."><Input inputSize="lg" value={summary} onChange={(event) => setSummary(event.target.value)} placeholder="Webhooks stopped arriving after we upgraded" autoFocus /></Field><Field label="Tell us more" required description="What you expected, what happened instead, and roughly when."><Textarea rows={7} value={description} onChange={(event) => setDescription(event.target.value)} placeholder="Include steps, error messages, and timestamps if you have them." /></Field><div className="grid gap-4 sm:grid-cols-2"><Field label="How urgent is this?" required><Select value={severity} onValueChange={setSeverity} aria-label="Severity" options={[{ value: "blocking", label: "Blocking", description: "We cannot work at all." }, { value: "major", label: "Major", description: "A core feature is broken." }, { value: "minor", label: "Minor", description: "Annoying, but we can continue." }, { value: "question", label: "Just a question" }]} /></Field><Field label="Your email" description="We reply here."><Input value={portalData.viewer.email} readOnly /></Field></div><Field label="Attachments" description="Screenshots, logs, or a screen recording. Up to 25 MB each."><label className={cn("flex cursor-pointer flex-col items-center gap-1.5 rounded-lg border border-dashed border-line px-4 py-7 text-center", "transition-colors hover:border-line-strong hover:bg-fill")}><UploadCloud aria-hidden="true" className="size-5 text-fg-muted" /><span className="text-xs text-fg-secondary">Attachments will be enabled after the portal upload flow is published.</span><input type="file" multiple className="sr-only" disabled /></label></Field><div className="flex items-center justify-between gap-3 border-t border-line pt-4"><Button variant="ghost" size="sm" leading={<Paperclip />} asChild><Link to="/kb">Browse guides instead</Link></Button><Button variant="primary" size="md" disabled={!summary.trim() || !description.trim() || create.isPending} onClick={() => void submit()}>{create.isPending ? "Sending…" : "Send request"}</Button></div>{Boolean(create.error) && <p className="text-sm text-danger">{create.error instanceof ApiError ? create.error.message : "Could not send your request. Your draft is still here."}</p>}</CardBody></Card><aside className="min-w-0"><div className="lg:sticky lg:top-20">{suggestions.length > 0 ? <div className="animate-fade-up rounded-lg border border-accent-border bg-accent-subtle p-4"><p className="text-xs font-medium text-accent-text">These might answer it already</p><ul className="mt-2.5 space-y-2">{suggestions.map((article) => <li key={article.slug}><Link to={`/kb/article/${article.slug}`} className="block rounded-md bg-surface p-2.5 transition-colors hover:bg-surface-hover"><p className="text-xs font-medium leading-snug text-fg">{article.title}</p><p className="mt-1 line-clamp-2 text-2xs leading-normal text-fg-muted">{article.excerpt}</p></Link></li>)}</ul></div> : <Callout tone="info">As you describe the problem, we will show any guides that look relevant here.</Callout>}<div className="mt-4 rounded-lg border border-line bg-surface p-4"><p className="text-xs font-medium text-fg">What happens next</p><ol className="mt-2 space-y-2 text-2xs leading-normal text-fg-muted"><li>1. You get a confirmation email with a ticket number.</li><li>2. A person reads it during business hours.</li><li>3. Replies appear in your requests here.</li></ol></div></div></aside></div></div>;
}
