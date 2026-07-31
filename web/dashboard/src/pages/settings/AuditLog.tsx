import {
  api,
  Avatar,
  Button,
  Callout,
  CodeBlock,
  DataTable,
  EmptyState,
  FilterBar,
  Page,
  PageBody,
  PageHeader,
  Toolbar,
  Tooltip,
  formatDateTime,
  formatRelativeShort,
  useInfinite,
  useQuery,
  type AuditLog as AuditLogEntry,
  type Column,
  type FilterCondition,
  type FilterFieldDef,
  type Member,
  type Paginated,
} from "@hubchat/shared";
import { ScrollText, ShieldCheck, User } from "lucide-react";
import { useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";

// Actions Stage 1 modules actually emit. As later stages add their own audit
// entries, this list grows — it is not exhaustive of every string the
// `action` column can ever hold.
const KNOWN_ACTIONS = [
  "user.signed_in",
  "user.signed_out",
  "user.sign_in_failed",
  "user.password_changed",
  "user.totp_enabled",
  "user.totp_disabled",
  "session.revoked",
  "workspace.updated",
  "workspace.security_settings_changed",
  "workspace.privacy_settings_changed",
  "member.invited",
  "member.joined",
  "member.role_changed",
  "member.removed",
  "team.created",
  "team.updated",
  "team.deleted",
  "tag.created",
  "tag.deleted",
  "tag.merged",
];

/** Audit log (§6.19). */
export default function AuditLog() {
  const [searchParams] = useSearchParams();
  const actorIdFromLink = searchParams.get("actor_id");

  const [conditions, setConditions] = useState<FilterCondition[]>(
    actorIdFromLink ? [{ field: "actor", operator: "is", value: actorIdFromLink }] : [],
  );
  const [expanded, setExpanded] = useState<string | null>(null);

  const members = useQuery<{ data: Member[] }>(["members"], (signal) => api.get("/members", { signal }));

  const actorId = conditionValue(conditions, "actor");
  const action = conditionValue(conditions, "action");

  const filterFields: FilterFieldDef[] = useMemo(
    () => [
      {
        key: "actor",
        label: "Actor",
        icon: <User />,
        operators: ["is"],
        options: (members.data?.data ?? []).map((member) => ({ value: member.id, label: member.name })),
      },
      {
        key: "action",
        label: "Action",
        icon: <ShieldCheck />,
        operators: ["is"],
        options: KNOWN_ACTIONS.map((value) => ({ value, label: value })),
      },
    ],
    [members.data],
  );

  const log = useInfinite<AuditLogEntry>(
    ["audit-logs", actorId ?? "", action ?? ""],
    (cursor, signal) => {
      const params = new URLSearchParams();
      if (actorId) params.set("actor_id", actorId);
      if (action) params.set("action", action);
      if (cursor) params.set("cursor", cursor);
      return api.get<Paginated<AuditLogEntry>>(`/audit-logs?${params.toString()}`, { signal });
    },
  );

  const columns: Column<AuditLogEntry>[] = [
    {
      key: "occurred_at",
      header: "When",
      width: "110px",
      numeric: true,
      cell: (entry) => (
        <Tooltip content={formatDateTime(entry.occurred_at)}>
          <span className="text-xs text-fg-muted">{formatRelativeShort(entry.occurred_at, new Date())}</span>
        </Tooltip>
      ),
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
            <Avatar name={entry.actor_name} seed={entry.actor_id ?? entry.actor_name} size="xs" />
          )}
          <span className="min-w-0 truncate text-xs text-fg-secondary">{entry.actor_name || "Unknown"}</span>
        </span>
      ),
    },
    {
      key: "action",
      header: "Action",
      cell: (entry) => <span className="font-mono text-xs text-fg">{entry.action}</span>,
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
      cell: (entry) => <span className="font-mono text-xs text-fg-muted">{entry.ip ?? "internal"}</span>,
    },
  ];

  const active = log.items.find((entry) => entry.id === expanded);

  return (
    <Page>
      <PageHeader
        title="Audit log"
        description="Append-only record of who changed what. Cannot be edited or deleted from the interface."
      />

      <Toolbar leading={<FilterBar fields={filterFields} conditions={conditions} onChange={setConditions} />} />

      <div className="flex min-h-0 flex-1">
        <div className="min-w-0 flex-1 overflow-auto">
          {log.error ? (
            <Callout tone="danger" className="m-4">
              Could not load the audit log.
            </Callout>
          ) : (
            <DataTable
              aria-label="Audit log"
              rows={log.items}
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
          )}

          {log.hasMore && (
            <div className="flex justify-center p-4">
              <Button variant="secondary" size="sm" loading={log.isFetching} onClick={() => void log.fetchNext()}>
                Load more
              </Button>
            </div>
          )}
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

      <PageBody className="border-t border-line py-4">
        <Callout tone="info">
          Audit entries are retained per the schedule set under Privacy & retention, and are
          excluded from the customer deletion workflow — removing them would defeat their purpose.
        </Callout>
      </PageBody>
    </Page>
  );
}

function conditionValue(conditions: FilterCondition[], field: string): string | null {
  const condition = conditions.find((c) => c.field === field);
  return typeof condition?.value === "string" ? condition.value : null;
}
