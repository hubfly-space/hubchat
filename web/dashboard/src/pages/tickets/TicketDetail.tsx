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
  Checkbox,
  Dialog,
  DialogContent,
  DetailRow,
  EmptyState,
  Eyebrow,
  Field,
  Input,
  Menu,
  MenuContent,
  MenuItem,
  MenuLabel,
  MenuTrigger,
  Page,
  PageBody,
  PageHeader,
  PriorityIndicator,
  QueryBoundary,
  Section,
  Select,
  TagChip,
  TicketStatusBadge,
  Textarea,
  Tooltip,
  formatDateTime,
  formatRelativeShort,
  invalidate,
  useInfinite,
  useMutation,
  useQuery,
  type Customer,
  type FieldDefinition,
  type Member,
  type Tag,
  type Ticket,
  type Paginated,
} from "@hubchat/shared";
import {
  ArrowLeft,
  Link2,
  MessageSquare,
  MoreHorizontal,
  Plus,
  TicketCheck,
  UserPlus,
} from "lucide-react";
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";
import { NewTicketDialog } from "./NewTicketDialog";

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

  const ticketQuery = useQuery<Ticket>(
    ticketId ? ["ticket", ticketId] : null,
    (signal) => api.get(`/tickets/${ticketId}`, { signal }),
  );

  if (ticketQuery.error instanceof ApiError && ticketQuery.error.status === 404) {
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

  return (
    <QueryBoundary query={ticketQuery}>
      {(ticket) => <TicketDetailBody ticket={ticket} onBack={() => navigate("/tickets")} />}
    </QueryBoundary>
  );
}

function TicketDetailBody({ ticket, onBack }: { ticket: Ticket; onBack: () => void }) {
  const { members, tagById } = useWorkspace();
  const [linking, setLinking] = useState(false);
  const [creatingChild, setCreatingChild] = useState(false);

  const customer = useQuery<Customer>(
    ticket.customer_id ? ["customer", ticket.customer_id] : null,
    (signal) => api.get(`/customers/${ticket.customer_id}`, { signal }),
  );
  const assignee = ticket.assignee_id ? members.find((m) => m.id === ticket.assignee_id) : undefined;

  const setStatus = useMutation<string, unknown>(
    (status) => api.patch(`/tickets/${ticket.id}/status`, { status }),
    { invalidates: [["tickets"], ["ticket", ticket.id]] },
  );

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
            onClick={onBack}
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
            {ticket.status !== "resolved" && ticket.status !== "closed" && (
              <Button variant="primary" size="sm" loading={setStatus.isPending} onClick={() => void setStatus.mutate("resolved")}>
                Resolve
              </Button>
            )}
            <Menu>
              <MenuTrigger asChild>
                <Button variant="ghost" size="sm" iconOnly aria-label="More" leading={<MoreHorizontal />} />
              </MenuTrigger>
              <MenuContent align="end">
                <MenuItem icon={<Link2 />} onSelect={() => setLinking(true)}>
                  Link a ticket…
                </MenuItem>
                <MenuItem icon={<Plus />} onSelect={() => setCreatingChild(true)}>
                  Create child ticket
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
            <NarrativeCard ticket={ticket} customer={customer.data} />
            <ActivitySection ticketId={ticket.id} />
            {ticket.conversation_id && <NoteComposer conversationId={ticket.conversation_id} />}
            <CustomFieldsSection ticket={ticket} />
          </div>

          {/* Record ------------------------------------------------------ */}
          <aside className="min-w-0 space-y-4">
            <PropertiesCard ticket={ticket} assignee={assignee} tagById={tagById} />
            <RequesterCard customer={customer.data} />
            <TimestampsCard ticket={ticket} />
            <LinksSection ticket={ticket} />
            <ChildrenSection ticketId={ticket.id} />
          </aside>
        </div>
      </PageBody>

      {linking && <LinkTicketDialog ticket={ticket} onClose={() => setLinking(false)} />}
      <NewTicketDialog
        open={creatingChild}
        onOpenChange={setCreatingChild}
        onCreated={() => {
          invalidate(["ticket", ticket.id]);
        }}
      />
    </Page>
  );
}

