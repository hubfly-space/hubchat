import {
  Badge,
  ApiError,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  CodeBlock,
  Dialog,
  DialogContent,
  DialogTrigger,
  Field,
  Input,
  Page,
  PageBody,
  PageHeader,
  RadioGroup,
  Section,
  Select,
  SettingsRow,
  Tabs,
  TabsContent,
  TabsList,
  api,
  idempotencyKey,
  useMutation,
  useInfinite,
  useQuery,
  type Paginated,
} from "@hubchat/shared";
import { AlertTriangle, CheckCircle2, RefreshCw, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useWorkspace } from "../../app/workspace-context";

type Mailbox = {
  id: string;
  address: string;
  display_name: string;
  inbox_id: string;
  inbound_mode: "webhook" | "imap" | "off";
  imap_configured: boolean;
  inbound_secret_configured: boolean;
  imap_host?: string | null;
  imap_port?: number | null;
  imap_username?: string | null;
  allowed_senders?: string[];
  blocked_senders?: string[];
  enabled: boolean;
  last_polled_at?: string | null;
  last_error?: string | null;
};
type CreatedMailbox = { mailbox: Mailbox; inbound_secret: string };
type DeliveryEvent = { id: string; provider: string; type: string; recipient?: string | null; bounce_type?: string | null; reason?: string | null; hard: boolean; occurred_at: string };
type Suppression = { address: string; reason: string; source: string; updated_at: string };
type EmailStatus = { configured: boolean; host: string; port: number; from_address: string; encryption: string };

