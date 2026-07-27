import {
  Badge,
  BulkActionBar,
  Button,
  DataTable,
  EmptyState,
  FilterBar,
  IdentityCell,
  Page,
  PageHeader,
  Pagination,
  SearchInput,
  StatusDot,
  TagChip,
  Toolbar,
  Tooltip,
  formatRelativeShort,
  type Column,
  type Customer,
  type FilterCondition,
  type FilterFieldDef,
} from "@hubchat/shared";
import { BadgeCheck, Building2, Download, ShieldQuestion, Tag, Upload, UserRound, Users } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";
import { NOW, customers } from "../../data/fixtures";

const FILTER_FIELDS: FilterFieldDef[] = [
  {
    key: "verification",
    label: "Identity",
    icon: <BadgeCheck />,
    operators: ["is", "is_not"],
    options: [
      { value: "verified", label: "Verified" },
      { value: "unverified", label: "Unverified" },
      { value: "anonymous", label: "Anonymous" },
    ],
  },
  { key: "company", label: "Company", icon: <Building2 />, operators: ["is", "in"] },
  { key: "tag", label: "Tag", icon: <Tag />, operators: ["is", "is_not", "in"] },
  {
    key: "plan",
    label: "Plan",
    icon: <UserRound />,
    operators: ["is", "in"],
    group: "Attributes",
    options: [
      { value: "starter", label: "Starter" },
      { value: "growth", label: "Growth" },
      { value: "enterprise", label: "Enterprise" },
    ],
  },
  { key: "last_seen_at", label: "Last seen", icon: <UserRound />, operators: ["gt", "lt"], group: "Activity" },
];

/** Customer directory (§6.9). */
export default function CustomerList() {
  const navigate = useNavigate();
  const { companyById, tagById } = useWorkspace();
  const [query, setQuery] = useState("");
  const [conditions, setConditions] = useState<FilterCondition[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const rows = customers.filter((customer) => {
    if (!query) return true;
    const haystack = `${customer.name ?? ""} ${customer.email ?? ""} ${customer.external_id ?? ""}`;
    return haystack.toLowerCase().includes(query.toLowerCase());
  });

  const columns: Column<Customer>[] = [
    {
      key: "name",
      header: "Customer",
      cell: (customer) => (
        <IdentityCell
          name={customer.name ?? "Anonymous visitor"}
          secondary={customer.email ?? customer.external_id ?? "No contact details"}
          seed={customer.id}
          status={customer.presence === "online" ? "online" : null}
        />
      ),
      sortable: true,
    },
    {
      key: "verification",
      header: "Identity",
      width: "116px",
      cell: (customer) =>
        customer.verification === "verified" ? (
          <Badge tone="success" leading={<BadgeCheck />}>
            Verified
          </Badge>
        ) : (
          <Tooltip
            content={
              customer.verification === "anonymous"
                ? "No identity supplied. Do not disclose account details."
                : "Self-reported. Not backed by a signed token."
            }
          >
            <span>
              <Badge
                tone={customer.verification === "anonymous" ? "neutral" : "warning"}
                leading={<ShieldQuestion />}
              >
                {customer.verification === "anonymous" ? "Anonymous" : "Unverified"}
              </Badge>
            </span>
          </Tooltip>
        ),
    },
    {
      key: "company",
      header: "Company",
      width: "180px",
      hideBelow: "md",
      cell: (customer) => {
        const company = companyById(customer.company_ids[0] ?? null);
        return company ? (
          <span className="truncate text-xs text-fg-secondary">{company.name}</span>
        ) : (
          <span className="text-xs text-fg-disabled">—</span>
        );
      },
    },
    {
      key: "plan",
      header: "Plan",
      width: "104px",
      hideBelow: "lg",
      cell: (customer) => (
        <span className="text-xs capitalize text-fg-secondary">
          {String(customer.attributes.plan ?? "—")}
        </span>
      ),
    },
    {
      key: "tags",
      header: "Tags",
      width: "160px",
      hideBelow: "xl",
      cell: (customer) => (
        <span className="flex flex-wrap gap-1">
          {customer.tag_ids.map((tagId) => {
            const tag = tagById(tagId);
            return tag ? <TagChip key={tagId} label={tag.name} color={tag.color} /> : null;
          })}
        </span>
      ),
    },
    {
      key: "last_seen_at",
      header: "Last seen",
      width: "100px",
      numeric: true,
      cell: (customer) => (
        <span className="flex items-center justify-end gap-1.5 text-xs text-fg-muted">
          {customer.presence === "online" && <StatusDot status="online" />}
          {customer.last_seen_at ? formatRelativeShort(customer.last_seen_at, NOW) : "never"}
        </span>
      ),
      sortable: true,
    },
  ];

  return (
    <Page>
      <PageHeader
        title="Customers"
        description="Everyone who has contacted you, identified or not."
        actions={
          <>
            <Button variant="secondary" size="sm" leading={<Upload />}>
              Import CSV
            </Button>
            <Button variant="secondary" size="sm" leading={<Download />}>
              Export
            </Button>
          </>
        }
      />

      <Toolbar
        leading={
          <>
            <div className="w-64">
              <SearchInput
                inputSize="sm"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                onClear={() => setQuery("")}
                placeholder="Name, email, or external ID"
              />
            </div>
            <FilterBar fields={FILTER_FIELDS} conditions={conditions} onChange={setConditions} />
          </>
        }
      />

      <div className="min-h-0 flex-1 overflow-auto">
        <DataTable
          aria-label="Customers"
          rows={rows}
          columns={columns}
          rowKey={(customer) => customer.id}
          onRowClick={(customer) => navigate(`/customers/${customer.id}`)}
          selection={{ selected, onChange: setSelected }}
          empty={
            <EmptyState
              icon={Users}
              title="No customers match"
              description="Customer records are created automatically the first time someone contacts you."
            />
          }
        />
      </div>

      <Pagination
        hasPrevious={false}
        hasNext={false}
        onPrevious={() => undefined}
        onNext={() => undefined}
        summary={`${rows.length} of ${customers.length}`}
      />

      <BulkActionBar count={selected.size} onClear={() => setSelected(new Set())}>
        <Button variant="ghost" size="sm">
          Add tag
        </Button>
        <Button variant="ghost" size="sm">
          Assign owner
        </Button>
        <Button variant="ghost" size="sm">
          Export selected
        </Button>
      </BulkActionBar>
    </Page>
  );
}
