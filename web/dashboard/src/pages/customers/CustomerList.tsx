import {
  api,
  Badge,
  BulkActionBar,
  Button,
  DataTable,
  EmptyState,
  FilterBar,
  IdentityCell,
  invalidate,
  Menu,
  MenuContent,
  MenuItem,
  MenuLabel,
  MenuTrigger,
  Page,
  PageHeader,
  Pagination,
  SearchInput,
  StatusDot,
  TagChip,
  Toolbar,
  downloadFile,
  useInfinite,
  useQuery,
  useToast,
  formatRelativeShort,
  type Column,
  type Company,
  type Customer,
  type FilterCondition,
  type FilterFieldDef,
  type Member,
  type Paginated,
  type Tag,
} from "@hubchat/shared";
import { Download, Tag as TagIcon, UserRound, Users } from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";

function conditionsToParams(conditions: FilterCondition[]): URLSearchParams {
  const params = new URLSearchParams();
  for (const condition of conditions) {
    if (typeof condition.value !== "string" || condition.value === "") continue;
    switch (condition.field) {
      case "verification":
        params.set("verification", condition.value);
        break;
      case "tag":
        params.set("tag_id", condition.value);
        break;
      case "company":
        params.set("company_id", condition.value);
        break;
    }
  }
  return params;
}

/**
 * Customer directory (§6.9). Every visitor and identified contact who has
 * ever reached out, regardless of channel.
 */
