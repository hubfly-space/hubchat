import {
  api,
  ApiError,
  Avatar,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  CodeBlock,
  ConfirmDialog,
  ConversationStateBadge,
  DetailRow,
  Dialog,
  DialogContent,
  EmptyState,
  Input,
  Menu,
  MenuContent,
  MenuItem,
  MenuSeparator,
  MenuTrigger,
  Page,
  PageBody,
  PageHeader,
  QueryBoundary,
  Section,
  TagChip,
  Tabs,
  TabsContent,
  TabsList,
  TicketStatusBadge,
  Tooltip,
  useInfinite,
  useMutation,
  useQuery,
  formatDateTime,
  formatRelativeShort,
  type AttributeDefinition,
  type Company,
  type Conversation,
  type Customer,
  type Ticket,
  type Paginated,
} from "@hubchat/shared";
import {
  Combine,
  Download,
  EyeOff,
  MoreHorizontal,
  ShieldAlert,
  Trash2,
  UserRound,
} from "lucide-react";
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";

type CustomerEvent = {
  id: string;
  type: string;
  source: string;
  payload: Record<string, unknown>;
  occurred_at: string;
};

type ContactSession = {
  id: string;
  started_at: string;
  last_seen_at: string;
  ended_at: string | null;
  browser: string | null;
  os: string | null;
  device: string;
  current_url: string | null;
};

/** Customer profile (§6.9) — the full record behind the inbox context panel. */
export default function CustomerDetail() {
  const { customerId } = useParams();

  const customerQuery = useQuery<Customer>(
    customerId ? ["customer", customerId] : null,
    (signal) => api.get(`/customers/${customerId}`, { signal }),
  );

  if (customerQuery.error instanceof ApiError && customerQuery.error.status === 404) {
    return (
      <Page>
        <EmptyState icon={UserRound} size="lg" title="Customer not found" />
      </Page>
    );
  }

  return <QueryBoundary query={customerQuery}>{(customer) => <CustomerDetailBody customer={customer} />}</QueryBoundary>;
}

