import {
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  CodeBlock,
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
} from "@hubchat/shared";
import { AlertTriangle, CheckCircle2, Send } from "lucide-react";
import { useState } from "react";
import { inboxes } from "../../data/fixtures";

/** Email channel (§6.15). */
export default function EmailChannel() {
  const [tab, setTab] = useState("outbound");
  const [inboundMode, setInboundMode] = useState("webhook");

  return (
    <Page>
      <PageHeader
        title="Email channel"
        description="Outbound notifications and inbound reply handling. Processed inside the Hubchat binary — no external worker."
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
                    onValueChange={setInboundMode}
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
                      defaultValue={inboxes[0]!.id}
                      aria-label="Default inbox"
                      options={inboxes.map((inbox) => ({ value: inbox.id, label: inbox.name }))}
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