/** Email channel (§6.15). */
export default function EmailChannel() {
  const { inboxes } = useWorkspace();
  const [tab, setTab] = useState("outbound");
  const [createOpen, setCreateOpen] = useState(false);
  const [newAddress, setNewAddress] = useState("");
  const [newInboxID, setNewInboxID] = useState("");
  const [newSecret, setNewSecret] = useState("");
  const [imapHost, setImapHost] = useState("");
  const [imapPort, setImapPort] = useState("993");
  const [imapUsername, setImapUsername] = useState("");
  const [imapPassword, setImapPassword] = useState("");
  const emailStatus = useQuery<EmailStatus>(["email-status"], (signal) => api.get("/email/status", { signal }));
  const mailboxes = useInfinite<Mailbox>(["email-mailboxes"], (cursor, signal) => api.get<Paginated<Mailbox>>(`/email/mailboxes?limit=25${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }));
  const [activeID, setActiveID] = useState("");
  const active = mailboxes.items.find((item) => item.id === activeID) ?? mailboxes.items[0];
  const [inboundMode, setInboundMode] = useState<Mailbox["inbound_mode"]>("off");
  const update = useMutation<Record<string, unknown>, Mailbox>((input) => api.patch(`/email/mailboxes/${encodeURIComponent(active?.id ?? "")}`, input, { idempotencyKey: idempotencyKey() }), { invalidates: [["email-mailboxes"]] });
  const create = useMutation<{ address: string; inbox_id: string; inbound_mode: "webhook"; enabled: boolean }, CreatedMailbox>((input) => api.post("/email/mailboxes", input, { idempotencyKey: idempotencyKey() }), { invalidates: [["email-mailboxes"]], onSuccess: (value) => { setCreateOpen(false); setNewAddress(""); setNewInboxID(""); setNewSecret(value.inbound_secret); } });
  const deliveryEvents = useInfinite<DeliveryEvent>(active ? ["email-delivery-events", active.id] : null, (cursor, signal) => {
    const params = new URLSearchParams({ limit: "20" });
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<DeliveryEvent>>(`/email/mailboxes/${encodeURIComponent(active?.id ?? "")}/delivery-events?${params.toString()}`, { signal, fresh: true });
  });
  const suppressions = useInfinite<Suppression>(["email-suppressions"], (cursor, signal) => {
    const params = new URLSearchParams({ limit: "50" });
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<Suppression>>(`/email/suppressions?${params.toString()}`, { signal });
  });
  const removeSuppression = useMutation<string, void>((address) => api.delete(`/email/mailboxes/${encodeURIComponent(active?.id ?? "")}/suppressions/${encodeURIComponent(address)}`), { invalidates: [["email-suppressions"]] });

  useEffect(() => {
    if (active) {
      setActiveID(active.id);
      setInboundMode(active.inbound_mode);
      setImapHost(active.imap_host ?? "");
      setImapPort(String(active.imap_port ?? 993));
      setImapUsername(active.imap_username ?? "");
      setImapPassword("");
    }
  }, [active]);

  return (
    <Page>
      <PageHeader
        title="Email channel"
        description="Outbound notifications and inbound reply handling. Processed inside the Hubchat binary — no external worker."
        actions={<Dialog open={createOpen} onOpenChange={setCreateOpen}><DialogTrigger asChild><Button variant="primary" size="sm">Add mailbox</Button></DialogTrigger><DialogContent title="Add inbound mailbox" footer={<><Button variant="ghost" size="sm" onClick={() => setCreateOpen(false)}>Cancel</Button><Button variant="primary" size="sm" loading={create.isPending} disabled={!newAddress.trim() || (!newInboxID && !inboxes[0]?.id)} onClick={() => void create.mutate({ address: newAddress.trim(), inbox_id: newInboxID || inboxes[0]?.id || "", inbound_mode: "webhook", enabled: true }).catch(() => {})}>Create mailbox</Button></>}><div className="space-y-4"><Field label="Support address"><Input autoFocus value={newAddress} onChange={(event) => setNewAddress(event.target.value)} placeholder="support@example.com" /></Field><Field label="Default inbox"><Select value={newInboxID || inboxes[0]?.id} onValueChange={setNewInboxID} options={inboxes.map((inbox) => ({ value: inbox.id, label: inbox.name }))} /></Field>{Boolean(create.error) && <p className="text-sm text-danger">Could not create mailbox.</p>}</div></DialogContent></Dialog>}
        tabs={
          <Tabs value={tab} onValueChange={setTab}>
            <TabsList
              items={[
                { value: "outbound", label: "Outbound" },
                { value: "inbound", label: "Inbound" },
                { value: "templates", label: "Templates" },
                { value: "deliverability", label: "Deliverability" },
              ]}
            />
          </Tabs>
        }
      />

      <PageBody>
        {mailboxes.isLoading && <Callout tone="info" className="mb-5">Loading live mailbox configuration…</Callout>}
        {Boolean(mailboxes.error) && <Callout tone="danger" className="mb-5">{mailboxes.error instanceof ApiError ? mailboxes.error.message : "Mailbox configuration is unavailable."}</Callout>}
        {newSecret && <Callout tone="success" className="mb-5" title="Save this inbound webhook secret now">It is shown only once: <code className="ml-1 font-mono text-xs">{newSecret}</code><Button className="ml-2" variant="ghost" size="xs" onClick={() => setNewSecret("")}>Dismiss</Button></Callout>}
        {active && <div className="mb-5 flex flex-wrap items-center gap-2 rounded-lg border border-line bg-surface px-3 py-2"><span className="text-xs text-fg-muted">Mailbox</span>{mailboxes.items.map((item) => <Button key={item.id} variant={item.id === active.id ? "secondary" : "ghost"} size="xs" onClick={() => setActiveID(item.id)}>{item.address}</Button>)}{mailboxes.hasMore && <Button variant="ghost" size="xs" loading={mailboxes.isFetching} onClick={() => void mailboxes.fetchNext()}>Load more</Button>}<Badge tone={active.inbound_secret_configured ? "success" : "warning"}>{active.inbound_secret_configured ? "Webhook secret configured" : "Webhook secret missing"}</Badge></div>}
        <Tabs value={tab} onValueChange={setTab}>
          <TabsContent value="outbound">
            {emailStatus.isLoading ? <Callout tone="info" className="mb-5">Checking outbound email readiness…</Callout> : emailStatus.error ? <Callout tone="danger" className="mb-5">Could not load outbound email readiness.</Callout> : !emailStatus.data?.configured ? <Callout tone="warning" className="mb-5" icon={<AlertTriangle />}>No SMTP server is configured. Outbound mail is being queued, not sent — customers are not receiving ticket notifications, magic links, or password resets.</Callout> : <Callout tone="success" className="mb-5" icon={<CheckCircle2 />}>Outbound mail is configured for <span className="font-mono">{emailStatus.data.from_address}</span> via <span className="font-mono">{emailStatus.data.host}:{emailStatus.data.port}</span>.</Callout>}

            <Section title="SMTP">
              <Card>
                <CardBody className="pt-0">
                  <Callout tone="info" className="mb-4">
                    Outbound SMTP is configured at the deployment level. These values are read-only here;
                    set <code className="font-mono">HUBCHAT_SMTP_*</code> and restart the worker to change them.
                  </Callout>
                  <SettingsRow label="Host" htmlFor="smtp-host">
                    <Input id="smtp-host" inputSize="sm" mono readOnly value={emailStatus.data?.host ?? ""} placeholder="Not configured" />
                  </SettingsRow>
                  <SettingsRow label="Port" htmlFor="smtp-port">
                    <Input id="smtp-port" inputSize="sm" type="number" readOnly value={emailStatus.data?.port ?? ""} className="max-w-32" />
                  </SettingsRow>
                  <SettingsRow label="Username" htmlFor="smtp-user">
                    <Input id="smtp-user" inputSize="sm" mono readOnly value="Configured outside Hubchat" />
                  </SettingsRow>
                  <SettingsRow
                    label="Password"
                    description="Stored encrypted. Never returned by the API or written to logs (§11.5)."
                    htmlFor="smtp-pass"
                  >
                    <Input id="smtp-pass" inputSize="sm" type="password" readOnly value="********" />
                  </SettingsRow>
                  <SettingsRow label="Encryption">
                    <Select
                      size="sm"
                      value={emailStatus.data?.encryption ?? ""}
                      disabled
                      aria-label="Encryption"
                      options={[
                        { value: "starttls", label: "STARTTLS", description: "Recommended" },
                        { value: "tls", label: "Implicit TLS" },
                        { value: "none", label: "None", description: "Only for a trusted local relay" },
                      ]}
                    />
                  </SettingsRow>
                </CardBody>
              </Card>
            </Section>

            <Section title="Sender identity">
              <Card>
                <CardBody className="pt-0">
                  <SettingsRow label="From name" htmlFor="from-name">
                    <Input id="from-name" inputSize="sm" readOnly value="Configured outside Hubchat" />
                  </SettingsRow>
                  <SettingsRow label="From address" htmlFor="from-email">
                    <Input id="from-email" inputSize="sm" mono readOnly value={emailStatus.data?.from_address ?? ""} placeholder="Not configured" />
                  </SettingsRow>
                  <SettingsRow
                    label="Reply-to"
                    description="Where customer replies are delivered so they thread back onto the conversation."
                    htmlFor="reply-to"
                  >
                    <Input id="reply-to" inputSize="sm" mono readOnly value="Derived from the inbound mailbox" />
                  </SettingsRow>
                </CardBody>
              </Card>

              <p className="mt-3 text-2xs text-fg-muted">
                A test message can be verified by creating a mailbox and sending a real customer notification;
                arbitrary SMTP test delivery is not exposed without a recipient audit trail.
              </p>
            </Section>
          </TabsContent>

          <TabsContent value="inbound">
            <Section
              title="How inbound mail reaches Hubchat"
              description="Provider webhooks are preferred; IMAP exists for self-hosted deployments that cannot receive inbound HTTP."
            >
              <Card>
                <CardBody>
                  <RadioGroup
                    variant="cards"
                    aria-label="Inbound mode"
                    value={inboundMode}
                    onValueChange={(value) => {
                      const mode = value as Mailbox["inbound_mode"];
                      setInboundMode(mode);
                      if (active) void update.mutate({ inbound_mode: mode });
                    }}
                    options={[
                      {
                        value: "webhook",
                        label: "Provider webhook",
                        description: "Your provider POSTs each message to Hubchat. Lowest latency, no polling.",
                      },
                      {
                        value: "imap",
                        label: "IMAP polling",
                        description: "Hubchat checks a mailbox on an interval. Works behind a firewall; adds delay.",
                      },
                      {
                        value: "off",
                        label: "Disabled",
                        description: "Replies by email are not accepted. Customers use the portal or widget.",
                      },
                    ]}
                  />
                </CardBody>
              </Card>
            </Section>

            {inboundMode === "webhook" && (
              <Section title="Webhook endpoint">
                <Card>
                  <CardBody>
                    <p className="mb-3 text-xs text-fg-muted">
                      Point your provider's inbound parse hook at this URL. Requests are verified by
                      signature; unsigned requests are rejected.
                    </p>
                    <CodeBlock
                      code={`${window.location.origin}/api/v1/email/inbound/postmark`}
                      language="url"
                    />
                  </CardBody>
                </Card>
              </Section>
            )}

            {inboundMode === "imap" && (
              <Section title="IMAP mailbox">
                <Card>
                  <CardBody className="pt-0">
                    <SettingsRow label="Host" htmlFor="imap-host">
                      <Input id="imap-host" inputSize="sm" mono value={imapHost} onChange={(event) => setImapHost(event.target.value)} placeholder="imap.example.com" />
                    </SettingsRow>
                    <SettingsRow label="Username" htmlFor="imap-user">
                      <Input id="imap-user" inputSize="sm" mono value={imapUsername} onChange={(event) => setImapUsername(event.target.value)} />
                    </SettingsRow>
                    <SettingsRow label="Password" htmlFor="imap-pass" description="Stored encrypted and never returned by the API.">
                      <Input id="imap-pass" inputSize="sm" type="password" value={imapPassword} onChange={(event) => setImapPassword(event.target.value)} placeholder={active?.imap_configured ? "Leave blank to keep current password" : "IMAP password"} />
                    </SettingsRow>
                    <SettingsRow label="Port" htmlFor="imap-port">
                      <Input id="imap-port" inputSize="sm" type="number" value={imapPort} onChange={(event) => setImapPort(event.target.value)} className="max-w-32" />
                    </SettingsRow>
                    <div className="mt-4 flex justify-end">
                      <Button
                        variant="secondary"
                        size="sm"
                        loading={update.isPending}
                        disabled={!active || !imapHost.trim() || !imapUsername.trim() || !Number(imapPort)}
                        onClick={() => void update.mutate({ imap_host: imapHost.trim(), imap_port: Number(imapPort), imap_username: imapUsername.trim(), ...(imapPassword.trim() ? { imap_password: imapPassword } : {}) }).then(() => setImapPassword("")).catch(() => {})}
                      >
                        Save IMAP settings
                      </Button>
                    </div>
                  </CardBody>
                </Card>
                {active?.last_error && <Callout tone="danger" className="mt-3" title="Last IMAP poll failed">{active.last_error}</Callout>}
                <p className="mt-2 text-2xs text-fg-muted">
                  {active?.last_polled_at ? `Last checked ${new Date(active.last_polled_at).toLocaleString()}.` : "IMAP has not been checked yet."}
                </p>
              </Section>
            )}

            <Section title="Routing">
              <Card>
                <CardBody className="pt-0">
                  <SettingsRow label="Default inbox" description="Where mail lands when no rule matches.">
                    <Select
                      size="sm"
                      value={active?.inbox_id ?? inboxes[0]?.id}
                      aria-label="Default inbox"
                      options={inboxes.map((inbox) => ({ value: inbox.id, label: inbox.name }))}
                      onValueChange={(value) => { if (active) void update.mutate({ inbox_id: value }); }}
                    />
                  </SettingsRow>
                  <SettingsRow
                    label="Strip quoted replies"
                    description="Removes the quoted history below the reply so the timeline stays readable."
                  >
                    <Badge tone="neutral">Provider default</Badge>
                  </SettingsRow>
                  <SettingsRow
                    label="Accept mail from unknown senders"
                    description="Off means only addresses already attached to a customer can create conversations."
                  >
                    <Badge tone="neutral">Configured by provider policy</Badge>
                  </SettingsRow>
                </CardBody>
              </Card>
            </Section>
          </TabsContent>

          <TabsContent value="templates">
            <Section
              title="Notification templates"
              description="Plain-text and HTML variants ship with the binary. Overrides are stored per workspace."
            >
              <Card>
                <CardBody className="p-0">
                  <ul className="divide-y divide-line-subtle">
                    {[
                      "Ticket created",
                      "Agent replied",
                      "Ticket resolved",
                      "Survey request",
                      "Portal magic link",
                      "Transcript delivery",
                    ].map((template) => (
                      <li key={template} className="flex items-center gap-3 px-4 py-2.5">
                        <span className="min-w-0 flex-1 text-sm text-fg">{template}</span>
                        <Badge tone="neutral">Binary default</Badge>
                      </li>
                    ))}
                  </ul>
                </CardBody>
              </Card>
            </Section>
          </TabsContent>

          <TabsContent value="deliverability">
            <Section title="DNS records">
              <Card>
                <CardBody>
                  <Callout tone="info" icon={<AlertTriangle />}>
                    DNS verification is not performed by the current binary. Verify SPF, DKIM, and DMARC
                    for the configured sender domain with your DNS provider before enabling customer mail.
                  </Callout>
                </CardBody>
              </Card>
            </Section>

            <Section title="Recent delivery events">
              <Card>
                <CardHeader
                  title="Bounces and complaints"
                  description="Recorded when the provider reports them. Addresses with a hard bounce are suppressed automatically."
                />
                <CardBody>
                  {suppressions.isLoading ? <p className="text-xs text-fg-muted">Loading suppression list…</p> : suppressions.error ? <div className="flex items-center justify-between gap-3"><p className="text-xs text-danger">Could not load suppressed addresses.</p><Button variant="ghost" size="xs" leading={<RefreshCw />} onClick={suppressions.refetch}>Retry</Button></div> : suppressions.items.length ? <><div className="space-y-2">{suppressions.items.map((item) => <div key={item.address} className="flex items-center gap-3 rounded-md border border-line-subtle px-3 py-2"><div className="min-w-0 flex-1"><p className="truncate font-mono text-xs text-fg">{item.address}</p><p className="text-2xs text-fg-muted">{item.reason} · {item.source}</p></div><Button variant="ghost" size="xs" leading={<Trash2 />} loading={removeSuppression.isPending} onClick={() => void removeSuppression.mutate(item.address).catch(() => {})}>Remove</Button></div>)}</div>{suppressions.hasMore && <div className="flex justify-center pt-3"><Button variant="secondary" size="xs" loading={suppressions.isFetching} onClick={() => void suppressions.fetchNext()}>Load more suppressed addresses</Button></div>}</> : <p className="text-xs text-fg-muted">No suppressed addresses.</p>}
                </CardBody>
              </Card>
              <Card className="mt-3">
                <CardHeader title="Delivery event history" description="Provider callbacks are retained for troubleshooting and replay-safe status updates." />
                <CardBody className="p-0">
                  {deliveryEvents.isLoading ? <p className="px-4 py-5 text-xs text-fg-muted">Loading delivery events…</p> : deliveryEvents.error ? <div className="flex items-center justify-between gap-3 px-4 py-5"><p className="text-xs text-danger">Could not load delivery events.</p><Button variant="ghost" size="xs" leading={<RefreshCw />} onClick={deliveryEvents.refetch}>Retry</Button></div> : deliveryEvents.items.length ? <><ul className="divide-y divide-line-subtle">{deliveryEvents.items.map((event) => <li key={event.id} className="flex items-start gap-3 px-4 py-2.5"><span className={`mt-1.5 size-2 shrink-0 rounded-full ${event.type === "bounced" ? "bg-danger" : event.type === "delivered" ? "bg-success-text" : "bg-warning-text"}`} /><div className="min-w-0 flex-1"><p className="text-xs text-fg">{event.type} {event.recipient ? `· ${event.recipient}` : ""}</p><p className="text-2xs text-fg-muted">{event.provider}{event.reason ? ` · ${event.reason}` : ""}</p></div><time className="shrink-0 text-2xs tabular text-fg-muted">{new Date(event.occurred_at).toLocaleString()}</time></li>)}</ul>{deliveryEvents.hasMore && <div className="flex justify-center border-t border-line-subtle p-3"><Button variant="secondary" size="xs" loading={deliveryEvents.isFetching} onClick={() => void deliveryEvents.fetchNext()}>Load older events</Button></div>}</> : <p className="px-4 py-5 text-xs text-fg-muted">No delivery events recorded.</p>}
                </CardBody>
              </Card>
            </Section>
          </TabsContent>
        </Tabs>
      </PageBody>
    </Page>
  );
}
