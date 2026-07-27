import {
  Avatar,
  Badge,
  Button,
  Card,
  CardBody,
  CardHeader,
  DetailRow,
  EmptyState,
  Eyebrow,
  Field,
  Menu,
  MenuContent,
  MenuItem,
  MenuLabel,
  MenuTrigger,
  Page,
  PageBody,
  PageHeader,
  PriorityIndicator,
  Section,
  Select,
  TagChip,
  TicketStatusBadge,
  Textarea,
  Tooltip,
  formatDateTime,
  formatRelativeShort,
} from "@hubchat/shared";
import {
  ArrowLeft,
  Clock,
  Link2,
  MessageSquare,
  MoreHorizontal,
  Paperclip,
  Plus,
  TicketCheck,
  Trash2,
  UserPlus,
} from "lucide-react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";
import { NOW, tickets } from "../../data/fixtures";

/**
 * Ticket detail (§6.3).
 *
 * Two columns: the case narrative on the left, the structured record on the
 * right. The split matters — a ticket is simultaneously a conversation and a
 * row in a database, and cramming both into one column makes each worse.
 */
export default function TicketDetail() {
  const { ticketId } = useParams();
  const navigate = useNavigate();
  const { memberById, customerById, companyById, tagById, members } = useWorkspace();

  const ticket = tickets.find((item) => item.id === ticketId);

  if (!ticket) {
    return (
      <Page>
        <EmptyState
          icon={TicketCheck}
          size="lg"
          title="Ticket not found"
          description="It may have been deleted or merged into another ticket."
          action={
            <Button variant="secondary" size="sm" asChild>
              <Link to="/tickets">Back to tickets</Link>
            </Button>
          }
        />
      </Page>
    );
  }

  const customer = customerById(ticket.customer_id);
  const company = companyById(ticket.company_id);
  const assignee = memberById(ticket.assignee_id);

  return (
    <Page>
      <PageHeader
        breadcrumbs={[{ label: "Tickets", href: "/tickets" }, { label: `${ticket.prefix}-${ticket.number}` }]}
        title={ticket.title}
        back={
          <Button
            variant="ghost"
            size="sm"
            iconOnly
            aria-label="Back to tickets"
            leading={<ArrowLeft />}
            onClick={() => navigate("/tickets")}
            className="mt-0.5"
          />
        }
        meta={
          <>
            <TicketStatusBadge status={ticket.status} />
            <PriorityIndicator priority={ticket.priority} showLabel />
          </>
        }
        actions={
          <>
            {ticket.conversation_id && (
              <Button variant="secondary" size="sm" leading={<MessageSquare />} asChild>
                <Link to={`/inbox/all/${ticket.conversation_id}`}>Open conversation</Link>
              </Button>
            )}
            <Button variant="primary" size="sm">
              Resolve
            </Button>
            <Menu>
              <MenuTrigger asChild>
                <Button variant="ghost" size="sm" iconOnly aria-label="More" leading={<MoreHorizontal />} />
              </MenuTrigger>
              <MenuContent align="end">
                <MenuItem icon={<Link2 />}>Link a ticket…</MenuItem>
                <MenuItem icon={<Plus />}>Create child ticket</MenuItem>
                <MenuItem icon={<Clock />}>Schedule follow-up</MenuItem>
                <MenuItem icon={<Trash2 />} destructive>
                  Delete ticket
                </MenuItem>
              </MenuContent>
            </Menu>
          </>
        }
      />

      <PageBody width="full">
        <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_320px]">
          {/* Narrative --------------------------------------------------- */}
          <div className="min-w-0">
            <Card className="mb-5">
              <CardBody>
                <div className="flex items-start gap-3">
                  <Avatar name={customer?.name} seed={customer?.id ?? ticket.id} size="md" />
                  <div className="min-w-0 flex-1">
                    <p className="text-sm text-fg">
                      <span className="font-medium">{customer?.name ?? "Unknown customer"}</span>{" "}
                      <span className="text-fg-muted">
                        opened this {formatRelativeShort(ticket.created_at, NOW)} ago via{" "}
                        {ticket.channel}
                      </span>
                    </p>
                    <p className="mt-2 whitespace-pre-wrap text-sm leading-normal text-fg-secondary">
                      {ticket.description}
                    </p>
                  </div>
                </div>
              </CardBody>
            </Card>

            <Section title="Activity">
              <Card>
                <CardBody className="p-0">
                  <ol className="relative px-5 py-4">
                    <span aria-hidden="true" className="absolute bottom-6 left-[26px] top-6 w-px bg-line" />
                    {[
                      { id: 1, who: "Routing rule · Enterprise → Core", what: "assigned this to Product Support", when: ticket.created_at, system: true },
                      { id: 2, who: assignee?.name ?? "Rui Ferreira", what: "set priority to " + ticket.priority, when: ticket.created_at },
                      { id: 3, who: customer?.name ?? "Customer", what: "added an attachment", when: ticket.created_at },
                    ].map((entry) => (
                      <li key={entry.id} className="relative flex gap-3 pb-4 last:pb-0">
                        <span
                          className={
                            entry.system
                              ? "z-10 grid size-5 shrink-0 place-items-center rounded-full bg-system-subtle text-system ring-4 ring-surface"
                              : "z-10 shrink-0 ring-4 ring-surface"
                          }
                        >
                          {entry.system ? (
                            <span className="size-1.5 rounded-full bg-current" />
                          ) : (
                            <Avatar name={entry.who} size="xs" />
                          )}
                        </span>
                        <span className="min-w-0 pt-0.5 text-xs">
                          <span className="text-fg-secondary">
                            <span className="font-medium text-fg">{entry.who}</span> {entry.what}
                          </span>
                          <Tooltip content={formatDateTime(entry.when)}>
                            <span className="ml-1.5 text-fg-muted">
                              {formatRelativeShort(entry.when, NOW)} ago
                            </span>
                          </Tooltip>
                        </span>
                      </li>
                    ))}
                  </ol>
                </CardBody>
              </Card>
            </Section>

            <Section title="Add an internal note">
              <Card>
                <CardBody>
                  <Textarea
                    autoResize
                    rows={3}
                    placeholder="Notes here are visible to your team only."
                    aria-label="Internal note"
                  />
                  <div className="mt-2 flex items-center justify-between">
                    <Button variant="ghost" size="sm" leading={<Paperclip />}>
                      Attach
                    </Button>
                    <Button variant="primary" size="sm">
                      Add note
                    </Button>
                  </div>
                </CardBody>
              </Card>
            </Section>
          </div>

          {/* Record ------------------------------------------------------ */}
          <aside className="min-w-0 space-y-4">
            <Card>
              <CardHeader title="Properties" />
              <CardBody className="space-y-3">
                <Field label="Status" orientation="vertical">
                  <Select
                    size="sm"
                    value={ticket.status}
                    options={[
                      { value: "new", label: "New" },
                      { value: "open", label: "Open" },
                      { value: "pending", label: "Pending" },
                      { value: "on_hold", label: "On hold" },
                      { value: "resolved", label: "Resolved" },
                      { value: "closed", label: "Closed" },
                    ]}
                    aria-label="Status"
                  />
                </Field>

                <Field label="Assignee">
                  <Menu>
                    <MenuTrigger asChild>
                      <Button
                        variant="secondary"
                        size="sm"
                        fullWidth
                        className="justify-start"
                        leading={
                          assignee ? (
                            <Avatar name={assignee.name} seed={assignee.id} size="2xs" />
                          ) : (
                            <UserPlus />
                          )
                        }
                      >
                        {assignee?.name ?? "Unassigned"}
                      </Button>
                    </MenuTrigger>
                    <MenuContent className="w-56">
                      <MenuLabel>Assign to</MenuLabel>
                      {members.map((member) => (
                        <MenuItem
                          key={member.id}
                          icon={<Avatar name={member.name} seed={member.id} size="2xs" />}
                        >
                          {member.name}
                        </MenuItem>
                      ))}
                    </MenuContent>
                  </Menu>
                </Field>

                <Field label="Priority">
                  <Select
                    size="sm"
                    value={ticket.priority}
                    options={[
                      { value: "urgent", label: "Urgent" },
                      { value: "high", label: "High" },
                      { value: "normal", label: "Normal" },
                      { value: "low", label: "Low" },
                    ]}
                    aria-label="Priority"
                  />
                </Field>

                <div>
                  <Eyebrow className="mb-1.5">Tags</Eyebrow>
                  <div className="flex flex-wrap gap-1">
                    {ticket.tag_ids.map((tagId) => {
                      const tag = tagById(tagId);
                      return tag ? (
                        <TagChip key={tagId} label={tag.name} color={tag.color} onRemove={() => undefined} />
                      ) : null;
                    })}
                    <Button variant="ghost" size="xs" leading={<Plus />}>
                      Add
                    </Button>
                  </div>
                </div>
              </CardBody>
            </Card>

            <Card>
              <CardHeader title="Requester" />
              <CardBody>
                {customer && (
                  <Link
                    to={`/customers/${customer.id}`}
                    className="-m-1.5 mb-2 flex items-center gap-2.5 rounded-md p-1.5 transition-colors hover:bg-fill"
                  >
                    <Avatar name={customer.name} seed={customer.id} size="md" />
                    <span className="min-w-0">
                      <span className="block truncate text-sm text-fg">{customer.name}</span>
                      <span className="block truncate text-xs text-fg-muted">{customer.email}</span>
                    </span>
                  </Link>
                )}
                <dl>
                  {company && (
                    <DetailRow label="Company">
                      <Link to={`/companies/${company.id}`} className="text-accent-text hover:underline">
                        {company.name}
                      </Link>
                    </DetailRow>
                  )}
                  {company && (
                    <DetailRow label="Tier">
                      <Badge tone={company.tier === "enterprise" ? "accent" : "neutral"}>
                        {company.tier}
                      </Badge>
                    </DetailRow>
                  )}
                </dl>
              </CardBody>
            </Card>

            <Card>
              <CardHeader title="Timestamps" />
              <CardBody>
                <dl>
                  <DetailRow label="Created">{formatDateTime(ticket.created_at)}</DetailRow>
                  <DetailRow label="Updated">{formatDateTime(ticket.updated_at)}</DetailRow>
                  <DetailRow label="Due">
                    {ticket.due_at ? formatDateTime(ticket.due_at) : "No due date"}
                  </DetailRow>
                  {ticket.resolved_at && (
                    <DetailRow label="Resolved">{formatDateTime(ticket.resolved_at)}</DetailRow>
                  )}
                </dl>
              </CardBody>
            </Card>
          </aside>
        </div>
      </PageBody>
    </Page>
  );
}
