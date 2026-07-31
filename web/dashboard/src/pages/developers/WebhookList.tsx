import {
  ApiError,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  Checkbox,
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
  Switch,
  formatCompact,
  idempotencyKey,
  api,
  useMutation,
  useInfinite,
  type Paginated,
  type WebhookEndpoint,
} from "@hubchat/shared";
import { AlertTriangle, Plus, Webhook } from "lucide-react";
import { Link } from "react-router-dom";
import { useState } from "react";

const EVENTS = [
  "conversation.created", "conversation.assigned", "conversation.resolved", "message.created",
  "ticket.created", "ticket.updated", "ticket.sla_breached", "customer.created", "customer.updated",
  "feedback.created", "feedback.status_changed", "article.published", "changelog.published", "survey.response_created",
];

export default function WebhookList() {
  const query = useInfinite<WebhookEndpoint>(
    ["webhooks"],
    (cursor, signal) => {
      const params = new URLSearchParams({ limit: "50" });
      if (cursor) params.set("cursor", cursor);
      return api.get<Paginated<WebhookEndpoint>>(`/webhooks?${params.toString()}`, { signal });
    },
  );
  const update = useMutation<{ id: string; enabled: boolean }, WebhookEndpoint>(
    ({ id, enabled }) => { const endpoint = query.items.find((item) => item.id === id); return api.patch(`/webhooks/${id}`, { url: endpoint?.url, description: endpoint?.description ?? "", events: endpoint?.events ?? [], enabled }); },
    { invalidates: [["webhooks"]] },
  );
  const disabled = query.items.filter((endpoint) => endpoint.auto_disabled_at);

  return (
    <Page>
      <PageHeader title="Webhooks" description="Signed, timestamped HTTP callbacks with retry, replay, and delivery history." actions={<CreateWebhook />} />
      <PageBody>
        {query.isLoading ? <p className="text-sm text-fg-muted">Loading webhook endpoints…</p> : query.error ? <EmptyState icon={Webhook} title="Webhooks unavailable" description={query.error instanceof ApiError ? query.error.message : "Could not load webhooks."} action={<Button variant="secondary" onClick={query.refetch}>Try again</Button>} /> : (
            <>
              {disabled.length > 0 && <Callout tone="danger" className="mb-5" icon={<AlertTriangle />} title={`${disabled.length} endpoint disabled automatically`}>
                Hubchat paused delivery after six consecutive failures. Fix the receiving service, re-enable the endpoint, then replay the failed deliveries.
              </Callout>}
              <Section>
                {query.items.length === 0 ? <EmptyState icon={Webhook} title="No webhook endpoints" description="Webhooks tell your systems when something happens in Hubchat." /> : <div className="space-y-3">
                  {query.items.map((endpoint) => {
                    const total = endpoint.success_24h + endpoint.failure_24h;
                    const failureRate = total > 0 ? endpoint.failure_24h / total : 0;
                    return <Card key={endpoint.id}><CardBody><div className="flex flex-wrap items-start gap-4">
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2"><Link to={`/developers/webhooks/${endpoint.id}`} className="truncate font-mono text-sm text-fg hover:underline">{endpoint.url}</Link>
                          {endpoint.auto_disabled_at ? <Badge tone="danger">Auto-disabled</Badge> : !endpoint.enabled ? <Badge tone="neutral">Paused</Badge> : failureRate > 0.02 ? <Badge tone="warning">{Math.round(failureRate * 100)}% failing</Badge> : null}
                        </div>
                        {endpoint.description && <p className="mt-1 text-xs text-fg-muted">{endpoint.description}</p>}
                        <div className="mt-2 flex flex-wrap gap-1">{endpoint.events.length === 0 ? <Badge tone="neutral" variant="outline">All events</Badge> : endpoint.events.map((event) => <Badge key={event} tone="neutral" variant="outline">{event}</Badge>)}</div>
                        <p className="mt-2 text-2xs tabular text-fg-disabled">{formatCompact(endpoint.success_24h)} delivered · {endpoint.failure_24h} failed in 24h · secret {endpoint.secret_hint}</p>
                      </div>
                      <Switch checked={endpoint.enabled} onCheckedChange={(checked) => void update.mutate({ id: endpoint.id, enabled: checked }).catch(() => {})} aria-label={`Enable ${endpoint.url}`} />
                    </div></CardBody></Card>;
                  })}
                </div>}
              </Section>
              <Pagination hasPrevious={false} hasNext={query.hasMore} onPrevious={() => undefined} onNext={() => void query.fetchNext()} summary={`${query.items.length} endpoint${query.items.length === 1 ? "" : "s"} loaded`} />
            </>
        )}
      </PageBody>
    </Page>
  );
}

function CreateWebhook() {
  const [open, setOpen] = useState(false);
  const [url, setURL] = useState("");
  const [description, setDescription] = useState("");
  const [events, setEvents] = useState<string[]>(["message.created"]);
  const [secret, setSecret] = useState<string | null>(null);
  const create = useMutation<{ url: string; description: string; events: string[] }, { secret: string }>(
    (input) => api.post("/webhooks", input, { idempotencyKey: idempotencyKey() }),
    { invalidates: [["webhooks"]], onSuccess: (result) => setSecret(result.secret) },
  );
  return <Dialog open={open} onOpenChange={(value) => { setOpen(value); if (!value) { setSecret(null); create.reset(); } }}>
    <DialogTrigger asChild><Button variant="primary" size="sm" leading={<Plus />}>New endpoint</Button></DialogTrigger>
    <DialogContent title={secret ? "Webhook endpoint created" : "New webhook endpoint"} description={secret ? "Copy the signing secret now. It will not be shown again." : "Choose the events this endpoint should receive."} size="lg" footer={<><Button variant="ghost" size="sm" onClick={() => setOpen(false)}>{secret ? "Done" : "Cancel"}</Button>{!secret && <Button variant="primary" size="sm" loading={create.isPending} disabled={!url.trim()} onClick={() => void create.mutate({ url, description, events }).catch(() => {})}>Create endpoint</Button>}</>}>
      {secret ? <Callout tone="success"><code className="break-all font-mono text-xs">{secret}</code></Callout> : <div className="space-y-4"><Field label="Endpoint URL" description="HTTPS is recommended for production."><Input value={url} onChange={(event) => setURL(event.target.value)} placeholder="https://example.com/hubchat" autoFocus /></Field><Field label="Description"><Input value={description} onChange={(event) => setDescription(event.target.value)} placeholder="Production events" /></Field><Field label="Events"><div className="grid gap-2 sm:grid-cols-2">{EVENTS.map((event) => <Checkbox key={event} label={<span className="font-mono text-xs">{event}</span>} checked={events.includes(event)} onCheckedChange={(checked) => setEvents((current) => checked === true ? [...new Set([...current, event])] : current.filter((item) => item !== event))} />)}</div></Field>{Boolean(create.error) && <Callout tone="danger">{create.error instanceof Error ? create.error.message : "Could not create endpoint."}</Callout>}</div>}
    </DialogContent>
  </Dialog>;
}
