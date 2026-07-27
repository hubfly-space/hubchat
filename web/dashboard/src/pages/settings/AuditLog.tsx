import {
  Avatar,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CodeBlock,
  DataTable,
  EmptyState,
  FilterBar,
  Page,
  PageBody,
  PageHeader,
  Pagination,
  Toolbar,
  Tooltip,
  formatDateTime,
  formatRelativeShort,
  type AuditLog as AuditLogEntry,
  type Column,
  type FilterCondition,
  type FilterFieldDef,
} from "@hubchat/shared";
import { Calendar, Download, ScrollText, ShieldCheck, User } from "lucide-react";
import { useState } from "react";
import { useWorkspace } from "../../app/workspace-context";
import { NOW, auditLogs } from "../../data/fixtures";

const FILTER_FIELDS: FilterFieldDef[] = [
  { key: "actor", label: "Actor", icon: <User />, operators: ["is", "is_not"] },
  {
    key: "action",
    label: "Action",
    icon: <ShieldCheck />,
    operators: ["is", "starts_with"],
    options: [
      { value: "widget.updated", label: "widget.updated" },
      { value: "api_key.created", label: "api_key.created" },
      { value: "member.role_changed", label: "member.role_changed" },
      { value: "customer.merged", label: "customer.merged" },
    ],
  },
  { key: "occurred_at", label: "Date", icon: <Calendar />, operators: ["gt", "lt"] },
];

/** Audit log (§6.19). */
export default function AuditLog() {
  const { memberById } = useWorkspace();
  const [conditions, setConditions] = useState<FilterCondition[]>([]);
  const [expanded, setExpanded] = useState<string | null>(null);

  const columns: Column<AuditLogEntry>[] = [
    {
      key: "occurred_at",
      header: "When",
      width: "110px",
      numeric: true,
      cell: (entry) => (
        <Tooltip content={formatDateTime(entry.occurred_at)}>
          <span className="text-xs text-fg-muted">
            {formatRelativeShort(entry.occurred_at, NOW)}
          </span>
        </Tooltip>
      ),
      sortable: true,
    },
    {
      key: "actor",
      header: "Actor",
      width: "180px",
      cell: (entry) => (
        <span className="flex items-center gap-2">
          {entry.actor_type === "system" ? (
            <span className="grid size-5 shrink-0 place-items-center rounded-full bg-system-subtle">
              <span className="size-1.5 rounded-full bg-system" />
            </span>
          ) : (
            <Avatar
              name={entry.actor_name}
              seed={entry.actor_id ?? entry.actor_name}
              size="xs"
            />
          )}
          <span className="min-w-0 truncate text-xs text-fg-secondary">{entry.actor_name}</span>
        </span>
      ),
    },
    {
      key: "action",
      header: "Action",
      cell: (entry) => (
        <span className="font-mono text-xs text-fg">{entry.action}</span>
      ),
    },
    {
      key: "entity",
      header: "Entity",
      width: "200px",
      hideBelow: "lg",
      cell: (entry) => (
        <span className="font-mono text-xs text-fg-muted">
          {entry.entity_type}/{entry.entity_id}
        </span>
      ),
    },
    {
      key: "ip",
      header: "Source",
      width: "130px",
      hideBelow: "xl",
      cell: (entry) => (
        <span className="font-mono text-xs text-fg-muted">{entry.ip ?? "internal"}</span>
      ),
    },
  ];

  const active = auditLogs.find((entry) => entry.id === expanded);

  return (
    <Page>
      <PageHeader
        title="Audit log"
        description="Append-only record of who changed what. Cannot be edited or deleted from the interface."
        actions={
          <Button variant="secondary" size="sm" leading={<Download />}>
            Export
          </Button>
        }
      />

      <Toolbar
        leading={<FilterBar fields={FILTER_FIELDS} conditions={conditions} onChange={setConditions} />}
      />

      <div className="flex min-h-0 flex-1">
        <div className="min-w-0 flex-1 overflow-auto">
          <DataTable
            aria-label="Audit log"
            rows={auditLogs}
            columns={columns}
            rowKey={(entry) => entry.id}
            onRowClick={(entry) => setExpanded(entry.id)}
            empty={
              <EmptyState
                icon={ScrollText}
                title="No matching entries"
                description="Audit entries are written for authentication, configuration, and sensitive-data access."
              />
            }
          />
        </div>

        {active && (
          <aside className="hidden w-[380px] shrink-0 overflow-y-auto border-l border-line bg-surface p-4 xl:block">
            <p className="mb-1 font-mono text-sm text-fg">{active.action}</p>
            <p className="mb-4 text-xs text-fg-muted">{formatDateTime(active.occurred_at)}</p>

            <CodeBlock
              language="json"
              showLineNumbers
              code={JSON.stringify(
                {
                  id: active.id,
                  workspace_id: active.workspace_id,
                  actor: { type: active.actor_type, id: active.actor_id, name: active.actor_name },
                  action: active.action,
                  entity: { type: active.entity_type, id: active.entity_id },
                  request_id: active.request_id,
                  ip: active.ip,
                  metadata: active.metadata,
                  occurred_at: active.occurred_at,
                },
                null,
                2,
              )}
            />

            <p className="mt-3 text-2xs leading-normal text-fg-muted">
              The request ID correlates this entry with the server logs and, for API-originated
              changes, with the caller's own trace.
            </p>
          </aside>
        )}
      </div>

      <Pagination
        hasPrevious={false}
        hasNext
        onPrevious={() => undefined}
        onNext={() => undefined}
        summary={`${auditLogs.length} entries`}
      />

      <PageBody className="border-t border-line py-4">
        <Callout tone="info">
          Audit entries are retained for two years by default and are excluded from the customer
          deletion workflow — removing them would defeat their purpose. Adjust retention under
          Privacy & retention.
        </Callout>
      </PageBody>
    </Page>
  );
}
