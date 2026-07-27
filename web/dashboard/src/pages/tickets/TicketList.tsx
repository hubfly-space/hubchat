import {
  Avatar,
  Badge,
  BulkActionBar,
  Button,
  DataTable,
  EmptyState,
  FilterBar,
  Page,
  PageHeader,
  Pagination,
  PriorityIndicator,
  TagChip,
  TicketStatusBadge,
  Toolbar,
  Tooltip,
  formatRelativeShort,
  type Column,
  type FilterCondition,
  type FilterFieldDef,
  type Ticket,
} from "@hubchat/shared";
import {
  Bookmark,
  Building2,
  Circle,
  Download,
  Flag,
  Plus,
  TicketCheck,
  UserRound,
} from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";
import { NOW, tickets } from "../../data/fixtures";

const FILTER_FIELDS: FilterFieldDef[] = [
  {
    key: "status",
    label: "Status",
    icon: <Circle />,
    operators: ["is", "is_not", "in"],
    options: [
      { value: "new", label: "New" },
      { value: "open", label: "Open" },
      { value: "pending", label: "Pending" },
      { value: "on_hold", label: "On hold" },
      { value: "resolved", label: "Resolved" },
      { value: "closed", label: "Closed" },
    ],
  },
  {
    key: "priority",
    label: "Priority",
    icon: <Flag />,
    operators: ["is", "is_not", "in"],
    options: [
      { value: "urgent", label: "Urgent" },
      { value: "high", label: "High" },
      { value: "normal", label: "Normal" },
      { value: "low", label: "Low" },
    ],
  },
  { key: "assignee", label: "Assignee", icon: <UserRound />, operators: ["is", "is_not", "is_set", "is_not_set"] },
  { key: "company", label: "Company", icon: <Building2 />, operators: ["is", "in"] },
  { key: "created_at", label: "Created", icon: <Circle />, operators: ["gt", "lt"], group: "Dates" },
  { key: "due_at", label: "Due", icon: <Circle />, operators: ["gt", "lt"], group: "Dates" },
];

/**
 * Ticket queue (§6.3).
 *
 * A table rather than the inbox's list, because tickets are worked in bulk and
 * compared across columns — the questions here are "which of these is oldest"
 * and "who owns the urgent ones", not "what did they say".
 */
