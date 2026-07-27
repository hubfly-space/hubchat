import {
  Avatar,
  Badge,
  Button,
  Card,
  CardBody,
  CardHeader,
  DetailRow,
  EmptyState,
  IdentityCell,
  Metric,
  Page,
  PageBody,
  PageHeader,
  Section,
  Sparkline,
  TagChip,
  TicketStatusBadge,
  formatCompact,
  formatRelativeShort,
} from "@hubchat/shared";
import { Building2, ExternalLink, Plus, Timer } from "lucide-react";
import { Link, useParams } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";
import { NOW, analytics, customers, slaPolicies, tickets } from "../../data/fixtures";

/** Company profile (§6.9) — the account view an escalation starts from. */
export default function CompanyDetail() {
  const { companyId } = useParams();
  const { companyById, memberById, tagById } = useWorkspace();

  const company = companyById(companyId ?? null);

  if (!company) {
    return (
      <Page>
        <EmptyState icon={Building2} size="lg" title="Company not found" />
      </Page>
    );
  }

  const owner = memberById(company.owner_id);
  const contacts = customers.filter((customer) => customer.company_ids.includes(company.id));
  const openTickets = tickets.filter((ticket) => ticket.company_id === company.id);
  const policy = slaPolicies.find((item) => item.id === company.sla_policy_id);

  return (
    <Page>
      <PageHeader
        breadcrumbs={[{ label: "Companies", href: "/companies" }, { label: company.name }]}
        title={company.name}
        description={company.domain ?? undefined}
        meta={<Badge tone={company.tier === "enterprise" ? "accent" : "neutral"}>{company.tier}</Badge>}
        actions={
          company.domain ? (
            <Button variant="secondary" size="sm" trailing={<ExternalLink />} asChild>
              <a href={`https://${company.domain}`} target="_blank" rel="noreferrer">
                Visit site
              </a>
            </Button>
          ) : null
        }
      />

      <PageBody>
        <Section>
          <Card>
            <CardBody className="grid gap-6 sm:grid-cols-4">
              <Metric
                label="Monthly revenue"
                value={`$${formatCompact(Number(company.attributes.mrr ?? 0))}`}
                delta={0.04}
                definition="Reported by your billing system through the company attributes API."
              />
              <Metric
                label="Contacts"
                value={company.customer_count}
                definition="Customers linked to this account."
              />
              <Metric
                label="Open tickets"
                value={company.open_ticket_count}
                higherIsBetter={false}
                delta={0.25}
                definition="Tickets in any non-resolved, non-closed state."
              />
              <Metric
                label="Conversations"
                value={formatCompact(148)}
                sparkline={analytics.conversations.slice(-14)}
                definition="Total conversations from this account, all time."
              />
            </CardBody>
          </Card>
        </Section>

        <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_300px]">
          <div className="min-w-0 space-y-5">
            <Section
              title="Contacts"
              actions={
                <Button variant="ghost" size="sm" leading={<Plus />}>
                  Link a customer
                </Button>
              }
            >
              <Card>
                <CardBody className="p-0">
                  <ul className="divide-y divide-line-subtle">
                    {contacts.map((customer) => (
                      <li key={customer.id}>
                        <Link
                          to={`/customers/${customer.id}`}
                          className="flex items-center gap-3 px-4 py-2.5 transition-colors hover:bg-surface-hover"
                        >
                          <IdentityCell
                            name={customer.name ?? "Anonymous"}
                            secondary={customer.email ?? "—"}
                            seed={customer.id}
                            size="sm"
                            status={customer.presence === "online" ? "online" : null}
                          />
                          <span className="ml-auto shrink-0 text-2xs text-fg-muted">
                            {customer.last_seen_at
                              ? `seen ${formatRelativeShort(customer.last_seen_at, NOW)} ago`
                              : "never seen"}
                          </span>
                        </Link>
                      </li>
                    ))}
                  </ul>
                </CardBody>
              </Card>
            </Section>

            <Section title="Recent tickets">
              <Card>
                <CardBody className="p-0">
                  <ul className="divide-y divide-line-subtle">
                    {openTickets.map((ticket) => (
                      <li key={ticket.id}>
                        <Link
                          to={`/tickets/${ticket.id}`}
                          className="flex items-center gap-3 px-4 py-2.5 transition-colors hover:bg-surface-hover"
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
                </CardBody>
              </Card>
            </Section>
          </div>

          <aside className="space-y-4">
            <Card>
              <CardBody className="flex flex-col items-center text-center">
                <Avatar name={company.name} seed={company.id} shape="square" size="xl" kind="company" />
                <p className="mt-2.5 text-md font-semibold text-fg">{company.name}</p>
                <p className="text-xs text-fg-muted">{company.domain}</p>
                {company.tag_ids.length > 0 && (
                  <div className="mt-3 flex flex-wrap justify-center gap-1">
                    {company.tag_ids.map((tagId) => {
                      const tag = tagById(tagId);
                      return tag ? <TagChip key={tagId} label={tag.name} color={tag.color} /> : null;
                    })}
                  </div>
                )}
              </CardBody>
            </Card>

            <Card>
              <CardHeader title="Account" />
              <CardBody>
                <dl>
                  <DetailRow label="External ID">
                    <span className="font-mono">{company.external_id ?? "—"}</span>
                  </DetailRow>
                  <DetailRow label="Seats">{String(company.attributes.seats ?? "—")}</DetailRow>
                  <DetailRow label="Region">
                    <span className="uppercase">{String(company.attributes.region ?? "—")}</span>
                  </DetailRow>
                  <DetailRow label="Owner">{owner?.name ?? "Unassigned"}</DetailRow>
                </dl>
              </CardBody>
            </Card>

            <Card>
              <CardHeader title="Service level" />
              <CardBody>
                {policy ? (
                  <>
                    <p className="flex items-center gap-1.5 text-sm text-fg">
                      <Timer aria-hidden="true" className="size-3.5 text-accent-text" />
                      {policy.name}
                    </p>
                    <dl className="mt-2">
                      <DetailRow label="Urgent first response">
                        {policy.targets[0]?.first_response_minutes} min
                      </DetailRow>
                      <DetailRow label="30-day compliance">
                        <span className="text-success-text">
                          {policy.compliance_30d
                            ? `${(policy.compliance_30d * 100).toFixed(1)}%`
                            : "—"}
                        </span>
                      </DetailRow>
                    </dl>
                    <Sparkline points={analytics.csat.slice(-20)} tone={4} className="mt-3 w-full" />
                  </>
                ) : (
                  <p className="text-xs text-fg-muted">
                    No company-specific policy. The workspace default applies.
                  </p>
                )}
              </CardBody>
            </Card>
          </aside>
        </div>
      </PageBody>
    </Page>
  );
}
