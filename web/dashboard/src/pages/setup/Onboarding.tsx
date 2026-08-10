import {
  api,
  ApiError,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CodeBlock,
  Field,
  Input,
  QueryError,
  RadioGroup,
  idempotencyKey,
  useAllPages,
  useMutation,
  useQuery,
  cn,
  formatDateTime,
  type Paginated,
} from "@hubchat/shared";
import { ArrowRight, Check, Copy, Mail, MessageSquare, Radio, Ticket } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { Wordmark } from "../../components/Wordmark";
import { useWorkspace, workspaceFormatOptions } from "../../app/workspace-context";

type Step = "usecase" | "inbox" | "surface" | "install" | "invite";

type Bootstrap = {
  workspace: { id: string; name: string; slug: string };
  inboxes: { id: string; name: string; slug: string; is_default: boolean }[];
};

type LiveWidget = {
  id: string;
  workspace_id: string;
  name: string;
  public_key: string;
  inbox_id: string | null;
  domains: string[];
  last_seen_at: string | null;
};

type InviteResult = { id: string; email: string; role: string };

/**
 * Workspace onboarding (§7.2) — distinct from installation.
 *
 * The flow is intentionally short, but every milestone is backed by the
 * server: the inbox comes from bootstrap, the widget is created through the
 * widget API, installation is detected from its real last-seen timestamp, and
 * invitations are sent through the normal member lifecycle.
 */
