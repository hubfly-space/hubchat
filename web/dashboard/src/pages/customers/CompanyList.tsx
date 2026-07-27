import {
  Badge,
  Button,
  DataTable,
  EmptyState,
  IdentityCell,
  Page,
  PageHeader,
  Pagination,
  SearchInput,
  Toolbar,
  formatCompact,
  type Column,
  type Company,
} from "@hubchat/shared";
import { Building2, Download, Plus, Upload } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";
import { companies } from "../../data/fixtures";

/** Company directory (§6.9) — the account layer above individual contacts. */
export default function CompanyList() {
  const navigate = useNavigate();
  const { memberById } = useWorkspace();
  const [query, setQuery] = useState("");

  const rows = companies.filter((company) =>
    `${company.name} ${company.domain ?? ""}`.toLowerCase().includes(query.toLowerCase()),
  );

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
      cell: (company) => (
        <Badge tone={company.tier === "enterprise" ? "accent" : "neutral"}>
          {company.tier ?? "—"}
        </Badge>
      ),
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
        <span className={company.open_ticket_count > 3 ? "text-warning-text" : undefined}>
          {company.open_ticket_count}
        </span>
      ),
      sortable: true,
    },
    {
      key: "mrr",
      header: "MRR",
      width: "96px",
      numeric: true,
      hideBelow: "lg",
      cell: (company) => `$${formatCompact(Number(company.attributes.mrr ?? 0))}`,
    },
    {
      key: "owner",
      header: "Owner",
      width: "150px",
      hideBelow: "md",
      cell: (company) => {
        const owner = memberById(company.owner_id);
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
            <Button variant="secondary" size="sm" leading={<Upload />}>
              Import CSV
            </Button>
            <Button variant="secondary" size="sm" leading={<Download />}>
              Export
            </Button>
            <Button variant="primary" size="sm" leading={<Plus />}>
              New company
            </Button>
          </>
        }
      />

      <Toolbar
        leading={
          <div className="w-64">
            <SearchInput
              inputSize="sm"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              onClear={() => setQuery("")}
              placeholder="Company or domain"
            />
          </div>
        }
      />

      <div className="min-h-0 flex-1 overflow-auto">
        <DataTable
          aria-label="Companies"
          rows={rows}
          columns={columns}
          rowKey={(company) => company.id}
          onRowClick={(company) => navigate(`/companies/${company.id}`)}
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
        hasNext={false}
        onPrevious={() => undefined}
        onNext={() => undefined}
        summary={`${rows.length} companies`}
      />
    </Page>
  );
}
