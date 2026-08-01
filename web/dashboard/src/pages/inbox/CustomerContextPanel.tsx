import {
  api,
  Avatar,
  Badge,
  Button,
  DetailRow,
  Eyebrow,
  QueryBoundary,
  TagChip,
  Tooltip,
  formatDateTime,
  formatRelativeShort,
  useMutation,
  useQuery,
  type Conversation,
  type Customer,
  type Ticket,
} from "@hubchat/shared";
import { BadgeCheck, Mail, MessageSquare, ShieldQuestion, StickyNote, Ticket as TicketIcon } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";

/**
 * Right-hand context panel (§6.9).
 *
 * Customer details, conversation history, and ticket history are all loaded
 * from workspace-scoped APIs. The larger customer 360 view remains available
 * from the full profile link so this compact panel stays useful in a narrow
 * inbox column.
 */
export function CustomerContextPanel({ customerId }: { customerId: string | null }) {
  const customer = useQuery<Customer>(
    customerId ? ["customer", customerId] : null,
    (signal) => api.get(`/customers/${customerId}`, { signal }),
  );

  if (!customerId) {
    return (
      <div className="flex h-full items-center justify-center p-6 text-center text-xs text-fg-muted">
        No customer is attached to this conversation yet.
      </div>
    );
  }

  return (
    <QueryBoundary query={customer}>
      {(data) => <CustomerContext customer={data} />}
    </QueryBoundary>
  );
}