export default function Onboarding() {
  const navigate = useNavigate();
  const [step, setStep] = useState<Step>("usecase");
  const [useCase, setUseCase] = useState("support");

  const bootstrap = useQuery<Bootstrap>(
    ["onboarding-bootstrap"],
    (signal) => api.get<Bootstrap>("/bootstrap", { signal, fresh: true }),
    { staleTime: 0 },
  );
  const workspaceID = bootstrap.data?.workspace.id;
  const widgets = useAllPages<LiveWidget>(
    workspaceID ? ["onboarding-widgets", workspaceID, "lookup"] : null,
    (cursor, signal) => api.get<Paginated<LiveWidget>>(`/widgets?limit=200${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal, workspaceId: workspaceID }),
    { enabled: Boolean(workspaceID), staleTime: 0 },
  );

  const order: Step[] = ["usecase", "inbox", "surface", "install", "invite"];
  const index = order.indexOf(step);
  const next = () => {
    const following = order[index + 1];
    if (following) setStep(following);
    else navigate("/overview");
  };

  if (bootstrap.isLoading) {
    return <CenteredMessage message="Loading your workspace…" />;
  }
  if (bootstrap.error || !bootstrap.data || !workspaceID) {
    return (
      <div className="mx-auto max-w-md p-8">
        <QueryError error={bootstrap.error} retry={bootstrap.refetch} />
      </div>
    );
  }

  const defaultInbox = bootstrap.data.inboxes.find((inbox) => inbox.is_default) ?? bootstrap.data.inboxes[0];
  const widget = widgets.items[0] ?? null;

  return (
    <div className="min-h-dvh bg-canvas">
      <header className="border-b border-line bg-surface">
        <div className="mx-auto flex max-w-3xl items-center justify-between px-6 py-3">
          <Wordmark size="sm" />
          <div className="flex items-center gap-3">
            <span className="text-xs tabular text-fg-muted">Step {index + 1} of {order.length}</span>
            <Button variant="ghost" size="sm" onClick={() => navigate("/overview")}>Skip setup</Button>
          </div>
        </div>
        <div className="h-0.5 bg-chart-track">
          <div className="h-full bg-accent transition-[width] duration-slow ease-out" style={{ width: `${((index + 1) / order.length) * 100}%` }} />
        </div>
      </header>

      <main className="mx-auto max-w-3xl px-6 py-12">
        <ol aria-label="Onboarding progress" className="mb-8 grid grid-cols-5 gap-1.5">
          {order.map((item, itemIndex) => {
            const complete = itemIndex < index;
            const current = itemIndex === index;
            return (
              <li key={item} className="min-w-0">
                <div className={cn("h-1 rounded-full", complete ? "bg-success" : current ? "bg-accent" : "bg-chart-track")} />
                <p className={cn("mt-1 truncate text-2xs", current ? "font-medium text-fg" : "text-fg-muted")}>
                  {item === "usecase" ? "Goal" : item === "surface" ? "Surface" : item === "install" ? "Install" : item === "invite" ? "Team" : "Inbox"}
                </p>
              </li>
            );
          })}
        </ol>
        {step === "usecase" && (
          <Shell title="What will you use Hubchat for first?" description="This helps you choose a sensible starting surface. Nothing here is permanent." onNext={next}>
            <RadioGroup
              variant="cards"
              value={useCase}
              onValueChange={setUseCase}
              aria-label="Primary use case"
              options={[
                { value: "support", label: "Live chat and support", description: "A widget on your product, a shared inbox behind it." },
                { value: "tickets", label: "Structured ticketing", description: "Forms, custom fields, SLAs, and a customer portal." },
                { value: "feedback", label: "Product feedback and roadmap", description: "Public boards, voting, and a changelog." },
                { value: "selfserve", label: "Self-service help centre", description: "A knowledge base with contact as the fallback." },
              ]}
            />
          </Shell>
        )}

        {step === "inbox" && (
          <Shell title="Your first inbox is ready" description="Every workspace starts with a real default inbox. You can add channels and rename it later in Settings." onNext={next}>
            {defaultInbox ? (
              <Card variant="sunken"><CardBody className="flex items-center gap-3"><span className="grid size-9 place-items-center rounded-full bg-accent-subtle text-accent-text"><MessageSquare className="size-4" /></span><div><p className="text-sm font-medium text-fg">{defaultInbox.name}</p><p className="text-xs text-fg-muted">{defaultInbox.slug} · default destination for new conversations</p></div><Badge className="ml-auto" tone="success">Live</Badge></CardBody></Card>
            ) : (
              <Callout tone="danger">No inbox was provisioned for this workspace. Open Channels → Inboxes after setup to repair it.</Callout>
            )}
          </Shell>
        )}

        {step === "surface" && (
          <Shell title="Choose how customers reach you" description="You can add every other channel later — this only guides the first install." onNext={next}>
            <div className="grid gap-3 sm:grid-cols-2">
              <SurfaceCard icon={<MessageSquare />} title="Website widget" detail="Recommended first step: live chat, help articles, and ticket forms in one surface." selected />
              <SurfaceCard icon={<Radio />} title="Customer portal" detail="Available after setup for hosted ticket and knowledge-base access." />
              <SurfaceCard icon={<Mail />} title="Email" detail="Available after setup once outbound and inbound email are configured." />
              <SurfaceCard icon={<Ticket />} title="Embedded form" detail="Available after setup for focused support requests." />
            </div>
            <Callout tone="info" className="mt-4">The first-run path creates a widget because it is the fastest way to prove the full loop: customer message → inbox → agent reply.</Callout>
          </Shell>
        )}

        {step === "install" && (
          <InstallStep
            workspaceID={workspaceID}
            inboxID={defaultInbox?.id ?? null}
            widget={widget}
            refreshWidgets={widgets.refetch}
            onNext={next}
          />
        )}

        {step === "invite" && <InviteStep workspaceID={workspaceID} onDone={() => navigate("/overview")} />}
      </main>
    </div>
  );
}

function InstallStep({
  workspaceID,
  inboxID,
  widget,
  refreshWidgets,
  onNext,
}: {
  workspaceID: string;
  inboxID: string | null;
  widget: LiveWidget | null;
  refreshWidgets: () => void;
  onNext: () => void;
}) {
  const { workspace } = useWorkspace();
  const dateFormat = workspaceFormatOptions(workspace);
  const [createdWidget, setCreatedWidget] = useState<LiveWidget | null>(null);
  const [name, setName] = useState("Website support");
  const [domain, setDomain] = useState("");
  const [copied, setCopied] = useState(false);
  const active = createdWidget ?? widget;
  const activeID = active?.id;
  const firstDomain = active?.domains[0];

  useEffect(() => {
    if (firstDomain && !domain) setDomain(firstDomain);
  }, [firstDomain, domain]);

  useEffect(() => {
    if (!activeID) return;
    const refresh = () => refreshWidgets();
    refresh();
    const timer = window.setInterval(refresh, 5_000);
    return () => window.clearInterval(timer);
  }, [activeID, refreshWidgets]);

  const create = useMutation<{ name: string; inbox_id: string }, LiveWidget>(
    (input) => api.post<LiveWidget>("/widgets", input, { workspaceId: workspaceID, idempotencyKey: idempotencyKey() }),
    {
      invalidates: [["onboarding-widgets", workspaceID]],
      onSuccess: (created) => setCreatedWidget(created),
    },
  );
  const addDomain = useMutation<{ domain: string }, { id: string; domain: string }>(
    (input) => api.post<{ id: string; domain: string }>(`/widgets/${encodeURIComponent(active?.id ?? "")}/domains`, input, { workspaceId: workspaceID, idempotencyKey: idempotencyKey() }),
    {
      invalidates: [["onboarding-widgets", workspaceID]],
      onSuccess: (result) => setCreatedWidget((current) => current ? { ...current, domains: [...new Set([...current.domains, result.domain])] } : current),
    },
  );

  const normalizedDomain = domain.trim().toLowerCase().replace(/^https?:\/\//, "").split("/")[0] ?? "";
  const snippet = active ? `<script>
  (function(h,u,b){h.Hubchat=h.Hubchat||function(){(h.Hubchat.q=h.Hubchat.q||[]).push(arguments)};
  var s=u.createElement('script');s.async=1;s.src=b;u.head.appendChild(s)}
  (window,document,'${window.location.origin}/widget/v1.js');

  Hubchat('boot', { key: '${active.public_key}' });
</script>` : "";
  const copySnippet = async () => {
    if (!snippet || !navigator.clipboard) return;
    await navigator.clipboard.writeText(snippet);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1800);
  };

  return (
    <Shell
      title={active ? "Install the live widget" : "Create your first widget"}
      description={active ? "The snippet below uses the real public key for this workspace. Add the hostname where it will run so origin protection can verify it." : "Create the widget that will receive your first customer conversation. Its public key and inbox destination will come from the server."}
      onNext={active ? onNext : () => { if (inboxID) void create.mutate({ name: name.trim(), inbox_id: inboxID }).catch(() => {}); }}
      nextLabel={active ? "Continue" : "Create widget"}
      nextDisabled={!active && (!name.trim() || !inboxID)}
      nextLoading={create.isPending}
    >
      {!active && (
        <div className="space-y-4">
          <Field label="Widget name" htmlFor="onboarding-widget-name"><Input id="onboarding-widget-name" inputSize="lg" value={name} onChange={(event) => setName(event.target.value)} placeholder="Website support" autoFocus /></Field>
          {!inboxID && <Callout tone="danger">The workspace has no inbox to receive widget conversations.</Callout>}
          {Boolean(create.error) && <Callout tone="danger">{create.error instanceof ApiError ? create.error.message : "Could not create the widget."}</Callout>}
        </div>
      )}

      {active && (
        <>
          <Card variant="sunken"><CardBody className="space-y-4">
            <div className="flex items-center gap-3"><span className={cn("grid size-8 shrink-0 place-items-center rounded-full", active.last_seen_at ? "bg-success-subtle text-success-text" : "bg-fill text-fg-muted")}>{active.last_seen_at ? <Check className="size-4" strokeWidth={3} /> : <span className="size-2 animate-pulse rounded-full bg-current" />}</span><div className="min-w-0"><p className="text-sm font-medium text-fg">{active.last_seen_at ? "Widget detected" : "Waiting for the first widget load"}</p><p className="text-xs text-fg-muted">{active.last_seen_at ? `Last seen ${formatDateTime(active.last_seen_at, dateFormat)}` : "Install the snippet on an allowlisted hostname, then this check will update automatically."}</p></div><Badge className="ml-auto" tone={active.last_seen_at ? "success" : "warning"}>{active.last_seen_at ? "Connected" : "Not detected"}</Badge></div>
            <div className="flex flex-wrap items-end gap-2"><Field className="min-w-[16rem] flex-1" label="Allowed hostname" htmlFor="onboarding-widget-domain" description="Use a hostname, for example app.example.com."><Input id="onboarding-widget-domain" value={domain} onChange={(event) => setDomain(event.target.value)} placeholder="app.example.com" /></Field><Button variant="secondary" size="md" disabled={!normalizedDomain || addDomain.isPending || active.domains.includes(normalizedDomain)} loading={addDomain.isPending} onClick={() => void addDomain.mutate({ domain: normalizedDomain }).catch(() => {})}>{active.domains.includes(normalizedDomain) ? "Allowlisted" : "Allow hostname"}</Button></div>
            {Boolean(addDomain.error) && <p className="text-sm text-danger">{addDomain.error instanceof ApiError ? addDomain.error.message : "Could not allowlist that hostname."}</p>}
            {!active.domains.length && <p className="text-xs text-warning-text">Add the hostname before testing the widget. Requests from other origins will be rejected.</p>}
          </CardBody></Card>

          <div className="mt-5"><CodeBlock filename="index.html" code={snippet} /></div>
          <div className="mt-3 flex flex-wrap justify-end gap-2">
            <Button variant="secondary" size="sm" disabled={!active.id} onClick={() => window.location.assign(`/app/channels/widgets/${active.id}`)}>Open widget preview</Button>
            <Button variant="ghost" size="sm" leading={<Copy />} onClick={() => void copySnippet()}>{copied ? "Copied" : "Copy install snippet"}</Button>
          </div>
        </>
      )}
    </Shell>
  );
}

function InviteStep({ workspaceID, onDone }: { workspaceID: string; onDone: () => void }) {
  const [emails, setEmails] = useState([""]);
  const [error, setError] = useState<string | null>(null);
  const invite = useMutation<{ email: string; role: string }, InviteResult>(
    (input) => api.post<InviteResult>("/invites", input, { workspaceId: workspaceID, idempotencyKey: idempotencyKey() }),
  );
  const send = async () => {
    const pending = emails.map((email) => email.trim().toLowerCase()).filter(Boolean);
    if (pending.length === 0) {
      onDone();
      return;
    }
    setError(null);
    try {
      for (const email of pending) await invite.mutate({ email, role: "agent" });
      onDone();
    } catch (caught) {
      setError(caught instanceof ApiError ? caught.message : "Could not send all invitations. Check the entries and try again.");
    }
  };

  return (
    <Shell title="Invite your team" description="Add the people who will answer conversations. Invitations use the workspace's normal member and email workflow." onNext={() => void send()} nextLabel={emails.some((email) => email.trim()) ? "Send invites and finish" : "Finish"} nextLoading={invite.isPending}>
      <div className="flex flex-col gap-2">
        {emails.map((email, index) => <Input key={index} inputSize="lg" type="email" value={email} onChange={(event) => setEmails((current) => current.map((item, itemIndex) => itemIndex === index ? event.target.value : item))} placeholder="teammate@example.com" aria-label={`Invite email ${index + 1}`} />)}
      </div>
      <Button variant="ghost" size="sm" className="mt-2" onClick={() => setEmails((current) => [...current, ""])}>Add another</Button>
      {error && <Callout tone="danger" className="mt-4">{error}</Callout>}
    </Shell>
  );
}

function Shell({
  title,
  description,
  children,
  onNext,
  nextLabel = "Continue",
  nextDisabled = false,
  nextLoading = false,
}: {
  title: string;
  description: string;
  children: ReactNode;
  onNext: () => void;
  nextLabel?: string;
  nextDisabled?: boolean;
  nextLoading?: boolean;
}) {
  return <div className="animate-fade-up"><h1 className="text-2xl font-semibold tracking-tighter text-fg">{title}</h1><p className="mt-2 max-w-measure text-sm leading-normal text-fg-muted">{description}</p><div className="mt-8">{children}</div><div className="mt-10 flex justify-end"><Button variant="primary" size="lg" disabled={nextDisabled} loading={nextLoading} onClick={onNext} trailing={<ArrowRight />}>{nextLabel}</Button></div></div>;
}

function SurfaceCard({ icon, title, detail, selected }: { icon: ReactNode; title: string; detail: string; selected?: boolean }) {
  return <Card interactive className={cn("p-4", selected && "border-accent-border bg-accent-subtle")}><div className="flex items-start justify-between gap-2"><span className="text-fg-muted [&_svg]:size-4">{icon}</span>{selected && <Badge tone="accent">Selected</Badge>}</div><p className="mt-2.5 text-sm font-medium text-fg">{title}</p><p className="mt-1 text-xs leading-normal text-fg-muted">{detail}</p></Card>;
}

function CenteredMessage({ message }: { message: string }) {
  return <div className="grid min-h-dvh place-items-center bg-canvas"><p className="text-sm text-fg-muted">{message}</p></div>;
}
