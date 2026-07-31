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
  Switch,
  Tabs,
  TabsContent,
  TabsList,
  api,
  idempotencyKey,
  useMutation,
  useQuery,
} from "@hubchat/shared";
import { AlertTriangle, CheckCircle2, Send } from "lucide-react";
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
  enabled: boolean;
  last_error?: string | null;
};
type CreatedMailbox = { mailbox: Mailbox; inbound_secret: string };

/** Email channel (§6.15). */
export default function EmailChannel() {
  const { inboxes } = useWorkspace();
  const [tab, setTab] = useState("outbound");
  const [createOpen, setCreateOpen] = useState(false);
  const [newAddress, setNewAddress] = useState("");
  const [newInboxID, setNewInboxID] = useState("");
  const [newSecret, setNewSecret] = useState("");
  const mailboxes = useQuery<{ data: Mailbox[] }>(["email-mailboxes"], (signal) => api.get("/email/mailboxes", { signal }));
  const [activeID, setActiveID] = useState("");
  const active = mailboxes.data?.data.find((item) => item.id === activeID) ?? mailboxes.data?.data[0];
  const [inboundMode, setInboundMode] = useState<Mailbox["inbound_mode"]>("off");
  const update = useMutation<Partial<Mailbox>, Mailbox>((input) => api.patch(`/email/mailboxes/${encodeURIComponent(active?.id ?? "")}`, input, { idempotencyKey: idempotencyKey() }), { invalidates: [["email-mailboxes"]] });
  const create = useMutation<{ address: string; inbox_id: string; inbound_mode: "webhook"; enabled: boolean }, CreatedMailbox>((input) => api.post("/email/mailboxes", input, { idempotencyKey: idempotencyKey() }), { invalidates: [["email-mailboxes"]], onSuccess: (value) => { setCreateOpen(false); setNewAddress(""); setNewInboxID(""); setNewSecret(value.inbound_secret); } });

  useEffect(() => {
    if (active) {
      setActiveID(active.id);
      setInboundMode(active.inbound_mode);
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
        {mailboxes.isError && <Callout tone="danger" className="mb-5">{mailboxes.error instanceof ApiError ? mailboxes.error.message : "Mailbox configuration is unavailable."}</Callout>}
        {newSecret && <Callout tone="success" className="mb-5" title="Save this inbound webhook secret now">It is shown only once: <code className="ml-1 font-mono text-xs">{newSecret}</code><Button className="ml-2" variant="ghost" size="xs" onClick={() => setNewSecret("")}>Dismiss</Button></Callout>}
        {active && <div className="mb-5 flex flex-wrap items-center gap-2 rounded-lg border border-line bg-surface px-3 py-2"><span className="text-xs text-fg-muted">Mailbox</span>{mailboxes.data?.data.map((item) => <Button key={item.id} variant={item.id === active.id ? "secondary" : "ghost"} size="xs" onClick={() => setActiveID(item.id)}>{item.address}</Button>)}<Badge tone={active.inbound_secret_configured ? "success" : "warning"}>{active.inbound_secret_configured ? "Webhook secret configured" : "Webhook secret missing"}</Badge></div>}
        <Tabs value={tab} onValueChange={setTab}>
          <TabsContent value="outbound">
            <Callout tone="warning" className="mb-5" icon={<AlertTriangle />}>
              No SMTP server is configured. Outbound mail is being queued, not sent — customers are
              not receiving ticket notifications, magic links, or password resets.
            </Callout>

            <Section title="SMTP">
              <Card>
                <CardBody className="pt-0">
                  <SettingsRow label="Host" htmlFor="smtp-host">
                    <Input id="smtp-host" inputSize="sm" mono placeholder="smtp.postmarkapp.com" />
                  </SettingsRow>
                  <SettingsRow label="Port" htmlFor="smtp-port">
                    <Input id="smtp-port" inputSize="sm" type="number" defaultValue={587} className="max-w-32" />
                  </SettingsRow>
                  <SettingsRow label="Username" htmlFor="smtp-user">
                    <Input id="smtp-user" inputSize="sm" mono />
                  </SettingsRow>
                  <SettingsRow
                    label="Password"
                    description="Stored encrypted. Never returned by the API or written to logs (§11.5)."
                    htmlFor="smtp-pass"
                  >
                    <Input id="smtp-pass" inputSize="sm" type="password" />
                  </SettingsRow>
                  <SettingsRow label="Encryption">
                    <Select
                      size="sm"
                      defaultValue="starttls"
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
                    <Input id="from-name" inputSize="sm" defaultValue="Northwind Support" />
                  </SettingsRow>
                  <SettingsRow label="From address" htmlFor="from-email">
                    <Input id="from-email" inputSize="sm" mono defaultValue="support@northwind.cloud" />
                  </SettingsRow>
                  <SettingsRow
                    label="Reply-to"
                    description="Where customer replies are delivered so they thread back onto the conversation."
                    htmlFor="reply-to"
                  >
                    <Input id="reply-to" inputSize="sm" mono defaultValue="reply@in.northwind.cloud" />
                  </SettingsRow>
                </CardBody>
              </Card>

              <div className="mt-3 flex justify-end">
                <Button variant="secondary" size="sm" leading={<Send />}>
                  Send a test email
                </Button>
              </div>
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
                      code="https://support.northwind.cloud/api/v1/email/inbound/postmark"
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
                      <Input id="imap-host" inputSize="sm" mono placeholder="imap.example.com" />
                    </SettingsRow>
                    <SettingsRow label="Username" htmlFor="imap-user">
                      <Input id="imap-user" inputSize="sm" mono />
                    </SettingsRow>
                    <SettingsRow label="Poll interval" htmlFor="imap-interval">
                      <Input id="imap-interval" inputSize="sm" type="number" suffix="seconds" defaultValue={60} />
                    </SettingsRow>
                  </CardBody>
                </Card>
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
                    <Switch defaultChecked aria-label="Strip quoted replies" />
                  </SettingsRow>
                  <SettingsRow
                    label="Accept mail from unknown senders"
                    description="Off means only addresses already attached to a customer can create conversations."
                  >
                    <Switch defaultChecked aria-label="Accept unknown senders" />
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
                        <Badge tone="neutral">Default</Badge>
                        <Button variant="ghost" size="sm">
                          Customise
                        </Button>
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
                <CardBody className="space-y-3">
                  {[
                    { label: "SPF", status: "ok", value: "v=spf1 include:spf.postmarkapp.com ~all" },
                    { label: "DKIM", status: "ok", value: "pm._domainkey.northwind.cloud" },
                    { label: "DMARC", status: "missing", value: "v=DMARC1; p=none; rua=mailto:dmarc@northwind.cloud" },
                  ].map((record) => (
                    <div key={record.label} className="flex items-start gap-3">
                      {record.status === "ok" ? (
                        <CheckCircle2 aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-success-text" />
                      ) : (
                        <AlertTriangle aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-warning-text" />
                      )}
                      <div className="min-w-0 flex-1">
                        <p className="text-sm text-fg">{record.label}</p>
                        <p className="mt-0.5 truncate font-mono text-2xs text-fg-muted">{record.value}</p>
                      </div>
                      <Badge tone={record.status === "ok" ? "success" : "warning"}>
                        {record.status === "ok" ? "Verified" : "Not found"}
                      </Badge>
                    </div>
                  ))}
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
                  <Field label="Suppression list">
                    <p className="text-xs text-fg-muted">
                      2 addresses suppressed. Removing an address here allows Hubchat to retry
                      delivery to it.
                    </p>
                  </Field>
                </CardBody>
              </Card>
            </Section>
          </TabsContent>
        </Tabs>
      </PageBody>
    </Page>
  );
}
