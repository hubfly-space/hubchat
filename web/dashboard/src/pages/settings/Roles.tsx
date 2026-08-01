import {
  api,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  Checkbox,
  Page,
  PageBody,
  PageHeader,
  QueryBoundary,
  Section,
  Tooltip,
  cn,
  useQuery,
  useAllPages,
  type Paginated,
  type Capability,
  type Member,
  type MemberRole,
} from "@hubchat/shared";
import { Info, Lock } from "lucide-react";
import { Fragment } from "react";

const ROLES: MemberRole[] = ["owner", "admin", "manager", "agent", "developer", "analyst"];

const CAPABILITY_GROUPS: { group: string; items: { key: Capability; label: string; detail: string }[] }[] = [
  {
    group: "Conversations",
    items: [
      { key: "conversation.read", label: "Read conversations", detail: "See threads in permitted inboxes." },
      { key: "conversation.reply", label: "Reply publicly", detail: "Send messages the customer receives." },
      { key: "conversation.assign", label: "Assign", detail: "Change owner or team." },
      { key: "conversation.delete", label: "Delete and redact", detail: "Irreversible. Audited." },
    ],
  },
  {
    group: "Customers",
    items: [
      { key: "customer.read", label: "Read customers", detail: "View profiles and history." },
      { key: "customer.read_sensitive", label: "Reveal sensitive fields", detail: "Every reveal is written to the audit log." },
      { key: "customer.merge", label: "Merge identities", detail: "Combine duplicate customer records." },
    ],
  },
  {
    group: "Configuration",
    items: [
      { key: "ticket.manage", label: "Manage tickets", detail: "Fields, forms, and workflow." },
      { key: "widget.manage", label: "Manage widgets", detail: "Appearance, behaviour, install." },
      { key: "portal.manage", label: "Manage portals", detail: "Branding, domains, permissions." },
      { key: "knowledgebase.manage", label: "Manage knowledge base", detail: "Write and publish articles." },
      { key: "feedback.moderate", label: "Moderate feedback", detail: "Approve, merge, set status." },
      { key: "automation.manage", label: "Manage automation", detail: "Rules, macros, saved replies." },
      { key: "sla.manage", label: "Manage SLAs", detail: "Policies and business hours." },
    ],
  },
  {
    group: "Administration",
    items: [
      { key: "member.manage", label: "Manage members", detail: "Invite, change roles, remove." },
      { key: "integration.manage", label: "Manage integrations", detail: "API keys and webhooks." },
      { key: "report.read", label: "Read reports", detail: "Analytics and exports." },
      { key: "audit.read", label: "Read audit log", detail: "Who did what, and when." },
      { key: "workspace.manage", label: "Manage workspace", detail: "Settings, branding, retention." },
      { key: "workspace.manage_security", label: "Manage security", detail: "Authentication and session policy." },
    ],
  },
];

const ROLE_SUMMARY: Record<MemberRole, string> = {
  owner: "Everything, including transferring ownership and deleting the workspace. There is always exactly one.",
  admin: "Manages people, surfaces, and integrations. Cannot transfer ownership.",
  manager: "Runs queues, assignments, SLAs, and reporting. No workspace configuration.",
  agent: "Reads and replies to conversations in permitted inboxes. Cannot change configuration.",
  developer: "Integrations, metadata, and technical logs. No conversation access unless granted separately.",
  analyst: "Read-only. Reports and records, with no ability to reply or modify.",
};

type RoleDefinition = { key: MemberRole; name: string; description: string | null; capabilities: Capability[] };

/**
 * Roles and capabilities (§5.9).
 *
 * A read-only matrix in this release. It exists now, before custom roles ship,
 * because "what can an agent actually do?" is a question every administrator
 * asks on day one and the answer should not require reading source.
 */
