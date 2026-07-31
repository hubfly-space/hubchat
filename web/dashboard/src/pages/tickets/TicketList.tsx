import {
  api,
  Avatar,
  Badge,
  BulkActionBar,
  Button,
  DataTable,
  Dialog,
  DialogClose,
  DialogContent,
  EmptyState,
  Field,
  FilterBar,
  invalidate,
  Input,
  Menu,
  MenuContent,
  MenuItem,
  MenuLabel,
  MenuTrigger,
  Page,
  PageHeader,
  Pagination,
  PriorityIndicator,
  Select,
  TagChip,
  TicketStatusBadge,
  Toolbar,
  Tooltip,
  idempotencyKey,
  useInfinite,
  useMutation,
  useQuery,
  formatRelativeShort,
  type Column,
  type Customer,
  type FilterCondition,
  type FilterFieldDef,
  type Paginated,
  type SavedView,
  type Ticket,
} from "@hubchat/shared";
import { Circle, Download, Flag, TicketCheck, UserRound, Users2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";
import { NewTicketDialog } from "./NewTicketDialog";

// Only fields the backend can actually filter on are offered. Saved views use
// this same condition shape, so a view opened from a link cannot invent a
// client-only filter and silently show the wrong queue.
const FILTER_FIELDS: FilterFieldDef[] = [
  {
    key: "status",
    label: "Status",
    icon: <Circle />,
    operators: ["is"],
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
    operators: ["is"],
    options: [
      { value: "urgent", label: "Urgent" },
      { value: "high", label: "High" },
      { value: "normal", label: "Normal" },
      { value: "low", label: "Low" },
    ],
  },
];

function fieldsWithDirectory(members: { id: string; name: string }[], teams: { id: string; name: string }[]): FilterFieldDef[] {
  return [
    ...FILTER_FIELDS,
    {
      key: "assignee",
      label: "Assignee",
      icon: <UserRound />,
      operators: ["is", "is_not_set"],
      options: members.map((m) => ({ value: m.id, label: m.name })),
    },
    {
      key: "team",
      label: "Team",
      icon: <Users2 />,
      operators: ["is"],
      options: teams.map((t) => ({ value: t.id, label: t.name })),
    },
  ];
}

function conditionsToParams(conditions: FilterCondition[]): URLSearchParams {
  const params = new URLSearchParams();
  for (const condition of conditions) {
    if (condition.operator === "is_not_set" && condition.field === "assignee") {
      params.set("assignee_id", "unassigned");
      continue;
    }
    if (condition.operator !== "is" && condition.operator !== "in") continue;
    const values = Array.isArray(condition.value)
      ? condition.value.map(String).filter(Boolean)
      : typeof condition.value === "string" && condition.value !== ""
        ? [condition.value]
        : [];
    if (values.length === 0) continue;
    const firstValue = values[0];
    if (!firstValue) continue;
    switch (condition.field) {
      case "status":
        params.set("status", values.join(","));
        break;
      case "priority":
        params.set("priority", firstValue);
        break;
      case "assignee":
        params.set("assignee_id", firstValue);
        break;
      case "team":
        params.set("team_id", firstValue);
        break;
    }
  }
  return params;
}

function decodeConditions(value: string | null): FilterCondition[] {
  if (!value) return [];
  try {
    const decoded: unknown = JSON.parse(value);
    return Array.isArray(decoded) ? (decoded as FilterCondition[]) : [];
  } catch {
    return [];
  }
}

function viewConditions(view: SavedView): FilterCondition[] {
  return Array.isArray(view.filters?.conditions) ? view.filters.conditions : [];
}

/**
 * Ticket queue (§6.3).
 *
 * A table rather than the inbox's list, because tickets are worked in bulk
 * and compared across columns — the questions here are "which of these is
 * oldest" and "who owns the urgent ones", not "what did they say".
 */
export default function TicketList() {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const { members, teams, tags, tagById } = useWorkspace();

  const activeViewID = searchParams.get("view") ?? "";
  const encodedFilters = searchParams.get("filters");
  const [conditions, setConditions] = useState<FilterCondition[]>(() => decodeConditions(encodedFilters));
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [newTicketOpen, setNewTicketOpen] = useState(false);
  const [saveViewOpen, setSaveViewOpen] = useState(false);
  const [saveViewName, setSaveViewName] = useState("");
  const [saveViewScope, setSaveViewScope] = useState<"personal" | "team" | "workspace">("personal");
  const [saveViewTeamID, setSaveViewTeamID] = useState("");
  const appliedViewRef = useRef<string | null>(null);

  const filterFields = useMemo(() => fieldsWithDirectory(members, teams), [members, teams]);
  const savedViews = useInfinite<SavedView>(["saved-views", "ticket"], (cursor, signal) => {
    const params = new URLSearchParams({ entity_type: "ticket", limit: "50" });
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<SavedView>>(`/saved-views?${params.toString()}`, { signal });
  });
  const selectedView = useQuery<SavedView>(
    activeViewID ? ["saved-view", activeViewID] : null,
    (signal) => api.get(`/saved-views/${encodeURIComponent(activeViewID)}`, { signal }),
  );
  const availableViews = useMemo(() => {
    const byID = new Map(savedViews.items.map((view) => [view.id, view]));
    if (selectedView.data) byID.set(selectedView.data.id, selectedView.data);
    return [...byID.values()];
  }, [savedViews.items, selectedView.data]);

  useEffect(() => {
    if (activeViewID) {
      const view = availableViews.find((item) => item.id === activeViewID);
      if (!view || appliedViewRef.current === view.id) return;
      setConditions(viewConditions(view));
      appliedViewRef.current = view.id;
      return;
    }
    if (appliedViewRef.current !== null || encodedFilters !== null) {
      setConditions(decodeConditions(encodedFilters));
      appliedViewRef.current = null;
    }
  }, [activeViewID, availableViews, encodedFilters]);

  const changeConditions = (next: FilterCondition[]) => {
    setConditions(next);
    appliedViewRef.current = null;
    setSearchParams((current) => {
      current.delete("view");
      if (next.length > 0) current.set("filters", JSON.stringify(next));
      else current.delete("filters");
      return current;
    }, { replace: true });
  };

  const selectSavedView = (viewID: string) => {
    setSearchParams((current) => {
      current.delete("filters");
      if (viewID) current.set("view", viewID);
      else current.delete("view");
      return current;
    }, { replace: true });
  };

  const filterParams = useMemo(() => conditionsToParams(conditions).toString(), [conditions]);

  const list = useInfinite<Ticket>(["tickets", filterParams], (cursor, signal) => {
    const params = new URLSearchParams(filterParams);
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<Ticket>>(`/tickets?${params.toString()}`, { signal });
  });

  const saveView = useMutation<
    { name: string; entity_type: "ticket"; scope: "personal" | "team" | "workspace"; team_id: string; filters: { match: "all"; conditions: FilterCondition[] }; sort: { field: string; direction: "desc" } },
    SavedView
  >(
    (input) => api.post("/saved-views", input, { idempotencyKey: idempotencyKey() }),
    {
      invalidates: [["saved-views", "ticket"]],
      onSuccess: (view) => {
        setSaveViewOpen(false);
        setSaveViewName("");
        setSaveViewScope("personal");
        selectSavedView(view.id);
      },
    },
  );

  const customerIds = useMemo(
    () => [...new Set(list.items.map((t) => t.customer_id).filter((id): id is string => !!id))],
    [list.items],
  );
  const customers = useQuery<{ data: Customer[] }>(
    customerIds.length > 0 ? ["customers", "by-ids", customerIds.join(",")] : null,
    (signal) => api.get(`/customers?ids=${customerIds.join(",")}`, { signal }),
  );
  const customersById = useMemo(
    () => new Map((customers.data?.data ?? []).map((c) => [c.id, c])),
    [customers.data],
  );

  const memberById = useMemo(() => new Map(members.map((m) => [m.id, m])), [members]);

  // Sorts what has loaded so far, client-side — the backend queue is always
  // most-recently-updated first (§16), and a column sort over unfetched rows
  // would either be a lie or force loading everything up front.
  const [sort, setSort] = useState<{ key: string; direction: "asc" | "desc" }>({
    key: "created_at",
    direction: "desc",
  });
  const sortedItems = useMemo(() => {
    const factor = sort.direction === "asc" ? 1 : -1;
    return [...list.items].sort((a, b) => {
      switch (sort.key) {
        case "number":
          return (a.number - b.number) * factor;
        case "title":
          return a.title.localeCompare(b.title) * factor;
        case "created_at":
          return (new Date(a.created_at).getTime() - new Date(b.created_at).getTime()) * factor;
        default:
          return 0;
      }
    });
  }, [list.items, sort]);

  const [bulkPending, setBulkPending] = useState(false);
  const runBulk = async (fn: (id: string) => Promise<unknown>) => {
    setBulkPending(true);
    try {
      await Promise.all([...selected].map(fn));
    } finally {
      invalidate(["tickets"]);
      setBulkPending(false);
      setSelected(new Set());
    }
  };

  const columns: Column<Ticket>[] = [
    {
      key: "number",
      header: "Ticket",
      width: "96px",
      cell: (t) => (
        <span className="font-mono text-xs text-fg-muted">
          {t.prefix}-{t.number}
        </span>
      ),
      sortable: true,
    },
    {
      key: "title",
      header: "Subject",
      cell: (t) => (
        <div className="flex min-w-0 items-center gap-2">
          <PriorityIndicator priority={t.priority} />
          <span className="min-w-0 flex-1 truncate text-fg">{t.title}</span>
          {t.tag_ids.slice(0, 2).map((tagId) => {
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
      cell: (t) => <TicketStatusBadge status={t.status} />,
    },
    {
      key: "customer",
      header: "Customer",
      width: "180px",
      hideBelow: "lg",
      cell: (t) => {
        const customer = t.customer_id ? customersById.get(t.customer_id) : undefined;
        return (
          <div className="flex min-w-0 items-center gap-2">
            <Avatar name={customer?.name} seed={customer?.id ?? t.id} size="xs" />
            <span className="min-w-0 truncate text-xs text-fg-secondary">{customer?.name ?? "Unknown"}</span>
          </div>
        );
      },
    },
    {
      key: "assignee",
      header: "Assignee",
      width: "140px",
      hideBelow: "md",
      cell: (t) => {
        const assignee = t.assignee_id ? memberById.get(t.assignee_id) : undefined;
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
      cell: (t) =>
        t.due_at ? (
          <Tooltip content={t.due_at}>
            <span className={new Date(t.due_at) < new Date() ? "text-xs text-danger-text" : "text-xs text-fg-muted"}>
              {formatRelativeShort(t.due_at, new Date())}
            </span>
          </Tooltip>
        ) : (
          <span className="text-xs text-fg-disabled">—</span>
        ),
    },
    {
      key: "created_at",
      header: "Created",
      width: "88px",
      numeric: true,
      cell: (t) => <span className="text-xs text-fg-muted">{formatRelativeShort(t.created_at, new Date())}</span>,
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
            <Button
              variant="secondary"
              size="sm"
              leading={<Download />}
              onClick={() => window.open(`/api/v1/tickets/export?${filterParams}`, "_blank")}
            >
              Export
            </Button>
            <Button variant="primary" size="sm" onClick={() => setNewTicketOpen(true)}>
              New ticket
            </Button>
          </>
        }
      />

      <Toolbar
        leading={
          <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
            <FilterBar fields={filterFields} conditions={conditions} onChange={changeConditions} />
            <Select
              size="sm"
              value={activeViewID}
              onValueChange={selectSavedView}
              aria-label="Ticket saved view"
              options={[
                { value: "", label: "Current filters" },
                ...availableViews.map((view) => ({ value: view.id, label: view.name })),
              ]}
            />
            <Button variant="ghost" size="sm" onClick={() => setSaveViewOpen(true)}>
              Save view
            </Button>
          </div>
        }
      />

      <div className="min-h-0 flex-1 overflow-auto">
        <DataTable
          aria-label="Tickets"
          rows={sortedItems}
          columns={columns}
          rowKey={(t) => t.id}
          onRowClick={(t) => navigate(`/tickets/${t.id}`)}
          selection={{ selected, onChange: setSelected }}
          sort={{ key: sort.key, direction: sort.direction, onChange: (key, direction) => setSort({ key, direction }) }}
          loading={list.isLoading}
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
        hasNext={list.hasMore}
        onPrevious={() => undefined}
        onNext={() => void list.fetchNext()}
        summary={`${list.items.length} loaded${list.hasMore ? "+" : ""}`}
      />

      <BulkActionBar count={selected.size} onClear={() => setSelected(new Set())}>
        <Button variant="ghost" size="sm" loading={bulkPending} onClick={() => void runBulk((id) => api.patch(`/tickets/${id}/status`, { status: "resolved" }))}>
          Resolve
        </Button>
        <Menu>
          <MenuTrigger asChild>
            <Button variant="ghost" size="sm" loading={bulkPending}>
              Set priority
            </Button>
          </MenuTrigger>
          <MenuContent>
            <MenuLabel>Priority</MenuLabel>
            {(["urgent", "high", "normal", "low"] as const).map((priority) => (
              <MenuItem key={priority} onSelect={() => void runBulk((id) => api.patch(`/tickets/${id}/priority`, { priority }))}>
                {priority}
              </MenuItem>
            ))}
          </MenuContent>
        </Menu>
        <Menu>
          <MenuTrigger asChild>
            <Button variant="ghost" size="sm" loading={bulkPending}>
              Add tag
            </Button>
          </MenuTrigger>
          <MenuContent>
            <MenuLabel>Tags</MenuLabel>
            {tags.map((tag) => (
              <MenuItem key={tag.id} onSelect={() => void runBulk((id) => api.post(`/tickets/${id}/tags`, { tag_id: tag.id }))}>
                {tag.name}
              </MenuItem>
            ))}
          </MenuContent>
        </Menu>
      </BulkActionBar>

      <NewTicketDialog
        open={newTicketOpen}
        onOpenChange={setNewTicketOpen}
        onCreated={(t) => {
          invalidate(["tickets"]);
          navigate(`/tickets/${t.id}`);
        }}
      />

      <Dialog open={saveViewOpen} onOpenChange={setSaveViewOpen}>
        <DialogContent
          title="Save ticket view"
          description="Save the current ticket filters as a reusable queue view."
          footer={
            <>
              <DialogClose asChild><Button variant="ghost" size="sm">Cancel</Button></DialogClose>
              <Button
                variant="primary"
                size="sm"
                loading={saveView.isPending}
                disabled={!saveViewName.trim() || (saveViewScope === "team" && !(saveViewTeamID || teams[0]?.id))}
                onClick={() => void saveView.mutate({
                  name: saveViewName.trim(),
                  entity_type: "ticket",
                  scope: saveViewScope,
                  team_id: saveViewScope === "team" ? saveViewTeamID || teams[0]?.id || "" : "",
                  filters: { match: "all", conditions },
                  sort: { field: "updated_at", direction: "desc" },
                }).catch(() => {})}
              >
                Save view
              </Button>
            </>
          }
        >
          <div className="space-y-4 pb-2">
            <Field label="Name" required>
              <Input autoFocus value={saveViewName} onChange={(event) => setSaveViewName(event.target.value)} placeholder="Urgent tickets" />
            </Field>
            <Field label="Visibility">
              <Select
                value={saveViewScope}
                onValueChange={(value) => setSaveViewScope(value as "personal" | "team" | "workspace")}
                options={[
                  { value: "personal", label: "Only me" },
                  { value: "workspace", label: "Everyone in the workspace" },
                  { value: "team", label: "A team" },
                ]}
              />
            </Field>
            {saveViewScope === "team" && (
              <Field label="Team" required>
                <Select
                  value={saveViewTeamID || teams[0]?.id || ""}
                  onValueChange={setSaveViewTeamID}
                  disabled={teams.length === 0}
                  options={teams.map((team) => ({ value: team.id, label: team.name }))}
                />
              </Field>
            )}
            {Boolean(saveView.error) && <p className="text-sm text-danger">Could not save this view. Try again.</p>}
          </div>
        </DialogContent>
      </Dialog>
    </Page>
  );
}