function CustomerContext({ customer }: { customer: Customer }) {
  const { tagById } = useWorkspace();

  const history = useQuery<{ data: Conversation[] }>(
    ["conversations", "customer-history", customer.id],
    (signal) =>
      api.get(
        `/conversations?customer_id=${encodeURIComponent(customer.id)}&limit=5&state=new,open,pending,waiting_for_customer,waiting_for_support,resolved,closed`,
        { signal },
      ),
  );
  const tickets = useQuery<{ data: Ticket[] }>(
    ["tickets", "customer-history", customer.id],
    (signal) => api.get(`/tickets?customer_id=${encodeURIComponent(customer.id)}&limit=5`, { signal }),
  );

  return (
    <div className="flex flex-col">
      {/* Identity ---------------------------------------------------------- */}
      <section className="border-b border-line p-4">
        <div className="flex flex-col items-center text-center">
          <Avatar name={customer.name} seed={customer.id} size="xl" />
          <h2 className="mt-2.5 text-md font-semibold tracking-tight text-fg">
            {customer.name ?? "Anonymous visitor"}
          </h2>

          <VerificationBadge verification={customer.verification} />

          {customer.email && (
            <a
              href={`mailto:${customer.email}`}
              className="mt-1.5 flex items-center gap-1.5 text-xs text-fg-muted transition-colors hover:text-accent-text"
            >
              <Mail aria-hidden="true" className="size-3" />
              {customer.email}
            </a>
          )}

          <div className="mt-3 flex w-full gap-1.5">
            <Button variant="secondary" size="sm" fullWidth asChild>
              <Link to={`/customers/${customer.id}`}>Full profile</Link>
            </Button>
          </div>
        </div>
      </section>

      {/* Attributes ---------------------------------------------------------- */}
      <section className="border-b border-line p-4">
        <Eyebrow className="mb-2">Details</Eyebrow>
        <dl>
          {Object.entries(customer.attributes).map(([key, value]) => (
            <DetailRow key={key} label={titleCase(key)}>
              <span className="break-words">
                {Array.isArray(value) ? value.join(", ") : String(value ?? "—")}
              </span>
            </DetailRow>
          ))}
          <DetailRow label="Language">{customer.language ?? "—"}</DetailRow>
          <DetailRow label="Timezone">{customer.timezone ?? "—"}</DetailRow>
          <DetailRow label="First seen">
            <Tooltip content={formatDateTime(customer.first_seen_at)}>
              <span>{formatRelativeShort(customer.first_seen_at, new Date())} ago</span>
            </Tooltip>
          </DetailRow>
        </dl>

        {customer.tag_ids.length > 0 && (
          <div className="mt-2 flex flex-wrap gap-1">
            {customer.tag_ids.map((tagId) => {
              const tag = tagById(tagId);
              return tag ? <TagChip key={tagId} label={tag.name} color={tag.color} /> : null;
            })}
          </div>
        )}

        <AddTag customer={customer} />
      </section>

      {/* History ----------------------------------------------------------- */}
      <section className="p-4">
        <Eyebrow className="mb-2">History</Eyebrow>

        <p className="mb-1 flex items-center gap-1.5 text-xs text-fg-secondary [&_svg]:size-3 [&_svg]:text-fg-muted">
          <TicketIcon />
          Tickets
          <span className="ml-auto tabular text-fg-muted">{tickets.data?.data.length ?? 0}</span>
        </p>
        {tickets.isLoading ? (
          <p className="mb-3 text-2xs text-fg-muted">Loading tickets…</p>
        ) : tickets.error ? (
          <p className="mb-3 text-2xs text-danger">Could not load ticket history.</p>
        ) : tickets.data?.data.length ? (
          <ul className="mb-3 flex flex-col gap-px">
            {tickets.data.data.map((item) => (
              <li key={item.id}>
                <Link
                  to={`/tickets/${item.id}`}
                  className="-mx-1.5 block rounded-md p-1.5 transition-colors hover:bg-fill"
                >
                  <span className="block truncate text-xs text-fg-secondary">
                    {item.prefix}-{item.number} · {item.title}
                  </span>
                  <span className="block truncate text-2xs capitalize text-fg-muted">
                    {item.status.replace(/_/g, " ")} · {formatRelativeShort(item.updated_at, new Date())}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        ) : (
          <p className="mb-3 text-2xs text-fg-muted">No tickets yet</p>
        )}

        <p className="mb-1 flex items-center gap-1.5 text-xs text-fg-secondary [&_svg]:size-3 [&_svg]:text-fg-muted">
          <MessageSquare />
          Conversations
          <span className="ml-auto tabular text-fg-muted">{history.data?.data.length ?? 0}</span>
        </p>
        <ul className="flex flex-col gap-px">
          {(history.data?.data ?? []).map((item) => (
            <li key={item.id}>
              <Link
                to={`/inbox/all/${item.id}`}
                className="-mx-1.5 block rounded-md p-1.5 transition-colors hover:bg-fill"
              >
                <span className="block truncate text-xs text-fg-secondary">
                  {item.subject ?? item.last_message_preview}
                </span>
                <span className="block truncate text-2xs capitalize text-fg-muted">
                  {item.state.replace(/_/g, " ")} · {formatRelativeShort(item.last_message_at, new Date())}
                </span>
              </Link>
            </li>
          ))}
          {(history.data?.data.length ?? 0) === 0 && (
            <li className="text-2xs text-fg-muted">
              {history.isLoading ? "Loading conversations…" : history.error ? "Could not load conversation history." : "None yet"}
            </li>
          )}
        </ul>
      </section>
    </div>
  );
}

function titleCase(key: string): string {
  return key
    .replace(/[_-]/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function VerificationBadge({ verification }: { verification: Customer["verification"] }) {
  if (verification === "verified") {
    return (
      <Tooltip content="Identity verified by a signed token from your application">
        <span className="mt-1.5">
          <Badge tone="success" leading={<BadgeCheck />}>
            Verified
          </Badge>
        </span>
      </Tooltip>
    );
  }

  return (
    <Tooltip
      content={
        verification === "anonymous"
          ? "This visitor has not identified themselves. Do not disclose account details."
          : "Self-reported identity. Not verified by a signed token."
      }
    >
      <span className="mt-1.5">
        <Badge tone={verification === "anonymous" ? "neutral" : "warning"} leading={<ShieldQuestion />}>
          {verification === "anonymous" ? "Anonymous" : "Unverified"}
        </Badge>
      </span>
    </Tooltip>
  );
}

function AddTag({ customer }: { customer: Customer }) {
  const { tags } = useWorkspace();
  const [open, setOpen] = useState(false);

  const addTag = useMutation<string, unknown>(
    (tagId) => api.post(`/customers/${customer.id}/tags`, { tag_id: tagId }),
    { invalidates: [["customer", customer.id]] },
  );

  const available = tags.filter((tag) => !customer.tag_ids.includes(tag.id));

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="mt-2 flex items-center gap-1 text-2xs text-fg-muted transition-colors hover:text-accent-text"
      >
        <StickyNote aria-hidden="true" className="size-3" />
        Add a tag
      </button>
    );
  }

  return (
    <div className="mt-2 flex flex-wrap gap-1">
      {available.length === 0 ? (
        <span className="text-2xs text-fg-muted">No more tags to add</span>
      ) : (
        available.map((tag) => (
          <button
            key={tag.id}
            type="button"
            onClick={() => {
              void addTag.mutate(tag.id).catch(() => {});
              setOpen(false);
            }}
          >
            <TagChip label={tag.name} color={tag.color} />
          </button>
        ))
      )}
    </div>
  );
}