export default function TicketList() {
  const navigate = useNavigate();
  const { memberById, customerById, companyById, tagById } = useWorkspace();

  const [conditions, setConditions] = useState<FilterCondition[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [sort, setSort] = useState<{ key: string; direction: "asc" | "desc" }>({
    key: "created_at",
    direction: "desc",
  });

  const columns: Column<Ticket>[] = [
    {
      key: "number",
      header: "Ticket",
      width: "96px",
      cell: (ticket) => (
        <span className="font-mono text-xs text-fg-muted">
          {ticket.prefix}-{ticket.number}
        </span>
      ),
      sortable: true,
    },
    {
      key: "title",
      header: "Subject",
      cell: (ticket) => (
        <div className="flex min-w-0 items-center gap-2">
          <PriorityIndicator priority={ticket.priority} />
          <span className="min-w-0 flex-1 truncate text-fg">{ticket.title}</span>
          {ticket.tag_ids.slice(0, 2).map((tagId) => {
            const tag = tagById(tagId);
            return tag ? <TagChip key={tagId} label={tag.name} color={tag.color} /> : null;
          })}
        </div>
      ),
      sortable: true,
    },
    {
      key: "status",
      header: "Status",
      width: "116px",
      cell: (ticket) => <TicketStatusBadge status={ticket.status} />,
      sortable: true,
    },
    {
      key: "customer",
      header: "Customer",
      width: "180px",
      hideBelow: "lg",
      cell: (ticket) => {
        const customer = customerById(ticket.customer_id);
        const company = companyById(ticket.company_id);
        return (
          <div className="flex min-w-0 items-center gap-2">
            <Avatar name={customer?.name} seed={customer?.id ?? ticket.id} size="xs" />
            <span className="min-w-0">
              <span className="block truncate text-xs text-fg-secondary">
                {customer?.name ?? "Unknown"}
              </span>
              <span className="block truncate text-2xs text-fg-muted">{company?.name}</span>
            </span>
          </div>
        );
      },
    },
    {
      key: "assignee",
      header: "Assignee",
      width: "140px",
      hideBelow: "md",
      cell: (ticket) => {
        const assignee = memberById(ticket.assignee_id);
        return assignee ? (
          <span className="flex items-center gap-1.5">
            <Avatar name={assignee.name} seed={assignee.id} size="2xs" />
            <span className="truncate text-xs text-fg-secondary">{assignee.name}</span>
          </span>
        ) : (
          <Badge tone="warning">Unassigned</Badge>
        );
      },
    },
    {
      key: "due_at",
      header: "Due",
      width: "88px",
      numeric: true,
      hideBelow: "xl",
      cell: (ticket) =>
        ticket.due_at ? (
          <Tooltip content={ticket.due_at}>
            <span
              className={
                new Date(ticket.due_at) < NOW ? "text-xs text-danger-text" : "text-xs text-fg-muted"
              }
            >
              {formatRelativeShort(ticket.due_at, NOW)}
            </span>
          </Tooltip>
        ) : (
          <span className="text-xs text-fg-disabled">—</span>
        ),
      sortable: true,
    },
    {
      key: "created_at",
      header: "Created",
      width: "88px",
      numeric: true,
      cell: (ticket) => (
        <span className="text-xs text-fg-muted">{formatRelativeShort(ticket.created_at, NOW)}</span>
      ),
      sortable: true,
    },
  ];

  return (
    <Page>
      <PageHeader
        title="Tickets"
        description="Structured cases with status, ownership, and a due date."
        actions={
          <>
            <Button variant="secondary" size="sm" leading={<Download />}>
              Export
            </Button>
            <Button variant="primary" size="sm" leading={<Plus />}>
              New ticket
            </Button>
          </>
        }
      />

      <Toolbar
        leading={
          <FilterBar
            fields={FILTER_FIELDS}
            conditions={conditions}
            onChange={setConditions}
          />
        }
        trailing={
          conditions.length > 0 ? (
            <Button variant="ghost" size="sm" leading={<Bookmark />}>
              Save as view
            </Button>
          ) : null
        }
      />

      <div className="min-h-0 flex-1 overflow-auto">
        <DataTable
          aria-label="Tickets"
          rows={tickets}
          columns={columns}
          rowKey={(ticket) => ticket.id}
          onRowClick={(ticket) => navigate(`/tickets/${ticket.id}`)}
          selection={{ selected, onChange: setSelected }}
          sort={{
            key: sort.key,
            direction: sort.direction,
            onChange: (key, direction) => setSort({ key, direction }),
          }}
          empty={
            <EmptyState
              icon={TicketCheck}
              title="No tickets match these filters"
              description="Adjust or clear the filters above to see more."
              action={
                <Button variant="secondary" size="sm" onClick={() => setConditions([])}>
                  Clear filters
                </Button>
              }
            />
          }
        />
      </div>

      <Pagination
        hasPrevious={false}
        hasNext
        onPrevious={() => undefined}
        onNext={() => undefined}
        summary={`1–${tickets.length} of ${tickets.length}`}
        pageSize={50}
        onPageSizeChange={() => undefined}
      />

      <BulkActionBar count={selected.size} onClear={() => setSelected(new Set())}>
        <Button variant="ghost" size="sm">
          Assign
        </Button>
        <Button variant="ghost" size="sm">
          Set priority
        </Button>
        <Button variant="ghost" size="sm">
          Add tag
        </Button>
        <Button variant="ghost" size="sm">
          Resolve
        </Button>
      </BulkActionBar>
    </Page>
  );
}