function NarrativeCard({ ticket, customer }: { ticket: Ticket; customer: Customer | undefined }) {
  const [title, setTitle] = useState(ticket.title);
  const [description, setDescription] = useState(ticket.description);
  const dirty = title !== ticket.title || description !== ticket.description;

  const update = useMutation<void, Ticket>(
    () =>
      api.patch<Ticket>(`/tickets/${ticket.id}`, {
        title, description, type: ticket.type, due_at: ticket.due_at, expected_version: ticket.version,
      }),
    { invalidates: [["tickets"], ["ticket", ticket.id]] },
  );

  return (
    <Card className="mb-5">
      <CardBody>
        <div className="flex items-start gap-3">
          <Avatar name={customer?.name} seed={customer?.id ?? ticket.id} size="md" />
          <div className="min-w-0 flex-1 space-y-2">
            <p className="text-sm text-fg">
              <span className="font-medium">{customer?.name ?? "Unknown customer"}</span>{" "}
              <span className="text-fg-muted">
                opened this {formatRelativeShort(ticket.created_at, new Date())} ago via {ticket.channel}
              </span>
            </p>
            <Input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              aria-label="Title"
              className="font-medium"
            />
            <Textarea
              autoResize
              rows={3}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              aria-label="Description"
            />
            {update.error ? (
              <Callout tone="danger">
                {update.error instanceof ApiError ? update.error.message : "Could not save these changes."}
              </Callout>
            ) : null}
            {dirty && (
              <div className="flex items-center gap-2">
                <Button variant="primary" size="xs" loading={update.isPending} onClick={() => void update.mutate().catch(() => {})}>
                  Save changes
                </Button>
                <Button
                  variant="ghost"
                  size="xs"
                  onClick={() => {
                    setTitle(ticket.title);
                    setDescription(ticket.description);
                  }}
                >
                  Discard
                </Button>
              </div>
            )}
          </div>
        </div>
      </CardBody>
    </Card>
  );
}

function ActivitySection({ ticketId }: { ticketId: string }) {
  const activity = useQuery<{ data: ActivityEntry[] }>(
    ["ticket-activity", ticketId],
    (signal) => api.get(`/tickets/${ticketId}/activity`, { signal }),
  );
  const entries = activity.data?.data ?? [];

  return (
    <Section title="Activity">
      <Card>
        <CardBody className="p-0">
          {entries.length === 0 ? (
            <EmptyState size="sm" title="No activity yet" />
          ) : (
            <ol className="relative px-5 py-4">
              <span aria-hidden="true" className="absolute bottom-6 left-[26px] top-6 w-px bg-line" />
              {entries.map((entry) => (
                <li key={entry.id} className="relative flex gap-3 pb-4 last:pb-0">
                  <span className="z-10 shrink-0 ring-4 ring-surface">
                    <Avatar name={entry.actor_name || "System"} size="xs" />
                  </span>
                  <span className="min-w-0 pt-0.5 text-xs">
                    <span className="text-fg-secondary">
                      <span className="font-medium text-fg">{entry.actor_name || "Automation"}</span>{" "}
                      {describeAction(entry)}
                    </span>
                    <Tooltip content={formatDateTime(entry.occurred_at)}>
                      <span className="ml-1.5 text-fg-muted">{formatRelativeShort(entry.occurred_at, new Date())} ago</span>
                    </Tooltip>
                  </span>
                </li>
              ))}
            </ol>
          )}
        </CardBody>
      </Card>
    </Section>
  );
}

type ActivityEntry = {
  id: string;
  actor_name: string;
  action: string;
  metadata: Record<string, unknown>;
  occurred_at: string;
};

function describeAction(entry: ActivityEntry): string {
  switch (entry.action) {
    case "ticket.created":
      return "created this ticket";
    case "ticket.assigned":
      return entry.metadata.assignee_id ? "changed the assignee" : "unassigned this ticket";
    case "ticket.state_changed":
      return `changed status from ${String(entry.metadata.from)} to ${String(entry.metadata.to)}`;
    case "ticket.linked":
      return "linked this ticket to another";
    case "ticket.updated":
      return "updated this ticket";
    default:
      return entry.action.replace(/^ticket\./, "").replace(/_/g, " ");
  }
}

function NoteComposer({ conversationId }: { conversationId: string }) {
  const [body, setBody] = useState("");
  const { viewer } = useWorkspace();

  const addNote = useMutation<void, unknown>(
    () => api.post(`/conversations/${conversationId}/messages`, { kind: "note", author_name: viewer.name, body }),
    { onSuccess: () => setBody("") },
  );

  return (
    <Section title="Add an internal note">
      <Card>
        <CardBody>
          <Textarea
            autoResize
            rows={3}
            value={body}
            onChange={(e) => setBody(e.target.value)}
            placeholder="Notes here are visible to your team only."
            aria-label="Internal note"
          />
          <div className="mt-2 flex items-center justify-end">
            <Button
              variant="primary"
              size="sm"
              loading={addNote.isPending}
              disabled={!body.trim()}
              onClick={() => void addNote.mutate().catch(() => {})}
            >
              Add note
            </Button>
          </div>
        </CardBody>
      </Card>
    </Section>
  );
}