function CustomerDetailBody({ customer }: { customer: Customer }) {
  const navigate = useNavigate();
  const { tagById, can } = useWorkspace();
  const [tab, setTab] = useState("activity");
  const [merging, setMerging] = useState(false);
  const [blocking, setBlocking] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const company = useQuery<Company>(
    customer.company_ids[0] ? ["company", customer.company_ids[0]] : null,
    (signal) => api.get(`/companies/${customer.company_ids[0]}`, { signal }),
  );

  const conversationsQuery = useInfinite<Conversation>(
    ["conversations", "by-customer", customer.id],
    (cursor, signal) => {
      const params = new URLSearchParams({ customer_id: customer.id, state: "new,open,pending,waiting_for_customer,waiting_for_support,snoozed,resolved,closed,spam", limit: "50" });
      if (cursor) params.set("cursor", cursor);
      return api.get<Paginated<Conversation>>(`/conversations?${params.toString()}`, { signal });
    },
  );
  const ticketsQuery = useInfinite<Ticket>(
    ["tickets", "by-customer", customer.id],
    (cursor, signal) => {
      const params = new URLSearchParams({ customer_id: customer.id, status: "new,open,pending,on_hold,resolved,closed", limit: "50" });
      if (cursor) params.set("cursor", cursor);
      return api.get<Paginated<Ticket>>(`/tickets?${params.toString()}`, { signal });
    },
  );
  const timelineQuery = useInfinite<CustomerEvent>(
    ["customer-timeline", customer.id],
    (cursor, signal) => {
      const params = new URLSearchParams({ limit: "50" });
      if (cursor) params.set("cursor", cursor);
      return api.get<Paginated<CustomerEvent>>(`/customers/${customer.id}/timeline?${params.toString()}`, { signal });
    },
  );
  const sessionsQuery = useInfinite<ContactSession>(
    ["customer-sessions", customer.id],
    (cursor, signal) => {
      const params = new URLSearchParams({ limit: "50" });
      if (cursor) params.set("cursor", cursor);
      return api.get<Paginated<ContactSession>>(`/customers/${customer.id}/sessions?${params.toString()}`, { signal });
    },
  );

  const theirConversations = conversationsQuery.items;
  const theirTickets = ticketsQuery.items;
  const events = timelineQuery.items;
  const sessions = sessionsQuery.items;

  const deleteCustomer = useMutation<void, unknown>(
    () => api.delete(`/customers/${customer.id}`),
    { invalidates: [["customers"]], onSuccess: () => navigate("/customers") },
  );
  const blockCustomer = useMutation<void, unknown>(
    () => api.post("/blocked-contacts", { kind: "customer", value: customer.id }),
    { onSuccess: () => setBlocking(false) },
  );

  return (
    <Page>
      <PageHeader
        breadcrumbs={[{ label: "Customers", href: "/customers" }, { label: customer.name ?? "Anonymous" }]}
        title={customer.name ?? "Anonymous visitor"}
        description={customer.email ?? undefined}
        meta={
          customer.verification === "verified" ? (
            <Badge tone="success">Verified</Badge>
          ) : (
            <Badge tone="warning">{customer.verification}</Badge>
          )
        }
        actions={
          <Menu>
            <MenuTrigger asChild>
              <Button variant="ghost" size="sm" iconOnly aria-label="More" leading={<MoreHorizontal />} />
            </MenuTrigger>
            <MenuContent align="end" className="w-64">
              {can("customer.merge") && (
                <MenuItem
                  icon={<Combine />}
                  description="Preview before applying — merges are audited and reversible for 30 days."
                  onSelect={() => setMerging(true)}
                >
                  Merge with another customer…
                </MenuItem>
              )}
              {can("customer.read_sensitive") && (
                <MenuItem
                  icon={<Download />}
                  onSelect={() => window.open(`/api/v1/customers/${customer.id}/export`, "_blank")}
                >
                  Export this customer's data
                </MenuItem>
              )}
              <MenuSeparator />
              <MenuItem icon={<ShieldAlert />} onSelect={() => setBlocking(true)}>
                Block from contacting
              </MenuItem>
              {can("customer.read_sensitive") && (
                <MenuItem icon={<Trash2 />} destructive onSelect={() => setDeleting(true)}>
                  Delete and anonymise
                </MenuItem>
              )}
            </MenuContent>
          </Menu>
        }
        tabs={
          <Tabs value={tab} onValueChange={setTab}>
            <TabsList
              items={[
                { value: "activity", label: "Activity", count: events.length },
                { value: "conversations", label: "Conversations", count: theirConversations.length },
                { value: "tickets", label: "Tickets", count: theirTickets.length },
                { value: "attributes", label: "Attributes" },
                { value: "sessions", label: "Sessions" },
              ]}
            />
          </Tabs>
        }
      />

      <PageBody width="full">
        <div className="grid gap-5 xl:grid-cols-[300px_minmax(0,1fr)]">
          {/* Identity ---------------------------------------------------- */}
          <aside className="space-y-4">
            <Card>
              <CardBody className="flex flex-col items-center text-center">
                <Avatar name={customer.name} seed={customer.id} size="xl" />
                <p className="mt-2.5 text-md font-semibold text-fg">{customer.name ?? "Anonymous visitor"}</p>
                <p className="text-xs text-fg-muted">{customer.email}</p>

                {customer.tag_ids.length > 0 && (
                  <div className="mt-3 flex flex-wrap justify-center gap-1">
                    {customer.tag_ids.map((tagId) => {
                      const tag = tagById(tagId);
                      return tag ? <TagChip key={tagId} label={tag.name} color={tag.color} /> : null;
                    })}
                  </div>
                )}
              </CardBody>
            </Card>

            <Card>
              <CardHeader title="Identity" />
              <CardBody>
                <dl>
                  <DetailRow label="External ID">
                    <span className="font-mono">{customer.external_id ?? "—"}</span>
                  </DetailRow>
                  <DetailRow label="Phone">{customer.phone ?? "—"}</DetailRow>
                  <DetailRow label="Language">{customer.language ?? "—"}</DetailRow>
                  <DetailRow label="Timezone">{customer.timezone ?? "—"}</DetailRow>
                  <DetailRow label="First seen">
                    <Tooltip content={formatDateTime(customer.first_seen_at)}>
                      <span>{formatRelativeShort(customer.first_seen_at, new Date())} ago</span>
                    </Tooltip>
                  </DetailRow>
                  <DetailRow label="Last contacted">
                    {customer.last_contacted_at ? `${formatRelativeShort(customer.last_contacted_at, new Date())} ago` : "never"}
                  </DetailRow>
                </dl>
              </CardBody>
            </Card>

            {company.data && (
              <Card>
                <CardHeader title="Company" />
                <CardBody>
                  <Link
                    to={`/companies/${company.data.id}`}
                    className="-m-1.5 flex items-center gap-2.5 rounded-md p-1.5 transition-colors hover:bg-fill"
                  >
                    <Avatar name={company.data.name} seed={company.data.id} shape="square" size="md" kind="company" />
                    <span className="min-w-0">
                      <span className="block truncate text-sm text-fg">{company.data.name}</span>
                      <span className="block truncate text-xs text-fg-muted">{company.data.domain}</span>
                    </span>
                  </Link>
                </CardBody>
              </Card>
            )}
          </aside>

          {/* Tab panels --------------------------------------------------- */}
          <div className="min-w-0">
            <Tabs value={tab} onValueChange={setTab}>
              <TabsContent value="activity">
                <Section title="Event timeline" description="Structured events sent by your application (§6.10).">
                  <Card>
                    <CardBody className="p-0">
                      {timelineQuery.isLoading ? (
                        <p className="p-5 text-sm text-fg-muted">Loading customer events…</p>
                      ) : timelineQuery.error ? (
                        <div className="flex items-center justify-between gap-3 p-5">
                          <p className="text-sm text-danger">Could not load customer events.</p>
                          <Button variant="ghost" size="xs" onClick={timelineQuery.refetch}>Retry</Button>
                        </div>
                      ) : events.length === 0 ? (
                        <EmptyState size="sm" title="No events yet" />
                      ) : (
                        <>
                          <ol className="divide-y divide-line-subtle">
                            {events.map((event) => (
                              <li key={event.id} className="flex items-start gap-3 px-4 py-3">
                                <span
                                  className={
                                    event.type.includes("fail")
                                      ? "mt-1.5 size-2 shrink-0 rounded-full bg-danger"
                                      : "mt-1.5 size-2 shrink-0 rounded-full bg-fg-disabled"
                                  }
                                />
                                <div className="min-w-0 flex-1">
                                  <p className="font-mono text-xs text-fg">{event.type}</p>
                                  <p className="mt-1 font-mono text-2xs text-fg-muted">{JSON.stringify(event.payload)}</p>
                                </div>
                                <span className="shrink-0 text-2xs tabular text-fg-muted">
                                  {formatRelativeShort(event.occurred_at, new Date())} ago
                                </span>
                              </li>
                            ))}
                          </ol>
                          {timelineQuery.hasMore && (
                            <div className="flex justify-center border-t border-line-subtle p-3">
                              <Button variant="secondary" size="sm" loading={timelineQuery.isFetching} onClick={() => void timelineQuery.fetchNext()}>
                                Load older events
                              </Button>
                            </div>
                          )}
                        </>
                      )}
                    </CardBody>
                  </Card>
                </Section>
              </TabsContent>

              <TabsContent value="conversations">
                <Card>
                  <CardBody className="p-0">
                    {conversationsQuery.isLoading ? (
                      <p className="p-5 text-sm text-fg-muted">Loading conversations…</p>
                    ) : conversationsQuery.error ? (
                      <div className="flex items-center justify-between gap-3 p-5"><p className="text-sm text-danger">Could not load conversations.</p><Button variant="ghost" size="xs" onClick={conversationsQuery.refetch}>Retry</Button></div>
                    ) : theirConversations.length === 0 ? (
                      <EmptyState size="sm" title="No conversations yet" />
                    ) : (
                      <>
                        <ul className="divide-y divide-line-subtle">
                          {theirConversations.map((conversation) => (
                            <li key={conversation.id}>
                              <Link
                                to={`/inbox/all/${conversation.id}`}
                                className="flex items-center gap-3 px-4 py-3 transition-colors hover:bg-surface-hover"
                              >
                                <span className="min-w-0 flex-1">
                                  <span className="block truncate text-sm text-fg">
                                    {conversation.subject ?? conversation.last_message_preview}
                                  </span>
                                  <span className="block truncate text-xs text-fg-muted">
                                    {conversation.message_count} messages · {formatRelativeShort(conversation.last_message_at, new Date())} ago
                                  </span>
                                </span>
                                <ConversationStateBadge state={conversation.state} />
                              </Link>
                            </li>
                          ))}
                        </ul>
                        {conversationsQuery.hasMore && <div className="flex justify-center border-t border-line-subtle p-3"><Button variant="secondary" size="sm" loading={conversationsQuery.isFetching} onClick={() => void conversationsQuery.fetchNext()}>Load older conversations</Button></div>}
                      </>
                    )}
                  </CardBody>
                </Card>
              </TabsContent>

              <TabsContent value="tickets">
                <Card>
                  <CardBody className="p-0">
                    {ticketsQuery.isLoading ? (
                      <p className="p-5 text-sm text-fg-muted">Loading tickets…</p>
                    ) : ticketsQuery.error ? (
                      <div className="flex items-center justify-between gap-3 p-5"><p className="text-sm text-danger">Could not load tickets.</p><Button variant="ghost" size="xs" onClick={ticketsQuery.refetch}>Retry</Button></div>
                    ) : theirTickets.length === 0 ? (
                      <EmptyState size="sm" title="No tickets yet" />
                    ) : (
                      <>
                        <ul className="divide-y divide-line-subtle">
                          {theirTickets.map((ticket) => (
                            <li key={ticket.id}>
                              <Link
                                to={`/tickets/${ticket.id}`}
                                className="flex items-center gap-3 px-4 py-3 transition-colors hover:bg-surface-hover"
                              >
                                <span className="shrink-0 font-mono text-xs text-fg-muted">
                                  {ticket.prefix}-{ticket.number}
                                </span>
                                <span className="min-w-0 flex-1 truncate text-sm text-fg">{ticket.title}</span>
                                <TicketStatusBadge status={ticket.status} />
                              </Link>
                            </li>
                          ))}
                        </ul>
                        {ticketsQuery.hasMore && <div className="flex justify-center border-t border-line-subtle p-3"><Button variant="secondary" size="sm" loading={ticketsQuery.isFetching} onClick={() => void ticketsQuery.fetchNext()}>Load older tickets</Button></div>}
                      </>
                    )}
                  </CardBody>
                </Card>
              </TabsContent>

              <TabsContent value="attributes">
                <AttributesTab customer={customer} />
              </TabsContent>

              <TabsContent value="sessions">
                <Card>
                  <CardBody className="p-0">
                    {sessionsQuery.isLoading ? (
                      <p className="p-5 text-sm text-fg-muted">Loading sessions…</p>
                    ) : sessionsQuery.error ? (
                      <div className="flex items-center justify-between gap-3 p-5"><p className="text-sm text-danger">Could not load sessions.</p><Button variant="ghost" size="xs" onClick={sessionsQuery.refetch}>Retry</Button></div>
                    ) : sessions.length === 0 ? (
                      <EmptyState size="sm" title="No sessions recorded" description="Sessions are recorded once the widget SDK is connected." />
                    ) : (
                      <>
                        <ul className="divide-y divide-line-subtle">
                          {sessions.map((session) => (
                            <li key={session.id} className="flex items-center gap-3 px-4 py-3">
                              <span className="min-w-0 flex-1">
                                <span className="block truncate text-sm text-fg">{session.current_url ?? "No page recorded"}</span>
                                <span className="block text-xs text-fg-muted">
                                  {[session.browser, session.os, session.device].filter(Boolean).join(" · ") || "Unknown device"}
                                </span>
                              </span>
                              <Badge tone={session.ended_at ? "neutral" : "success"}>{session.ended_at ? "Ended" : "Active"}</Badge>
                            </li>
                          ))}
                        </ul>
                        {sessionsQuery.hasMore && <div className="flex justify-center border-t border-line-subtle p-3"><Button variant="secondary" size="sm" loading={sessionsQuery.isFetching} onClick={() => void sessionsQuery.fetchNext()}>Load older sessions</Button></div>}
                      </>
                    )}
                  </CardBody>
                </Card>
              </TabsContent>
            </Tabs>
          </div>
        </div>
      </PageBody>

      {merging && <MergeDialog customer={customer} onClose={() => setMerging(false)} />}

      <ConfirmDialog
        open={blocking}
        onOpenChange={setBlocking}
        title="Block this customer?"
        description="They will no longer be able to start new conversations."
        confirmLabel="Block"
        destructive
        loading={blockCustomer.isPending}
        onConfirm={() => void blockCustomer.mutate().catch(() => {})}
      />
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title="Delete and anonymise this customer?"
        description="Their name, email, phone, and attributes are cleared, and their event/session history is erased. Conversations and tickets keep their history but no longer identify this person. This cannot be undone."
        confirmLabel="Delete and anonymise"
        destructive
        loading={deleteCustomer.isPending}
        confirmationPhrase={customer.name ?? "delete"}
        onConfirm={() => void deleteCustomer.mutate().catch(() => {})}
      />
    </Page>
  );
}

