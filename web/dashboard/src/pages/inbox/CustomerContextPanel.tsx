import {
  Avatar,
  Badge,
  Button,
  DetailRow,
  Eyebrow,
  TagChip,
  Tooltip,
  cn,
  formatDateTime,
  formatRelativeShort,
  type Customer,
} from "@hubchat/shared";
import {
  Activity,
  BadgeCheck,
  Building2,
  ChevronRight,
  ExternalLink,
  Eye,
  EyeOff,
  Globe,
  Mail,
  MessageSquare,
  ShieldQuestion,
  StickyNote,
  TicketCheck,
} from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";
import {
  NOW,
  conversations,
  customerEvents,
  fieldDefinitions,
  tickets,
} from "../../data/fixtures";

/**
 * Right-hand context panel (§6.9, §6.10).
 *
 * The thesis of the whole product lives here: an agent should know who they are
 * talking to, what plan they are on, and what just broke in their application —
 * before typing a word. Ordering reflects that: identity, then account, then
 * live session, then the application event stream, then history.
 */
export function CustomerContextPanel({ customerId }: { customerId: string | null }) {
  const { customerById, companyById, memberById, tagById, can } = useWorkspace();
  const customer = customerById(customerId);

  if (!customer) {
    return (
      <div className="flex h-full items-center justify-center p-6 text-center text-xs text-fg-muted">
        No customer is attached to this conversation yet.
      </div>
    );
  }

  const company = companyById(customer.company_ids[0] ?? null);
  const owner = memberById(customer.owner_id);
  const relatedConversations = conversations.filter(
    (item) => item.customer_id === customer.id,
  );
  const relatedTickets = tickets.filter((item) => item.customer_id === customer.id);
  const events = customerEvents.filter((item) => item.customer_id === customer.id);

  return (
    <div className="flex flex-col">
      {/* Identity ---------------------------------------------------------- */}
      <section className="border-b border-line p-4">
        <div className="flex flex-col items-center text-center">
          <Avatar
            name={customer.name}
            seed={customer.id}
            size="xl"
            status={customer.presence}
            pulse={customer.presence === "online"}
          />
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
            <Button
              variant="secondary"
              size="sm"
              fullWidth
              asChild
            >
              <Link to={`/customers/${customer.id}`}>Full profile</Link>
            </Button>
            <Tooltip content="Add a note about this customer">
              <Button variant="secondary" size="sm" iconOnly aria-label="Add note" leading={<StickyNote />} />
            </Tooltip>
          </div>
        </div>
      </section>

      {/* Live session ------------------------------------------------------ */}
      {customer.presence === "online" && customer.current_url && (
        <section className="border-b border-line bg-accent-subtle/40 p-4">
          <Eyebrow className="mb-2 flex items-center gap-1.5 text-accent-text">
            <span className="size-1.5 animate-pulse-ring rounded-full bg-live" />
            On your site now
          </Eyebrow>
          <a
            href={customer.current_url}
            target="_blank"
            rel="noreferrer"
            className="flex items-start gap-1.5 text-xs text-fg-secondary transition-colors hover:text-accent-text"
          >
            <Globe aria-hidden="true" className="mt-0.5 size-3 shrink-0" />
            <span className="min-w-0 break-all">{customer.current_url}</span>
            <ExternalLink aria-hidden="true" className="mt-0.5 size-3 shrink-0" />
          </a>
        </section>
      )}

      {/* Account ----------------------------------------------------------- */}
      {company && (
        <section className="border-b border-line p-4">
          <Eyebrow className="mb-2">Account</Eyebrow>
          <Link
            to={`/companies/${company.id}`}
            className="flex items-center gap-2.5 rounded-md p-1.5 -mx-1.5 transition-colors hover:bg-fill"
          >
            <Avatar name={company.name} seed={company.id} shape="square" size="md" kind="company" />
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm text-fg">{company.name}</span>
              <span className="block truncate text-xs text-fg-muted">{company.domain}</span>
            </span>
            <ChevronRight aria-hidden="true" className="size-3.5 shrink-0 text-fg-disabled" />
          </Link>

          <dl className="mt-1">
            <DetailRow label="Tier">
              <Badge tone={company.tier === "enterprise" ? "accent" : "neutral"}>
                {company.tier}
              </Badge>
            </DetailRow>
            <DetailRow label="Open tickets">{company.open_ticket_count}</DetailRow>
            {owner && <DetailRow label="Account owner">{owner.name}</DetailRow>}
          </dl>
        </section>
      )}

      {/* Custom attributes ------------------------------------------------- */}
      <section className="border-b border-line p-4">
        <Eyebrow className="mb-2">Attributes</Eyebrow>
        <dl>
          {Object.entries(customer.attributes).map(([key, value]) => {
            const definition = fieldDefinitions.find((field) => field.key === key);
            return (
              <AttributeRow
                key={key}
                label={definition?.label ?? key}
                value={value}
                sensitive={definition?.sensitive ?? false}
                canReveal={can("customer.read_sensitive")}
              />
            );
          })}
          <DetailRow label="Language">{customer.language ?? "—"}</DetailRow>
          <DetailRow label="Timezone">{customer.timezone ?? "—"}</DetailRow>
          <DetailRow label="First seen">
            <Tooltip content={formatDateTime(customer.first_seen_at)}>
              <span>{formatRelativeShort(customer.first_seen_at, NOW)} ago</span>
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
      </section>

      {/* Application events ------------------------------------------------ */}
      <section className="border-b border-line p-4">
        <Eyebrow className="mb-2 flex items-center gap-1.5">
          <Activity aria-hidden="true" className="size-3" />
          Recent events
        </Eyebrow>

        {events.length === 0 ? (
          <p className="text-xs text-fg-muted">No events recorded for this customer.</p>
        ) : (
          <ol className="relative space-y-2.5 pl-4">
            <span
              aria-hidden="true"
              className="absolute bottom-1 left-[3px] top-1.5 w-px bg-line"
            />
            {events.map((event) => (
              <li key={event.id} className="relative">
                <span
                  aria-hidden="true"
                  className={cn(
                    "absolute -left-4 top-1 size-[7px] rounded-full ring-2 ring-surface",
                    event.type.includes("fail") ? "bg-danger" : "bg-fg-disabled",
                  )}
                />
                <p className="font-mono text-xs text-fg-secondary">{event.type}</p>
                <p className="mt-0.5 truncate text-2xs text-fg-muted">
                  {Object.entries(event.payload)
                    .map(([key, value]) => `${key}=${String(value)}`)
                    .join(" · ")}
                </p>
                <p className="text-2xs tabular text-fg-disabled">
                  {formatRelativeShort(event.occurred_at, NOW)} ago
                </p>
              </li>
            ))}
          </ol>
        )}
      </section>

      {/* History ----------------------------------------------------------- */}
      <section className="p-4">
        <Eyebrow className="mb-2">History</Eyebrow>

        <HistoryGroup
          icon={<MessageSquare />}
          label="Conversations"
          count={relatedConversations.length}
          items={relatedConversations.slice(0, 3).map((item) => ({
            id: item.id,
            to: `/inbox/all/${item.id}`,
            title: item.subject ?? item.last_message_preview,
            meta: `${item.state.replace(/_/g, " ")} · ${formatRelativeShort(item.last_message_at, NOW)}`,
          }))}
        />

        <HistoryGroup
          icon={<TicketCheck />}
          label="Tickets"
          count={relatedTickets.length}
          items={relatedTickets.slice(0, 3).map((item) => ({
            id: item.id,
            to: `/tickets/${item.id}`,
            title: item.title,
            meta: `${item.prefix}-${item.number} · ${item.status}`,
          }))}
        />
      </section>
    </div>
  );
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

/**
 * §12 — a field marked sensitive is masked in the UI and reveals only on an
 * explicit action, which the server records in the audit log.
 */
function AttributeRow({
  label,
  value,
  sensitive,
  canReveal,
}: {
  label: string;
  value: unknown;
  sensitive: boolean;
  canReveal: boolean;
}) {
  const [revealed, setRevealed] = useState(false);
  const display = Array.isArray(value) ? value.join(", ") : String(value ?? "—");

  if (!sensitive) {
    return (
      <DetailRow label={label}>
        <span className="break-words">{display}</span>
      </DetailRow>
    );
  }

  return (
    <DetailRow label={label}>
      <span className="flex items-center justify-end gap-1.5">
        <span className="font-mono">{revealed ? display : "••••••••"}</span>
        {canReveal && (
          <Tooltip content={revealed ? "Hide" : "Reveal — this is audited"}>
            <button
              type="button"
              onClick={() => setRevealed((current) => !current)}
              aria-label={revealed ? `Hide ${label}` : `Reveal ${label}`}
              className="rounded-xs p-0.5 text-fg-muted transition-colors hover:bg-fill hover:text-fg"
            >
              {revealed ? <EyeOff className="size-3" /> : <Eye className="size-3" />}
            </button>
          </Tooltip>
        )}
      </span>
    </DetailRow>
  );
}

function HistoryGroup({
  icon,
  label,
  count,
  items,
}: {
  icon: React.ReactNode;
  label: string;
  count: number;
  items: { id: string; to: string; title: string; meta: string }[];
}) {
  return (
    <div className="mb-3 last:mb-0">
      <p className="mb-1 flex items-center gap-1.5 text-xs text-fg-secondary [&_svg]:size-3 [&_svg]:text-fg-muted">
        {icon}
        {label}
        <span className="ml-auto tabular text-fg-muted">{count}</span>
      </p>
      <ul className="flex flex-col gap-px">
        {items.map((item) => (
          <li key={item.id}>
            <Link
              to={item.to}
              className="-mx-1.5 block rounded-md p-1.5 transition-colors hover:bg-fill"
            >
              <span className="block truncate text-xs text-fg-secondary">{item.title}</span>
              <span className="block truncate text-2xs capitalize text-fg-muted">{item.meta}</span>
            </Link>
          </li>
        ))}
        {items.length === 0 && <li className="text-2xs text-fg-muted">None yet</li>}
      </ul>
    </div>
  );
}

/** Company glyph re-exported for the panel header in narrow layouts. */
export { Building2 as CompanyIcon };
