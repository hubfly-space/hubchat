import {
  api,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  Checkbox,
  Dialog,
  DialogContent,
  Field,
  Input,
  Page,
  PageBody,
  PageHeader,
  QueryBoundary,
  Section,
  cn,
  idempotencyKey,
  useMutation,
  useQuery,
  useAllPages,
  type Paginated,
  type Capability,
  type BuiltinMemberRole,
  type Member,
  type RoleDefinition,
} from "@hubchat/shared";
import { Info, Pencil, Plus, Trash2 } from "lucide-react";
import { Fragment, useState } from "react";

const ROLES: BuiltinMemberRole[] = ["owner", "admin", "manager", "agent", "developer", "analyst"];

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

const ROLE_SUMMARY: Record<BuiltinMemberRole, string> = {
  owner: "Everything, including transferring ownership and deleting the workspace. There is always exactly one.",
  admin: "Manages people, surfaces, and integrations. Cannot transfer ownership.",
  manager: "Runs queues, assignments, SLAs, and reporting. No workspace configuration.",
  agent: "Reads and replies to conversations in permitted inboxes. Cannot change configuration.",
  developer: "Integrations, metadata, and technical logs. No conversation access unless granted separately.",
  analyst: "Read-only. Reports and records, with no ability to reply or modify.",
};

/**
 * Roles and capabilities (§5.9).
 */
export default function Roles() {
  const roles = useQuery<{ data: RoleDefinition[] }>(["roles"], (signal) => api.get("/roles", { signal }));
  const members = useAllPages<Member>(["members", "lookup"], (cursor, signal) => api.get<Paginated<Member>>(`/members?limit=200${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }));
  const [editor, setEditor] = useState<RoleDefinition | null | false>(false);
  const customRoles = roles.data?.data.filter((role) => !role.is_builtin) ?? [];
  const save = useMutation<{
    id?: string;
    key?: string;
    name: string;
    description: string;
    capabilities: Capability[];
  }, RoleDefinition>((input) => input.id
    ? api.patch(`/roles/${encodeURIComponent(input.id)}`, { name: input.name, description: input.description, capabilities: input.capabilities })
    : api.post("/roles", { key: input.key, name: input.name, description: input.description, capabilities: input.capabilities }, { idempotencyKey: idempotencyKey() }),
  { invalidates: [["roles"]], onSuccess: () => setEditor(false) });
  const remove = useMutation<string, void>((id) => api.delete(`/roles/${encodeURIComponent(id)}`, { idempotencyKey: idempotencyKey() }), { invalidates: [["roles"], ["members"]] });

  return (
    <Page>
      <PageHeader
        title="Roles & permissions"
        description="Built-in presets and workspace-specific roles. Capabilities are checked in the service layer on every request."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />} onClick={() => setEditor(null)}>Create custom role</Button>
        }
      />

      <PageBody width="full">
        <Callout tone="info" className="mb-5" icon={<Info />}>
          Roles are bundles of capabilities. Built-in roles are fixed and workspace custom roles
          can be edited or removed when no member is assigned to them.
        </Callout>

        <QueryBoundary query={roles}>
          {(roleData) => {
            const capabilitiesFor = (role: BuiltinMemberRole): Capability[] =>
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

                <Section title="Custom workspace roles">
                  {customRoles.length === 0 ? (
                    <Card><CardBody><p className="text-sm text-fg-muted">No custom roles yet. Create one when the built-in presets do not match your support workflow.</p></CardBody></Card>
                  ) : (
                    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                      {customRoles.map((role) => (
                        <Card key={role.id}>
                          <CardHeader
                            title={role.name}
                            description={<span className="font-mono text-2xs">{role.key}</span>}
                            actions={<div className="flex items-center gap-1"><Button variant="ghost" size="xs" iconOnly leading={<Pencil />} aria-label={`Edit ${role.name}`} onClick={() => setEditor(role)} /><Button variant="danger-ghost" size="xs" iconOnly leading={<Trash2 />} aria-label={`Delete ${role.name}`} onClick={() => void remove.mutate(role.id).catch(() => {})} /></div>}
                          />
                          <CardBody><p className="text-xs text-fg-muted">{role.description || "No description."}</p><p className="mt-3 text-2xs text-fg-disabled">{role.capabilities.length} capabilities</p></CardBody>
                        </Card>
                      ))}
                    </div>
                  )}
                </Section>
              </>
            );
          }}
        </QueryBoundary>
      </PageBody>
      {editor !== false ? <RoleEditor role={editor} pending={save.isPending} error={save.error} onCancel={() => setEditor(false)} onSave={(input) => void save.mutate(input).catch(() => {})} /> : null}
    </Page>
  );
}

function RoleEditor({ role, pending, error, onCancel, onSave }: { role: RoleDefinition | null; pending: boolean; error: unknown; onCancel: () => void; onSave: (input: { id?: string; key?: string; name: string; description: string; capabilities: Capability[] }) => void }) {
  const [key, setKey] = useState(role?.key ?? "");
  const [name, setName] = useState(role?.name ?? "");
  const [description, setDescription] = useState(role?.description ?? "");
  const [capabilities, setCapabilities] = useState<Capability[]>(role?.capabilities ?? []);
  const toggle = (capability: Capability, checked: boolean) => setCapabilities((current) => checked ? [...current, capability] : current.filter((item) => item !== capability));
  return <Dialog open onOpenChange={(open) => !open && onCancel()}><DialogContent title={role ? `Edit ${role.name}` : "Create custom role"} description="Choose the capabilities this role grants. Changes take effect for assigned members on their next request." size="lg" footer={<><Button variant="ghost" size="sm" onClick={onCancel}>Cancel</Button><Button variant="primary" size="sm" loading={pending} disabled={!name.trim() || (!role && !/^[a-z][a-z0-9_]{1,31}$/.test(key))} onClick={() => onSave({ id: role?.id, key: role ? undefined : key, name: name.trim(), description: description.trim(), capabilities })}>{role ? "Save changes" : "Create role"}</Button></>}>{error ? <Callout tone="danger" className="mb-4">Could not save this role.</Callout> : null}<div className="space-y-4"><div className="grid gap-4 sm:grid-cols-2"><Field label="Key" description={role ? "The key cannot change after creation." : "Lowercase letters, numbers, and underscores."}><Input value={key} disabled={Boolean(role)} onChange={(event) => setKey(event.target.value)} placeholder="billing_specialist" /></Field><Field label="Name"><Input autoFocus={!role} value={name} onChange={(event) => setName(event.target.value)} placeholder="Billing specialist" /></Field></div><Field label="Description"><Input value={description} onChange={(event) => setDescription(event.target.value)} placeholder="Handles billing conversations and reports." /></Field><div><p className="mb-2 text-sm font-medium text-fg">Capabilities</p><div className="grid gap-2 sm:grid-cols-2">{CAPABILITY_GROUPS.flatMap((group) => group.items).map((item) => <label key={item.key} className="flex items-start gap-2 rounded-md border border-line-subtle px-3 py-2"><Checkbox checked={capabilities.includes(item.key)} onCheckedChange={(checked) => toggle(item.key, checked === true)} /><span><span className="block text-xs text-fg">{item.label}</span><span className="block font-mono text-2xs text-fg-muted">{item.key}</span></span></label>)}</div></div></div></DialogContent></Dialog>;
}