export default function CustomerList() {
  const navigate = useNavigate();
  const { members, tags, tagById, workspace } = useWorkspace();
  const toast = useToast();

  const [query, setQuery] = useState("");
  const [conditions, setConditions] = useState<FilterCondition[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const companies = useQuery<{ data: Company[] }>(["companies", "picker"], (signal) =>
    api.get("/companies?limit=100", { signal }),
  );

  const filterFields: FilterFieldDef[] = useMemo(
    () => [
      {
        key: "verification",
        label: "Verification",
        icon: <UserRound />,
        operators: ["is"],
        options: [
          { value: "verified", label: "Verified" },
          { value: "unverified", label: "Unverified" },
          { value: "anonymous", label: "Anonymous" },
        ],
      },
      {
        key: "tag",
        label: "Tag",
        icon: <TagIcon />,
        operators: ["is"],
        options: tags.map((t) => ({ value: t.id, label: t.name })),
      },
      {
        key: "company",
        label: "Company",
        icon: <Users />,
        operators: ["is"],
        options: (companies.data?.data ?? []).map((c) => ({ value: c.id, label: c.name })),
      },
    ],
    [tags, companies.data],
  );

  const filterParams = useMemo(() => {
    const params = conditionsToParams(conditions);
    if (query.trim()) params.set("q", query.trim());
    return params.toString();
  }, [conditions, query]);

  const list = useInfinite<Customer>(["customers", "directory", filterParams], (cursor, signal) => {
    const params = new URLSearchParams(filterParams);
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<Customer>>(`/customers?${params.toString()}`, { signal });
  });

  const companyById = useMemo(() => new Map((companies.data?.data ?? []).map((c) => [c.id, c])), [companies.data]);
  const memberById = useMemo(() => new Map(members.map((m) => [m.id, m])), [members]);

  const [bulkPending, setBulkPending] = useState(false);
  const runBulk = async (fn: (id: string) => Promise<unknown>) => {
    setBulkPending(true);
    try {
      await Promise.all([...selected].map(fn));
    } finally {
      invalidate(["customers"]);
      setBulkPending(false);
      setSelected(new Set());
    }
  };

  const columns: Column<Customer>[] = [
    {
      key: "name",
      header: "Customer",
      cell: (c) => <IdentityCell name={c.name ?? "Anonymous"} secondary={c.email ?? c.external_id ?? undefined} seed={c.id} />,
      sortable: true,
    },
    {
      key: "verification",
      header: "Verification",
      width: "120px",
      cell: (c) =>
        c.verification === "verified" ? (
          <Badge tone="success">Verified</Badge>
        ) : (
          <Badge tone={c.verification === "anonymous" ? "neutral" : "warning"}>{c.verification}</Badge>
        ),
    },
    {
      key: "company",
      header: "Company",
      width: "160px",
      hideBelow: "lg",
      cell: (c) => {
        const company = c.company_ids[0] ? companyById.get(c.company_ids[0]) : undefined;
        return <span className="truncate text-xs text-fg-secondary">{company?.name ?? "—"}</span>;
      },
    },
    {
      key: "owner",
      header: "Owner",
      width: "140px",
      hideBelow: "md",
      cell: (c) => {
        const owner = c.owner_id ? memberById.get(c.owner_id) : undefined;
        return <span className="truncate text-xs text-fg-secondary">{owner?.name ?? "—"}</span>;
      },
    },
    {
      key: "tags",
      header: "Tags",
      width: "160px",
      hideBelow: "lg",
      cell: (c) => (
        <div className="flex flex-wrap gap-1">
          {c.tag_ids.slice(0, 2).map((tagId) => {
            const tag = tagById(tagId);
            return tag ? <TagChip key={tagId} label={tag.name} color={tag.color} /> : null;
          })}
        </div>
      ),
    },
    {
      key: "last_seen_at",
      header: "Last seen",
      width: "110px",
      numeric: true,
      cell: (c) => (
        <span className="flex items-center justify-end gap-1.5 text-xs text-fg-muted">
          {c.presence === "online" && <StatusDot status="live" />}
          {c.last_seen_at ? `${formatRelativeShort(c.last_seen_at, new Date())} ago` : "never"}
        </span>
      ),
    },
  ];

  return (
    <Page>
      <PageHeader
        title="Customers"
        description="Everyone who has contacted you, identified or not."
        actions={
          <Button
            variant="secondary"
            size="sm"
            leading={<Download />}
            onClick={() => void downloadFile(`/customers/export?${filterParams}`, "customers-export.csv", workspace.id).catch((error) => toast.error({ title: "Could not export customers", description: error instanceof Error ? error.message : "Try again." }))}
          >
            Export
          </Button>
        }
      />

      <Toolbar
        leading={
          <div className="flex items-center gap-2">
            <SearchInput
              inputSize="sm"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onClear={() => setQuery("")}
              placeholder="Search by name, email…"
            />
            <FilterBar fields={filterFields} conditions={conditions} onChange={setConditions} />
          </div>
        }
      />

      <div className="min-h-0 flex-1 overflow-auto">
        <DataTable
          aria-label="Customers"
          rows={list.items}
          columns={columns}
          rowKey={(c) => c.id}
          onRowClick={(c) => navigate(`/customers/${c.id}`)}
          selection={{ selected, onChange: setSelected }}
          loading={list.isLoading}
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
        hasNext={list.hasMore}
        onPrevious={() => undefined}
        onNext={() => void list.fetchNext()}
        summary={`${list.items.length} loaded${list.hasMore ? "+" : ""}`}
      />

      <BulkActionBar count={selected.size} onClear={() => setSelected(new Set())}>
        <TagPickerMenu
          tags={tags}
          disabled={bulkPending}
          onSelect={(tagId) => void runBulk((id) => api.post(`/customers/${id}/tags`, { tag_id: tagId }))}
        />
        <OwnerPickerMenu
          members={members}
          disabled={bulkPending}
          onSelect={(ownerId) => void runBulk((id) => api.patch(`/customers/${id}/owner`, { owner_id: ownerId }))}
        />
      </BulkActionBar>
    </Page>
  );
}

function TagPickerMenu({
  tags,
  disabled,
  onSelect,
}: {
  tags: Tag[];
  disabled: boolean;
  onSelect: (tagId: string) => void;
}) {
  return (
    <Menu>
      <MenuTrigger asChild>
        <Button variant="ghost" size="sm" disabled={disabled}>
          Add tag
        </Button>
      </MenuTrigger>
      <MenuContent>
        <MenuLabel>Tags</MenuLabel>
        {tags.length === 0 ? (
          <div className="px-2 py-1.5 text-xs text-fg-muted">No tags yet</div>
        ) : (
          tags.map((tag) => (
            <MenuItem key={tag.id} onSelect={() => onSelect(tag.id)}>
              {tag.name}
            </MenuItem>
          ))
        )}
      </MenuContent>
    </Menu>
  );
}

function OwnerPickerMenu({
  members,
  disabled,
  onSelect,
}: {
  members: Member[];
  disabled: boolean;
  onSelect: (ownerId: string) => void;
}) {
  return (
    <Menu>
      <MenuTrigger asChild>
        <Button variant="ghost" size="sm" disabled={disabled}>
          Assign owner
        </Button>
      </MenuTrigger>
      <MenuContent>
        <MenuLabel>Owner</MenuLabel>
        {members.map((member) => (
          <MenuItem key={member.id} onSelect={() => onSelect(member.id)}>
            {member.name}
          </MenuItem>
        ))}
      </MenuContent>
    </Menu>
  );
}