function CustomFieldsSection({ ticket }: { ticket: Ticket }) {
  const definitions = useQuery<{ data: FieldDefinition[] }>(
    ["field-definitions", "ticket"],
    (signal) => api.get(`/field-definitions?entity_type=ticket`, { signal }),
  );
  const fields = definitions.data?.data ?? [];
  if (fields.length === 0) return null;

  return (
    <Section title="Custom fields" description="Structured data captured beyond the built-in properties.">
      <Card>
        <CardBody className="space-y-3">
          {fields.map((field) => (
            <CustomFieldInput key={field.id} ticket={ticket} field={field} />
          ))}
        </CardBody>
      </Card>
    </Section>
  );
}

function CustomFieldInput({ ticket, field }: { ticket: Ticket; field: FieldDefinition }) {
  const current = ticket.field_values[field.key];
  const [value, setValue] = useState<unknown>(current ?? null);

  const save = useMutation<unknown, unknown>(
    (v) => api.put(`/tickets/${ticket.id}/field-values/${field.key}`, { value: v }),
    { invalidates: [["ticket", ticket.id]] },
  );

  const commit = (v: unknown) => {
    setValue(v);
    void save.mutate(v).catch(() => {});
  };

  return (
    <Field label={field.label}>
      {field.type === "boolean" ? (
        <Checkbox checked={value === true} onCheckedChange={(checked) => commit(checked === true)} />
      ) : field.type === "enum" ? (
        <Select
          size="sm"
          value={typeof value === "string" ? value : ""}
          onValueChange={commit}
          options={(field.options ?? []).map((o) => ({ value: o, label: o }))}
          aria-label={field.label}
        />
      ) : field.type === "integer" || field.type === "decimal" ? (
        <Input
          type="number"
          inputSize="sm"
          defaultValue={typeof value === "number" ? value : ""}
          onBlur={(e) => commit(e.target.value === "" ? null : Number(e.target.value))}
        />
      ) : field.type === "date" ? (
        <Input type="date" inputSize="sm" defaultValue={typeof value === "string" ? value : ""} onBlur={(e) => commit(e.target.value || null)} />
      ) : (
        <Input
          inputSize="sm"
          defaultValue={typeof value === "string" ? value : ""}
          placeholder={field.description ?? undefined}
          onBlur={(e) => commit(e.target.value || null)}
        />
      )}
    </Field>
  );
}