export default function Roles() {
  const roles = useQuery<{ data: RoleDefinition[] }>(["roles"], (signal) => api.get("/roles", { signal }));
  const members = useAllPages<Member>(["members", "lookup"], (cursor, signal) => api.get<Paginated<Member>>(`/members?limit=200${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }));

  return (
    <Page>
      <PageHeader
        title="Roles & permissions"
        description="What each role is permitted to do. Capabilities are checked in the service layer on every request."
        actions={
          <Tooltip content="Custom roles arrive in a later release">
            <span>
              <Button variant="secondary" size="sm" leading={<Lock />} disabled>
                Create custom role
              </Button>
            </span>
          </Tooltip>
        }
      />

      <PageBody width="full">
        <Callout tone="info" className="mb-5" icon={<Info />}>
          Roles are presets over a capability set. When custom roles arrive, these six become
          editable templates rather than fixed definitions — no migration required, because the
          underlying model is already capability-based.
        </Callout>

        <QueryBoundary query={roles}>
          {(roleData) => {
            const capabilitiesFor = (role: MemberRole): Capability[] =>
              roleData.data.find((r) => r.key === role)?.capabilities ?? [];

            return (
              <>
                <Section title="Capability matrix">
                  <Card>
                    <CardBody className="overflow-x-auto p-0">
                      <table className="w-full min-w-[720px] text-sm">
                        <thead className="sticky top-0 z-[var(--z-sticky)]">
                          <tr>
                            <th className="border-b border-line bg-surface px-4 py-2.5 text-left text-2xs font-semibold uppercase tracking-caps text-fg-muted">
                              Capability
                            </th>
                            {ROLES.map((role) => (
                              <th
                                key={role}
                                className="border-b border-line bg-surface px-3 py-2.5 text-center text-2xs font-semibold uppercase tracking-caps text-fg-muted"
                              >
                                {role}
                              </th>
                            ))}
                          </tr>
                        </thead>

                        <tbody>
                          {CAPABILITY_GROUPS.map((group) => (
                            <Fragment key={group.group}>
                              <tr>
                                <td
                                  colSpan={ROLES.length + 1}
                                  className="border-b border-line bg-inset px-4 py-1.5 text-2xs font-semibold uppercase tracking-caps text-fg-muted"
                                >
                                  {group.group}
                                </td>
                              </tr>

                              {group.items.map((item) => (
                                <tr key={item.key} className="border-b border-line-subtle hover:bg-surface-hover">
                                  <td className="px-4 py-2">
                                    <p className="text-sm text-fg">{item.label}</p>
                                    <p className="font-mono text-2xs text-fg-muted">{item.key}</p>
                                    <p className="mt-0.5 text-xs text-fg-muted">{item.detail}</p>
                                  </td>

                                  {ROLES.map((role) => {
                                    const granted = capabilitiesFor(role).includes(item.key);
                                    return (
                                      <td key={role} className="px-3 py-2 text-center">
                                        <Checkbox
                                          checked={granted}
                                          disabled
                                          aria-label={`${role} ${granted ? "has" : "does not have"} ${item.key}`}
                                          className={cn("inline-flex", !granted && "opacity-25")}
                                        />
                                      </td>
                                    );
                                  })}
                                </tr>
                              ))}
                            </Fragment>
                          ))}
                        </tbody>
                      </table>
                    </CardBody>
                  </Card>
                </Section>

                <Section title="Role summaries">
                  <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                    {ROLES.map((role) => (
                      <Card key={role}>
                        <CardHeader
                          title={<span className="capitalize">{role}</span>}
                          actions={
                            <Badge tone="neutral">
                              {members.items.filter((member) => member.role === role).length}
                            </Badge>
                          }
                        />
                        <CardBody>
                          <p className="text-xs leading-normal text-fg-muted">{ROLE_SUMMARY[role]}</p>
                        </CardBody>
                      </Card>
                    ))}
                  </div>
                </Section>
              </>
            );
          }}
        </QueryBoundary>
      </PageBody>
    </Page>
  );
}
