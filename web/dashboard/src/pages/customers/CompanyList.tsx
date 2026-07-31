import {
  api,
  ApiError,
  Badge,
  Button,
  Callout,
  DataTable,
  Dialog,
  DialogContent,
  EmptyState,
  Field,
  IdentityCell,
  Input,
  Page,
  PageHeader,
  Pagination,
  SearchInput,
  Toolbar,
  downloadFile,
  useInfinite,
  useMutation,
  useToast,
  formatCompact,
  type Column,
  type Company,
  type Paginated,
} from "@hubchat/shared";
import { Building2, Download, Plus } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";

/** Company directory (§6.9) — the account layer above individual contacts. */
export default function CompanyList() {
  const navigate = useNavigate();
  const { members, workspace } = useWorkspace();
  const toast = useToast();
  const [query, setQuery] = useState("");
  const [creating, setCreating] = useState(false);

  const list = useInfinite<Company>(["companies", "directory", query], (cursor, signal) => {
    const params = new URLSearchParams();
    if (query.trim()) params.set("q", query.trim());
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<Company>>(`/companies?${params.toString()}`, { signal });
  });

  const memberById = new Map(members.map((m) => [m.id, m]));

  const columns: Column<Company>[] = [
    {
      key: "name",
      header: "Company",
      cell: (company) => (
        <IdentityCell
          name={company.name}
          secondary={company.domain ?? company.external_id ?? "—"}
          seed={company.id}
          kind="company"
          size="sm"
        />
      ),
      sortable: true,
    },
    {
      key: "tier",
      header: "Tier",
      width: "116px",
      cell: (company) => <Badge tone={company.tier === "enterprise" ? "accent" : "neutral"}>{company.tier ?? "—"}</Badge>,
      sortable: true,
    },
    {
      key: "customer_count",
      header: "Contacts",
      width: "92px",
      numeric: true,
      cell: (company) => formatCompact(company.customer_count),
      sortable: true,
    },
    {
      key: "open_ticket_count",
      header: "Open tickets",
      width: "104px",
      numeric: true,
      cell: (company) => (
        <span className={company.open_ticket_count > 3 ? "text-warning-text" : undefined}>{company.open_ticket_count}</span>
      ),
      sortable: true,
    },
    {
      key: "owner",
      header: "Owner",
      width: "150px",
      hideBelow: "md",
      cell: (company) => {
        const owner = company.owner_id ? memberById.get(company.owner_id) : undefined;
        return owner ? (
          <span className="text-xs text-fg-secondary">{owner.name}</span>
        ) : (
          <span className="text-xs text-fg-disabled">Unassigned</span>
        );
      },
    },
  ];

  return (
    <Page>
      <PageHeader
        title="Companies"
        description="Accounts that group contacts, tickets, and service levels."
        actions={
          <>
            <Button
              variant="secondary"
              size="sm"
              leading={<Download />}
              onClick={() => void downloadFile(`/companies/export?q=${encodeURIComponent(query)}`, "companies-export.csv", workspace.id).catch((error) => toast.error({ title: "Could not export companies", description: error instanceof Error ? error.message : "Try again." }))}
            >
              Export
            </Button>
            <Button variant="primary" size="sm" leading={<Plus />} onClick={() => setCreating(true)}>
              New company
            </Button>
          </>
        }
      />

      <Toolbar
        leading={
          <div className="w-64">
            <SearchInput inputSize="sm" value={query} onChange={(e) => setQuery(e.target.value)} onClear={() => setQuery("")} placeholder="Company or domain" />
          </div>
        }
      />

      <div className="min-h-0 flex-1 overflow-auto">
        <DataTable
          aria-label="Companies"
          rows={list.items}
          columns={columns}
          rowKey={(company) => company.id}
          onRowClick={(company) => navigate(`/companies/${company.id}`)}
          loading={list.isLoading}
          empty={
            <EmptyState
              icon={Building2}
              title="No companies yet"
              description="Companies are created when a customer is linked to an account ID, or you can add them manually."
            />
          }
        />
      </div>

      <Pagination
        hasPrevious={false}
        hasNext={list.hasMore}
        onPrevious={() => undefined}
        onNext={() => void list.fetchNext()}
        summary={`${list.items.length} loaded${list.hasMore ? "+" : ""}`}
      />

      <NewCompanyDialog open={creating} onOpenChange={setCreating} onCreated={(c) => navigate(`/companies/${c.id}`)} />
    </Page>
  );
}

function NewCompanyDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (company: Company) => void;
}) {
  const [name, setName] = useState("");
  const [domain, setDomain] = useState("");

  const create = useMutation<void, Company>(
    () => api.post<Company>("/companies", { name, domain: domain || null }),
    {
      invalidates: [["companies"]],
      onSuccess: (created) => {
        setName("");
        setDomain("");
        onOpenChange(false);
        onCreated(created);
      },
    },
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title="New company"
        footer={
          <Button variant="primary" size="sm" loading={create.isPending} disabled={!name.trim()} onClick={() => void create.mutate().catch(() => {})}>
            Create company
          </Button>
        }
      >
        {create.error ? (
          <Callout tone="danger" className="mb-3">
            {create.error instanceof ApiError ? create.error.message : "Could not create the company."}
          </Callout>
        ) : null}
        <div className="flex flex-col gap-3">
          <Field label="Name">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Acme Corp" autoFocus />
          </Field>
          <Field label="Domain (optional)">
            <Input value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="acme.com" />
          </Field>
        </div>
      </DialogContent>
    </Dialog>
  );
}