function PropertiesCard({
  ticket,
  assignee,
  tagById,
}: {
  ticket: Ticket;
  assignee: Member | undefined;
  tagById: (id: string) => Tag | undefined;
}) {
  const { members, tags } = useWorkspace();

  const setStatus = useMutation<string, unknown>(
    (status) => api.patch(`/tickets/${ticket.id}/status`, { status }),
    { invalidates: [["tickets"], ["ticket", ticket.id]] },
  );
  const setPriority = useMutation<string, unknown>(
    (priority) => api.patch(`/tickets/${ticket.id}/priority`, { priority }),
    { invalidates: [["tickets"], ["ticket", ticket.id]] },
  );
  const setAssignee = useMutation<string | null, unknown>(
    (assigneeId) => api.patch(`/tickets/${ticket.id}/assignee`, { assignee_id: assigneeId }),
    { invalidates: [["tickets"], ["ticket", ticket.id]] },
  );
  const setDueAt = useMutation<string | null, unknown>(
    (dueAt) => api.patch(`/tickets/${ticket.id}/due`, { due_at: dueAt }),
    { invalidates: [["tickets"], ["ticket", ticket.id]] },
  );
  const addTag = useMutation<string, unknown>(
    (tagId) => api.post(`/tickets/${ticket.id}/tags`, { tag_id: tagId }),
    { invalidates: [["ticket", ticket.id]] },
  );
  const removeTag = useMutation<string, unknown>(
    (tagId) => api.delete(`/tickets/${ticket.id}/tags/${tagId}`),
    { invalidates: [["ticket", ticket.id]] },
  );

  const availableTags = tags.filter((tag) => !ticket.tag_ids.includes(tag.id));

  return (
    <Card>
      <CardHeader title="Properties" />
      <CardBody className="space-y-3">
        <Field label="Status" orientation="vertical">
          <Select
            size="sm"
            value={ticket.status}
            onValueChange={(status) => void setStatus.mutate(status)}
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
                leading={assignee ? <Avatar name={assignee.name} seed={assignee.id} size="2xs" /> : <UserPlus />}
              >
                {assignee?.name ?? "Unassigned"}
              </Button>
            </MenuTrigger>
            <MenuContent className="w-56">
              <MenuLabel>Assign to</MenuLabel>
              {ticket.assignee_id && (
                <MenuItem onSelect={() => void setAssignee.mutate(null)}>Unassign</MenuItem>
              )}
              {members.map((member) => (
                <MenuItem
                  key={member.id}
                  icon={<Avatar name={member.name} seed={member.id} size="2xs" />}
                  onSelect={() => void setAssignee.mutate(member.id)}
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
            onValueChange={(priority) => void setPriority.mutate(priority)}
            options={[
              { value: "urgent", label: "Urgent" },
              { value: "high", label: "High" },
              { value: "normal", label: "Normal" },
              { value: "low", label: "Low" },
            ]}
            aria-label="Priority"
          />
        </Field>

        <Field label="Due date">
          <Input
            type="date"
            inputSize="sm"
            defaultValue={ticket.due_at ? ticket.due_at.slice(0, 10) : ""}
            onBlur={(e) => void setDueAt.mutate(e.target.value ? new Date(e.target.value).toISOString() : null)}
          />
        </Field>

        <div>
          <Eyebrow className="mb-1.5">Tags</Eyebrow>
          <div className="flex flex-wrap gap-1">
            {ticket.tag_ids.map((tagId) => {
              const tag = tagById(tagId);
              return tag ? (
                <TagChip key={tagId} label={tag.name} color={tag.color} onRemove={() => void removeTag.mutate(tagId)} />
              ) : null;
            })}
            <Menu>
              <MenuTrigger asChild>
                <Button variant="ghost" size="xs" leading={<Plus />}>
                  Add
                </Button>
              </MenuTrigger>
              <MenuContent>
                <MenuLabel>Tags</MenuLabel>
                {availableTags.length === 0 ? (
                  <div className="px-2 py-1.5 text-xs text-fg-muted">No more tags</div>
                ) : (
                  availableTags.map((tag) => (
                    <MenuItem key={tag.id} onSelect={() => void addTag.mutate(tag.id)}>
                      <TagChip label={tag.name} color={tag.color} />
                    </MenuItem>
                  ))
                )}
              </MenuContent>
            </Menu>
          </div>
        </div>
      </CardBody>
    </Card>
  );
}

function RequesterCard({ customer }: { customer: Customer | undefined }) {
  if (!customer) {
    return (
      <Card>
        <CardHeader title="Requester" />
        <CardBody>
          <p className="text-xs text-fg-muted">No customer attached to this ticket.</p>
        </CardBody>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader title="Requester" />
      <CardBody>
        <Link
          to={`/customers/${customer.id}`}
          className="-m-1.5 mb-2 flex items-center gap-2.5 rounded-md p-1.5 transition-colors hover:bg-fill"
        >
          <Avatar name={customer.name} seed={customer.id} size="md" />
          <span className="min-w-0">
            <span className="block truncate text-sm text-fg">{customer.name ?? "Unnamed"}</span>
            <span className="block truncate text-xs text-fg-muted">{customer.email}</span>
          </span>
        </Link>
      </CardBody>
    </Card>
  );
}

function TimestampsCard({ ticket }: { ticket: Ticket }) {
  return (
    <Card>
      <CardHeader title="Timestamps" />
      <CardBody>
        <dl>
          <DetailRow label="Created">{formatDateTime(ticket.created_at)}</DetailRow>
          <DetailRow label="Updated">{formatDateTime(ticket.updated_at)}</DetailRow>
          <DetailRow label="Due">{ticket.due_at ? formatDateTime(ticket.due_at) : "No due date"}</DetailRow>
          {ticket.resolved_at && <DetailRow label="Resolved">{formatDateTime(ticket.resolved_at)}</DetailRow>}
          {ticket.closed_at && <DetailRow label="Closed">{formatDateTime(ticket.closed_at)}</DetailRow>}
        </dl>
      </CardBody>
    </Card>
  );
}

function LinksSection({ ticket }: { ticket: Ticket }) {
  const links = useInfinite<{ id: string; source_id: string; target_id: string; relation: string }>(["ticket-links", ticket.id], (cursor, signal) => api.get<Paginated<{ id: string; source_id: string; target_id: string; relation: string }>>(`/tickets/${ticket.id}/links?limit=25${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }));
  const unlink = useMutation<{ targetId: string; relation: string }, unknown>(
    ({ targetId, relation }) => api.delete(`/tickets/${ticket.id}/links/${targetId}?relation=${relation}`),
    { invalidates: [["ticket-links", ticket.id]] },
  );
  const entries = links.items;
  if (entries.length === 0) return null;

  return (
    <Card>
      <CardHeader title="Linked tickets" />
      <CardBody className="space-y-1">
        {entries.map((link) => {
          const otherId = link.source_id === ticket.id ? link.target_id : link.source_id;
          return (
            <div key={link.id} className="flex items-center justify-between gap-2 text-xs">
              <Link to={`/tickets/${otherId}`} className="truncate text-accent-text hover:underline">
                {otherId}
              </Link>
              <div className="flex items-center gap-1.5 text-fg-muted">
                <Badge tone="neutral">{link.relation.replace(/_/g, " ")}</Badge>
                <button
                  type="button"
                  className="text-fg-muted hover:text-danger-text"
                  onClick={() => void unlink.mutate({ targetId: otherId, relation: link.relation })}
                >
                  Remove
                </button>
                </div>
              </div>
            );
        })}
        {links.hasMore && <Button variant="ghost" size="sm" loading={links.isFetching} onClick={() => void links.fetchNext()}>Load more links</Button>}
      </CardBody>
    </Card>
  );
}

function ChildrenSection({ ticketId }: { ticketId: string }) {
  const children = useInfinite<string>(["ticket-children", ticketId], (cursor, signal) => api.get<Paginated<string>>(`/tickets/${ticketId}/children?limit=25${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }));
  const ids = children.items;
  if (ids.length === 0) return null;

  return (
    <Card>
      <CardHeader title="Child tickets" />
      <CardBody className="space-y-1">
        {ids.map((id) => (
          <Link key={id} to={`/tickets/${id}`} className="block truncate text-xs text-accent-text hover:underline">
            {id}
          </Link>
        ))}
        {children.hasMore && <Button variant="ghost" size="sm" loading={children.isFetching} onClick={() => void children.fetchNext()}>Load more child tickets</Button>}
      </CardBody>
    </Card>
  );
}

function LinkTicketDialog({ ticket, onClose }: { ticket: Ticket; onClose: () => void }) {
  const [query, setQuery] = useState("");
  const [relation, setRelation] = useState("related");

  const results = useInfinite<Ticket>(
    query.trim().length > 1 ? ["tickets", "link-search", query] : null,
    (cursor, signal) => api.get<Paginated<Ticket>>(`/tickets?status=new,open,pending,on_hold&q=${encodeURIComponent(query.trim())}&limit=25${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }),
    { enabled: query.trim().length > 1 },
  );
  const candidates = results.items.filter((t) => t.id !== ticket.id);

  const link = useMutation<string, unknown>(
    (targetId) => api.post(`/tickets/${ticket.id}/links`, { target_id: targetId, relation }),
    { invalidates: [["ticket-links", ticket.id]], onSuccess: onClose },
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent title="Link a ticket">
        <div className="mb-3 flex items-center gap-2">
          <Select
            size="sm"
            value={relation}
            onValueChange={setRelation}
            options={[
              { value: "related", label: "Related to" },
              { value: "duplicate_of", label: "Duplicate of" },
              { value: "blocks", label: "Blocks" },
              { value: "blocked_by", label: "Blocked by" },
            ]}
            aria-label="Relation"
          />
        </div>
        <Input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search tickets by title…" autoFocus />
        <ul className="mt-2 flex max-h-64 flex-col gap-1 overflow-y-auto">
          {candidates.map((candidate) => (
            <li key={candidate.id}>
              <button
                type="button"
                onClick={() => void link.mutate(candidate.id).catch(() => {})}
                className="w-full rounded-md px-2 py-2 text-left text-sm hover:bg-inset"
              >
                <span className="block truncate text-fg">
                  {candidate.prefix}-{candidate.number} · {candidate.title}
                </span>
              </button>
            </li>
          ))}
          {query.trim().length > 1 && candidates.length === 0 && (
            <EmptyState size="sm" title="No matching tickets" />
          )}
        </ul>
        {query.trim().length > 1 && results.hasMore && <Button variant="ghost" size="sm" loading={results.isFetching} onClick={() => void results.fetchNext()}>Load more tickets</Button>}
      </DialogContent>
    </Dialog>
  );
}
