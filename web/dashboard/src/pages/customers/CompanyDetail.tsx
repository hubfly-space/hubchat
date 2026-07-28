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
  Dialog,
  DialogContent,
  DetailRow,
  EmptyState,
  Field,
  IdentityCell,
  Input,
  Metric,
  Page,
  PageBody,
  PageHeader,
  QueryBoundary,
  Section,
  Select,
  TagChip,
  TicketStatusBadge,
  useMutation,
  useQuery,
  formatRelativeShort,
  type Company,
  type Customer,
  type Ticket,
} from "@hubchat/shared";
import { Building2, ExternalLink, Pencil, Plus } from "lucide-react";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";

/** Company profile (§6.9) — the account view an escalation starts from. */
export default function CompanyDetail() {
  const { companyId } = useParams();

  const companyQuery = useQuery<Company>(
    companyId ? ["company", companyId] : null,
    (signal) => api.get(`/companies/${companyId}`, { signal }),
  );

  if (companyQuery.error instanceof ApiError && companyQuery.error.status === 404) {
    return (
      <Page>
        <EmptyState icon={Building2} size="lg" title="Company not found" />
      </Page>
    );
  }

  return <QueryBoundary query={companyQuery}>{(company) => <CompanyDetailBody company={company} />}</QueryBoundary>;
}