function AttributesTab({ customer }: { customer: Customer }) {
  const definitions = useQuery<{ data: AttributeDefinition[] }>(
    ["attribute-definitions", "customer"],
    (signal) => api.get(`/attribute-definitions?entity_type=customer`, { signal }),
  );
  const defByKey = new Map((definitions.data?.data ?? []).map((d) => [d.key, d]));
  const maskedKeys = new Set(customer.masked_attribute_keys);

  const reveal = useMutation<string, { key: string; value: unknown }>(
    (key) => api.post(`/customers/${customer.id}/attributes/${key}/reveal`, {}),
  );
  const [revealed, setRevealed] = useState<Record<string, unknown>>({});

  const entries = Object.entries(customer.attributes);

  return (
    <>
      <Callout tone="info" className="mb-4">
        Attributes arrive from the JavaScript SDK, the REST API, or a signed identity token. Which sources are accepted for
        each key is configured in Developers → Metadata schema.
      </Callout>

      <Card>
        <CardBody className="p-0">
          {entries.length === 0 ? (
            <EmptyState size="sm" title="No attributes set" />
          ) : (
            <ul className="divide-y divide-line-subtle">
              {entries.map(([key, value]) => {
                const definition = defByKey.get(key);
                const isMasked = maskedKeys.has(key);
                return (
                  <li key={key} className="flex items-center gap-3 px-4 py-2.5">
                    <span className="w-40 shrink-0">
                      <span className="block font-mono text-xs text-fg-secondary">{key}</span>
                      {definition && <span className="block text-2xs text-fg-muted">{definition.label}</span>}
                    </span>
                    <span className="min-w-0 flex-1 truncate text-xs text-fg">
                      {isMasked && !(key in revealed) ? (
                        <button
                          type="button"
                          className="flex items-center gap-1 text-fg-muted hover:text-accent-text"
                          onClick={() =>
                            void reveal
                              .mutate(key)
                              .then((res) => setRevealed((r) => ({ ...r, [key]: (res as unknown as { value: unknown }).value })))
                              .catch(() => {})
                          }
                        >
                          <EyeOff className="size-3" /> •••••• Reveal
                        </button>
                      ) : Array.isArray(revealed[key] ?? value) ? (
                        (revealed[key] ?? value as unknown[]).toString()
                      ) : (
                        String(revealed[key] ?? value)
                      )}
                    </span>
                    {definition?.sensitive && <Badge tone="warning">Sensitive</Badge>}
                    <Badge tone="neutral">{definition?.type ?? "string"}</Badge>
                  </li>
                );
              })}
            </ul>
          )}
        </CardBody>
      </Card>

      <Section title="Identify this customer" className="mt-6">
        <p className="mb-2 text-xs text-fg-muted">Generate the signed token server-side. Never expose your secret in browser code.</p>
        <CodeBlock
          language="javascript"
          code={`Hubchat('identify', {
  external_id: ${JSON.stringify(customer.external_id ?? "u_00000")},
  email: ${JSON.stringify(customer.email ?? "customer@example.com")},
  token: identityToken, // HMAC-signed by your server
});`}
        />
      </Section>
    </>
  );
}

