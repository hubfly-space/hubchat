import {
  Avatar,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  CodeBlock,
  ConversationStateBadge,
  DetailRow,
  EmptyState,
  Menu,
  MenuContent,
  MenuItem,
  MenuSeparator,
  MenuTrigger,
  Page,
  PageBody,
  PageHeader,
  Section,
  TagChip,
  Tabs,
  TabsContent,
  TabsList,
  TicketStatusBadge,
  Tooltip,
  formatDateTime,
  formatRelativeShort,
} from "@hubchat/shared";
import {
  Combine,
  Download,
  MessageSquare,
  MoreHorizontal,
  ShieldAlert,
  Trash2,
  UserRound,
} from "lucide-react";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";
import { NOW, conversations, customerEvents, fieldDefinitions, tickets } from "../../data/fixtures";

/** Customer profile (§6.9) — the full record behind the inbox context panel. */
export default function CustomerDetail() {
  const { customerId } = useParams();
  const { customerById, companyById, memberById, tagById } = useWorkspace();
  const [tab, setTab] = useState("activity");

  const customer = customerById(customerId ?? null);

  if (!customer) {
    return (
      <Page>
        <EmptyState icon={UserRound} size="lg" title="Customer not found" />
      </Page>
    );
  }

  const company = companyById(customer.company_ids[0] ?? null);
  const owner = memberById(customer.owner_id);
  const theirConversations = conversations.filter((item) => item.customer_id === customer.id);
  const theirTickets = tickets.filter((item) => item.customer_id === customer.id);
  const events = customerEvents.filter((item) => item.customer_id === customer.id);

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
          <>
            <Button variant="secondary" size="sm" leading={<MessageSquare />}>
              Start conversation
            </Button>
            <Menu>
              <MenuTrigger asChild>
                <Button variant="ghost" size="sm" iconOnly aria-label="More" leading={<MoreHorizontal />} />
              </MenuTrigger>
              <MenuContent align="end" className="w-60">
                <MenuItem icon={<Combine />} description="Preview before applying — merges are audited and reversible for 30 days.">
                  Merge with another customer…
                </MenuItem>
                <MenuItem icon={<Download />}>Export this customer's data</MenuItem>
                <MenuSeparator />
                <MenuItem icon={<ShieldAlert />}>Block from contacting</MenuItem>
                <MenuItem icon={<Trash2 />} destructive>
                  Delete and anonymise
                </MenuItem>
              </MenuContent>
            </Menu>
          </>
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
                <Avatar
                  name={customer.name}
                  seed={customer.id}
                  size="xl"
                  status={customer.presence}
                  pulse={customer.presence === "online"}
                />
                <p className="mt-2.5 text-md font-semibold text-fg">
                  {customer.name ?? "Anonymous visitor"}
                </p>
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
                      <span>{formatRelativeShort(customer.first_seen_at, NOW)} ago</span>
                    </Tooltip>
                  </DetailRow>
                  <DetailRow label="Last contacted">
                    {customer.last_contacted_at
                      ? `${formatRelativeShort(customer.last_contacted_at, NOW)} ago`
                      : "never"}
                  </DetailRow>
                  {owner && <DetailRow label="Account owner">{owner.name}</DetailRow>}
                </dl>
              </CardBody>
            </Card>

            {company && (
              <Card>
                <CardHeader title="Company" />
                <CardBody>
                  <Link
                    to={`/companies/${company.id}`}
                    className="-m-1.5 flex items-center gap-2.5 rounded-md p-1.5 transition-colors hover:bg-fill"
                  >
                    <Avatar name={company.name} seed={company.id} shape="square" size="md" kind="company" />
                    <span className="min-w-0">
                      <span className="block truncate text-sm text-fg">{company.name}</span>
                      <span className="block truncate text-xs text-fg-muted">{company.domain}</span>
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
                              <p className="mt-1 font-mono text-2xs text-fg-muted">
                                {JSON.stringify(event.payload)}
                              </p>
                            </div>
                            <span className="shrink-0 text-2xs tabular text-fg-muted">
                              {formatRelativeShort(event.occurred_at, NOW)} ago
                            </span>
                          </li>
                        ))}
                      </ol>
                    </CardBody>
                  </Card>
                </Section>
              </TabsContent>

              <TabsContent value="conversations">
                <Card>
                  <CardBody className="p-0">
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
                                {conversation.message_count} messages ·{" "}
                                {formatRelativeShort(conversation.last_message_at, NOW)} ago
                              </span>
                            </span>
                            <ConversationStateBadge state={conversation.state} />
                          </Link>
                        </li>
                      ))}
                    </ul>
                  </CardBody>
                </Card>
              </TabsContent>

              <TabsContent value="tickets">
                <Card>
                  <CardBody className="p-0">
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
                            <span className="min-w-0 flex-1 truncate text-sm text-fg">
                              {ticket.title}
                            </span>
                            <TicketStatusBadge status={ticket.status} />
                          </Link>
                        </li>
                      ))}
                    </ul>
                  </CardBody>
                </Card>
              </TabsContent>

              <TabsContent value="attributes">
                <Callout tone="info" className="mb-4">
                  Attributes arrive from the JavaScript SDK, the REST API, or a signed identity
                  token. Which sources are accepted for each key is configured in Developers →
                  Metadata schema.
                </Callout>

                <Card>
                  <CardBody className="p-0">
                    <ul className="divide-y divide-line-subtle">
                      {Object.entries(customer.attributes).map(([key, value]) => {
                        const definition = fieldDefinitions.find((field) => field.key === key);
                        return (
                          <li key={key} className="flex items-center gap-3 px-4 py-2.5">
                            <span className="w-40 shrink-0">
                              <span className="block font-mono text-xs text-fg-secondary">{key}</span>
                              {definition && (
                                <span className="block text-2xs text-fg-muted">{definition.label}</span>
                              )}
                            </span>
                            <span className="min-w-0 flex-1 truncate text-xs text-fg">
                              {Array.isArray(value) ? value.join(", ") : String(value)}
                            </span>
                            {definition?.sensitive && <Badge tone="warning">Sensitive</Badge>}
                            <Badge tone="neutral">{definition?.type ?? "string"}</Badge>
                          </li>
                        );
                      })}
                    </ul>
                  </CardBody>
                </Card>

                <Section title="Identify this customer" className="mt-6">
                  <p className="mb-2 text-xs text-fg-muted">
                    Generate the signed token server-side. Never expose your secret in browser code.
                  </p>
                  <CodeBlock
                    language="javascript"
                    code={`Hubchat('identify', {
  external_id: ${JSON.stringify(customer.external_id ?? "u_00000")},
  email: ${JSON.stringify(customer.email ?? "customer@example.com")},
  token: identityToken, // HMAC-signed by your server
  attributes: {
    plan: 'enterprise',
    seats: 240,
  },
});`}
                  />
                </Section>
              </TabsContent>

              <TabsContent value="sessions">
                <Card>
                  <CardBody className="p-0">
                    <ul className="divide-y divide-line-subtle">
                      <li className="flex items-center gap-3 px-4 py-3">
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-sm text-fg">
                            {customer.current_url ?? "No active session"}
                          </span>
                          <span className="block text-xs text-fg-muted">
                            Chrome 141 · macOS · Lisbon, PT
                          </span>
                        </span>
                        <Badge tone={customer.presence === "online" ? "success" : "neutral"}>
                          {customer.presence === "online" ? "Active now" : "Ended"}
                        </Badge>
                      </li>
                    </ul>
                  </CardBody>
                </Card>
              </TabsContent>
            </Tabs>
          </div>
        </div>
      </PageBody>
    </Page>
  );
}