function CompanyDetailBody({ company }: { company: Company }) {
  const { members, tagById } = useWorkspace();
  const [editing, setEditing] = useState(false);
  const [linking, setLinking] = useState(false);

  const owner = company.owner_id ? members.find((m) => m.id === company.owner_id) : undefined;

  const contacts = useQuery<{ data: Customer[] }>(
    ["companies", company.id, "customers"],
    (signal) => api.get(`/companies/${company.id}/customers`, { signal }),
  );
  const openTickets = useQuery<{ data: Ticket[] }>(
    ["tickets", "by-company", company.id],
    (signal) => api.get(`/tickets?company_id=${company.id}`, { signal }),
  );

  const attributeEntries = Object.entries(company.attributes);

  return (
    <Page>
      <PageHeader
        breadcrumbs={[{ label: "Companies", href: "/companies" }, { label: company.name }]}
        title={company.name}
        description={company.domain ?? undefined}
        meta={<Badge tone={company.tier === "enterprise" ? "accent" : "neutral"}>{company.tier ?? "no tier"}</Badge>}
        actions={
          <>
            {company.domain && (
              <Button variant="secondary" size="sm" trailing={<ExternalLink />} asChild>
                <a href={`https://${company.domain}`} target="_blank" rel="noreferrer">
                  Visit site
                </a>
              </Button>
            )}
            <Button variant="ghost" size="sm" leading={<Pencil />} onClick={() => setEditing(true)}>
              Edit
            </Button>
          </>
        }
      />

      <PageBody>
        <Section>
          <Card>
            <CardBody className="grid gap-6 sm:grid-cols-2">
              <Metric label="Contacts" value={company.customer_count} definition="Customers linked to this account." />
              <Metric
                label="Open tickets"
                value={company.open_ticket_count}
                higherIsBetter={false}
                definition="Tickets in any non-resolved, non-closed state."
              />
            </CardBody>
          </Card>
        </Section>

        <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_300px]">
          <div className="min-w-0 space-y-5">
            <Section
              title="Contacts"
              actions={
                <Button variant="ghost" size="sm" leading={<Plus />} onClick={() => setLinking(true)}>
                  Link a customer
                </Button>
              }
            >
              <Card>
                <CardBody className="p-0">
                  {(contacts.data?.data ?? []).length === 0 ? (
                    <EmptyState size="sm" title="No contacts linked yet" />
                  ) : (
                    <ul className="divide-y divide-line-subtle">
                      {(contacts.data?.data ?? []).map((customer) => (
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
                            />
                            <span className="ml-auto shrink-0 text-2xs text-fg-muted">
                              {customer.last_seen_at ? `seen ${formatRelativeShort(customer.last_seen_at, new Date())} ago` : "never seen"}
                            </span>
                          </Link>
                        </li>
                      ))}
                    </ul>
                  )}
                </CardBody>
              </Card>
            </Section>

            <Section title="Recent tickets">
              <Card>
                <CardBody className="p-0">
                  {(openTickets.data?.data ?? []).length === 0 ? (
                    <EmptyState size="sm" title="No tickets from this account" />
                  ) : (
                    <ul className="divide-y divide-line-subtle">
                      {(openTickets.data?.data ?? []).map((ticket) => (
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
                  )}
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
                  <DetailRow label="Owner">{owner?.name ?? "Unassigned"}</DetailRow>
                  {attributeEntries.map(([key, value]) => (
                    <DetailRow key={key} label={key.replace(/_/g, " ")}>
                      {Array.isArray(value) ? value.join(", ") : String(value ?? "—")}
                    </DetailRow>
                  ))}
                </dl>
              </CardBody>
            </Card>
          </aside>
        </div>
      </PageBody>

      {editing && <EditCompanyDialog company={company} onClose={() => setEditing(false)} />}
      {linking && <LinkCustomerDialog company={company} onClose={() => setLinking(false)} />}
    </Page>
  );
}

function EditCompanyDialog({ company, onClose }: { company: Company; onClose: () => void }) {
  const [name, setName] = useState(company.name);
  const [domain, setDomain] = useState(company.domain ?? "");
  const [tier, setTier] = useState(company.tier ?? "");

  const save = useMutation<void, Company>(
    () =>
      api.patch<Company>(`/companies/${company.id}`, {
        name, domain: domain || null, external_id: company.external_id, tier: tier || null, owner_id: company.owner_id,
      }),
    { invalidates: [["company", company.id], ["companies"]], onSuccess: onClose },
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title="Edit company"
        footer={
          <Button variant="primary" size="sm" loading={save.isPending} disabled={!name.trim()} onClick={() => void save.mutate().catch(() => {})}>
            Save changes
          </Button>
        }
      >
        {save.error ? (
          <Callout tone="danger" className="mb-3">
            {save.error instanceof ApiError ? save.error.message : "Could not save this company."}
          </Callout>
        ) : null}
        <div className="flex flex-col gap-3">
          <Field label="Name">
            <Input value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label="Domain">
            <Input value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="acme.com" />
          </Field>
          <Field label="Tier">
            <Select
              value={tier}
              onValueChange={setTier}
              options={[
                { value: "", label: "No tier" },
                { value: "starter", label: "Starter" },
                { value: "growth", label: "Growth" },
                { value: "enterprise", label: "Enterprise" },
              ]}
              aria-label="Tier"
            />
          </Field>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function LinkCustomerDialog({ company, onClose }: { company: Company; onClose: () => void }) {
  const [query, setQuery] = useState("");

  const results = useQuery<{ data: Customer[] }>(
    query.trim().length > 1 ? ["customers", "search", query] : null,
    (signal) => api.get(`/customers?q=${encodeURIComponent(query)}&limit=10`, { signal }),
  );

  const link = useMutation<string, unknown>(
    (customerId) => api.put(`/companies/${company.id}/customers/${customerId}`, {}),
    { invalidates: [["companies", company.id, "customers"], ["company", company.id]], onSuccess: onClose },
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent title="Link a customer">
        <Input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search by name or email…" autoFocus />
        <ul className="mt-2 flex max-h-64 flex-col gap-1 overflow-y-auto">
          {(results.data?.data ?? []).map((c) => (
            <li key={c.id}>
              <button
                type="button"
                onClick={() => void link.mutate(c.id).catch(() => {})}
                className="w-full rounded-md px-2 py-2 text-left text-sm hover:bg-inset"
              >
                <span className="block truncate text-fg">{c.name ?? "Unnamed"}</span>
                <span className="block truncate text-xs text-fg-muted">{c.email ?? "—"}</span>
              </button>
            </li>
          ))}
          {query.trim().length > 1 && (results.data?.data.length ?? 0) === 0 && <EmptyState size="sm" title="No matching customers" />}
        </ul>
      </DialogContent>
    </Dialog>
  );
}
