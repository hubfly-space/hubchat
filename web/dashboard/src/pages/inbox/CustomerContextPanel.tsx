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
  type VisitorContext,
  type Ticket,
  type ApiCustomer360,
} from "@hubchat/shared";
import { BadgeCheck, Mail, MessageSquare, ShieldQuestion, StickyNote, Ticket as TicketIcon } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { useWorkspace, workspaceFormatOptions } from "../../app/workspace-context";

/**
 * Right-hand context panel (§6.9).
 *
 * Customer details, conversation history, and ticket history are all loaded
 * from workspace-scoped APIs. The larger customer 360 view remains available
 * from the full profile link so this compact panel stays useful in a narrow
 * inbox column.
 */
export function CustomerContextPanel({ customerId, visitorId }: { customerId: string | null; visitorId: string | null }) {
  const visitor = useQuery<VisitorContext>(
    !customerId && visitorId ? ["visitor-context", visitorId] : null,
    (signal) => api.get(`/visitors/${visitorId}/context`, { signal }),
    { refetchInterval: 15_000 },
  );
  const customer = useQuery<Customer>(
    customerId ? ["customer", customerId] : null,
    (signal) => api.get(`/customers/${customerId}`, { signal }),
    { refetchInterval: 15_000 },
  );

  if (!customerId && !visitorId) {
    return (
      <div className="flex h-full items-center justify-center p-6 text-center text-xs text-fg-muted">
        No customer is attached to this conversation yet.
      </div>
    );
  }

  if (!customerId) {
    return (
      <QueryBoundary query={visitor}>
        {(data) => <AnonymousVisitorContext context={data} />}
      </QueryBoundary>
    );
  }

  return (
    <QueryBoundary query={customer}>
      {(data) => <CustomerContext customer={data} />}
    </QueryBoundary>
  );
}

function AnonymousVisitorContext({ context }: { context: VisitorContext }) {
  const currentPage = context.current_page;
  const device = context.device;
  return (
    <div className="flex flex-col">
      <section className="border-b border-line p-4">
        <div className="flex flex-col items-center text-center">
          <Avatar name="Anonymous visitor" seed={context.visitor_id} size="xl" />
          <h2 className="mt-2.5 text-md font-semibold tracking-tight text-fg">Anonymous visitor</h2>
          <Badge tone="neutral" leading={<ShieldQuestion />}>Not identified</Badge>
          <p className="mt-2 text-2xs text-fg-muted">Visitor {context.visitor_id}</p>
        </div>
      </section>

      <section className="border-b border-line p-4">
        <Eyebrow className="mb-2">Live context</Eyebrow>
        <dl>
          <DetailRow label="Presence">{context.presence}</DetailRow>
          <DetailRow label="Current page">
            {currentPage?.url ? <a className="break-all text-accent-text hover:underline" href={currentPage.url} target="_blank" rel="noreferrer">{currentPage.url}</a> : "—"}
          </DetailRow>
          <DetailRow label="Device">{[device?.device, device?.browser, device?.os].filter(Boolean).join(" · ") || "—"}</DetailRow>
          <DetailRow label="Language">{device?.language || "—"}</DetailRow>
          <DetailRow label="Timezone">{device?.timezone || "—"}</DetailRow>
          <DetailRow label="Platform">{device?.platform || "—"}</DetailRow>
          <DetailRow label="Viewport">{formatViewport(device?.viewport)}</DetailRow>
          <DetailRow label="Referrer">{device?.referrer_origin || "—"}</DetailRow>
          {device?.user_agent && <DetailRow label="User agent"><span className="break-all text-2xs">{device.user_agent}</span></DetailRow>}
          <DetailRow label="Pages viewed">{context.session?.page_views ?? context.page_journey.length}</DetailRow>
        </dl>
        <ContextMetadata metadata={context.context_metadata} />
      </section>

      <section className="border-b border-line p-4">
        <Eyebrow className="mb-2">Recent pages</Eyebrow>
        {context.page_journey.length ? (
          <ul className="flex flex-col gap-px">
            {context.page_journey.slice(0, 10).map((page) => (
              <li key={page.id} className="rounded-md hover:bg-fill">
                {page.url ? <a href={page.url} target="_blank" rel="noreferrer" className="block px-1.5 py-1.5">
                  <span className="block truncate text-xs text-accent-text">{page.title || page.url}</span>
                  <span className="block truncate text-2xs text-fg-muted">{page.url} · {formatRelativeShort(page.occurred_at, new Date())} ago</span>
                </a> : <div className="px-1.5 py-1.5">
                  <span className="block truncate text-xs text-fg-secondary">{page.title || "Page visit"}</span>
                  <span className="block truncate text-2xs text-fg-muted">Unknown URL · {formatRelativeShort(page.occurred_at, new Date())} ago</span>
                </div>}
              </li>
            ))}
          </ul>
        ) : <p className="text-2xs text-fg-muted">No page visits recorded yet.</p>}
      </section>

      <section className="p-4">
        <Eyebrow className="mb-2">Session</Eyebrow>
        <dl>
          <DetailRow label="First seen">{formatRelativeShort(context.first_seen_at, new Date())} ago</DetailRow>
          <DetailRow label="Last seen">{formatRelativeShort(context.last_seen_at, new Date())} ago</DetailRow>
          <DetailRow label="Session ID">{context.session?.id ?? "—"}</DetailRow>
        </dl>
      </section>
    </div>
  );
}