function MergeDialog({ customer, onClose }: { customer: Customer; onClose: () => void }) {
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  const [target, setTarget] = useState<Customer | null>(null);

  const results = useQuery<{ data: Customer[] }>(
    query.trim().length > 1 ? ["customers", "search", query] : null,
    (signal) => api.get(`/customers?q=${encodeURIComponent(query)}&limit=10`, { signal }),
  );

  const preview = useQuery<{ conversation_count: number; ticket_count: number; tag_count: number; company_count: number }>(
    target ? ["merge-preview", customer.id, target.id] : null,
    (signal) => api.post(`/customers/merge/preview`, { winner_id: customer.id, loser_id: target?.id }, { signal }),
  );

  const merge = useMutation<void, unknown>(
    () => api.post(`/customers/merge`, { winner_id: customer.id, loser_id: target?.id }),
    {
      invalidates: [["customers"]],
      onSuccess: () => {
        onClose();
        navigate(`/customers/${customer.id}`);
      },
    },
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title="Merge with another customer"
        description="The other customer's conversations and tickets move here, and their record is removed. Reversible for 30 days."
        footer={
          target && (
            <Button variant="primary" size="sm" loading={merge.isPending} onClick={() => void merge.mutate().catch(() => {})}>
              Merge into {customer.name ?? "this customer"}
            </Button>
          )
        }
      >
        {merge.error ? (
          <Callout tone="danger" className="mb-3">
            {merge.error instanceof ApiError ? merge.error.message : "Could not merge these customers."}
          </Callout>
        ) : null}

        {!target ? (
          <>
            <Input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search by name or email…" autoFocus />
            <ul className="mt-2 flex max-h-64 flex-col gap-1 overflow-y-auto">
              {(results.data?.data ?? [])
                .filter((c) => c.id !== customer.id)
                .map((c) => (
                  <li key={c.id}>
                    <button type="button" onClick={() => setTarget(c)} className="w-full rounded-md px-2 py-2 text-left text-sm hover:bg-inset">
                      <span className="block truncate text-fg">{c.name ?? "Unnamed"}</span>
                      <span className="block truncate text-xs text-fg-muted">{c.email ?? "—"}</span>
                    </button>
                  </li>
                ))}
            </ul>
          </>
        ) : (
          <div>
            <p className="mb-2 text-sm text-fg">
              Merging <span className="font-medium">{target.name ?? "Unnamed"}</span> into{" "}
              <span className="font-medium">{customer.name ?? "this customer"}</span> will move:
            </p>
            {preview.data ? (
              <ul className="list-inside list-disc text-sm text-fg-secondary">
                <li>{preview.data.conversation_count} conversations</li>
                <li>{preview.data.ticket_count} tickets</li>
                <li>{preview.data.tag_count} tags</li>
                <li>{preview.data.company_count} company links</li>
              </ul>
            ) : (
              <p className="text-xs text-fg-muted">Loading preview…</p>
            )}
            <Button variant="ghost" size="sm" className="mt-3" onClick={() => setTarget(null)}>
              Choose someone else
            </Button>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