function CustomerContext({ customer }: { customer: Customer }) {
  const { tagById, workspace } = useWorkspace();
  const dateFormat = workspaceFormatOptions(workspace);

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
  const context = useQuery<ApiCustomer360>(
    ["customer", "360", customer.id],
    (signal) => api.get(`/customers/${customer.id}/360`, { signal }),
    { refetchInterval: 15_000 },
  );
  const currentPage = context.data?.current_page;
  const currentSession = context.data?.sessions[0];

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

      <section className="border-b border-line p-4">
        <Eyebrow className="mb-2">At a glance</Eyebrow>
        <div className="grid grid-cols-2 gap-2">
          <SummaryCell label="Presence" value={customer.presence || "Unknown"} />
          <SummaryCell label="Identity" value={customer.verification === "verified" ? "Verified" : "Unverified"} />
          <SummaryCell label="Open tickets" value={tickets.isLoading ? "…" : String((tickets.data?.data ?? []).filter((item) => !["resolved", "closed"].includes(item.status)).length)} />
          <SummaryCell label="Recent conversations" value={history.isLoading ? "…" : String(history.data?.data.length ?? 0)} />
        </div>
        {currentPage?.title ? <p className="mt-3 truncate text-xs text-fg-secondary" title={currentPage.title}>Currently viewing: {currentPage.title}</p> : null}
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
          <DetailRow label="Phone">{customer.phone || "—"}</DetailRow>
          <DetailRow label="External ID">{customer.external_id || "—"}</DetailRow>
          <DetailRow label="Language">{customer.language ?? "—"}</DetailRow>
          <DetailRow label="Timezone">{customer.timezone ?? "—"}</DetailRow>
          <DetailRow label="First seen">
            <Tooltip content={formatDateTime(customer.first_seen_at, dateFormat)}>
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

      <section className="border-b border-line p-4">
        <Eyebrow className="mb-2">Live context</Eyebrow>
        <dl>
          <DetailRow label="Current page">
            {currentPage?.url || customer.current_url ? <a className="break-all text-accent-text hover:underline" href={currentPage?.url || customer.current_url || "#"} target="_blank" rel="noreferrer">{currentPage?.url || customer.current_url}</a> : "—"}
          </DetailRow>
          {currentPage?.title && <DetailRow label="Page title"><span className="break-words">{currentPage.title}</span></DetailRow>}
          {!currentPage?.title && currentSession?.current_title && <DetailRow label="Page title"><span className="break-words">{currentSession.current_title}</span></DetailRow>}
          <DetailRow label="Presence">{customer.presence}</DetailRow>
          <DetailRow label="Device">
            {[context.data?.device?.device, context.data?.device?.browser, context.data?.device?.os].filter(Boolean).join(" · ") || "—"}
          </DetailRow>
          <DetailRow label="Language">{context.data?.device?.language || customer.language || "—"}</DetailRow>
          <DetailRow label="Timezone">{context.data?.device?.timezone || customer.timezone || "—"}</DetailRow>
          <DetailRow label="Platform">{context.data?.device?.platform || "—"}</DetailRow>
          <DetailRow label="Viewport">{formatViewport(context.data?.device?.viewport)}</DetailRow>
          <DetailRow label="Referrer">{context.data?.device?.referrer_origin || "—"}</DetailRow>
          {context.data?.device?.user_agent && <DetailRow label="User agent"><span className="break-all text-2xs">{context.data.device.user_agent}</span></DetailRow>}
          <DetailRow label="Session pages">{currentSession?.page_views ?? context.data?.page_journey.length ?? 0}</DetailRow>
          <DetailRow label="Last seen">{customer.last_seen_at ? formatRelativeShort(customer.last_seen_at, new Date()) + " ago" : "—"}</DetailRow>
        </dl>
        <ContextMetadata metadata={context.data?.context_metadata} />
        <Eyebrow className="mb-2 mt-4">Recent pages</Eyebrow>
        {context.isLoading ? <p className="text-2xs text-fg-muted">Loading page history…</p> : context.error ? <p className="text-2xs text-danger">Could not load page history.</p> : context.data?.page_journey.length ? (
          <ul className="flex flex-col gap-px">
            {context.data.page_journey.slice(0, 10).map((page) => <li key={page.id} className="rounded-md hover:bg-fill">{page.url ? <a href={page.url} target="_blank" rel="noreferrer" className="block px-1.5 py-1.5"><span className="block truncate text-xs text-accent-text">{page.title || page.url}</span><span className="block truncate text-2xs text-fg-muted">{page.url} · {formatRelativeShort(page.occurred_at, new Date())} ago</span></a> : <div className="px-1.5 py-1.5"><span className="block truncate text-xs text-fg-secondary">{page.title || "Page visit"}</span><span className="block truncate text-2xs text-fg-muted">Unknown URL · {formatRelativeShort(page.occurred_at, new Date())} ago</span></div>}</li>)}
          </ul>
        ) : <p className="text-2xs text-fg-muted">No page visits recorded yet.</p>}
      </section>

      {context.data?.companies.length ? <section className="border-b border-line p-4">
        <Eyebrow className="mb-2">Companies</Eyebrow>
        <ul className="space-y-1.5">
          {context.data.companies.map((company) => <li key={company.id} className="flex items-center justify-between gap-3 text-xs"><span className="truncate text-fg-secondary">{company.name}</span><span className="shrink-0 text-2xs text-fg-muted">{company.tier || company.domain || "—"}</span></li>)}
        </ul>
      </section> : null}

      {context.data?.events.length ? <section className="border-b border-line p-4">
        <Eyebrow className="mb-2">Recent activity</Eyebrow>
        <ul className="space-y-1.5">
          {context.data.events.slice(0, 8).map((event) => <li key={event.id} className="flex items-baseline justify-between gap-3"><span className="truncate text-xs text-fg-secondary">{humanEventType(event.type)}</span><span className="shrink-0 text-2xs text-fg-muted">{formatRelativeShort(event.occurred_at, new Date())} ago</span></li>)}
        </ul>
      </section> : null}

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

function humanEventType(value: string): string {
  return value
    .replace(/[._-]+/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function SummaryCell({ label, value }: { label: string; value: string }) {
  return <div className="rounded-md border border-line-subtle bg-inset px-2.5 py-2"><p className="text-2xs text-fg-muted">{label}</p><p className="mt-0.5 truncate text-xs font-medium capitalize text-fg">{value}</p></div>;
}

function formatViewport(viewport: { width?: number; height?: number; device_pixel_ratio?: number } | null | undefined): string {
  if (!viewport?.width || !viewport.height) return "—";
  return `${viewport.width} × ${viewport.height}${viewport.device_pixel_ratio ? ` · ${viewport.device_pixel_ratio}× DPR` : ""}`;
}

function ContextMetadata({ metadata }: { metadata?: Record<string, unknown> }) {
  const entries = Object.entries(metadata ?? {});
  if (entries.length === 0) return null;
  return (
    <div className="mt-4">
      <Eyebrow className="mb-2">Shared context</Eyebrow>
      <dl>
        {entries.map(([key, value]) => (
          <DetailRow key={key} label={titleCase(key)}>
            <span className="break-words">{formatContextValue(value)}</span>
          </DetailRow>
        ))}
      </dl>
    </div>
  );
}

function formatContextValue(value: unknown): string {
  if (typeof value === "string") return value;
  if (value === null || value === undefined) return "—";
  if (typeof value === "object") {
    try {
      return JSON.stringify(value);
    } catch {
      return "[unavailable]";
    }
  }
  return String(value);
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
