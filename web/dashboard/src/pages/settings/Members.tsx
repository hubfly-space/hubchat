import {
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  DataTable,
  Dialog,
  DialogClose,
  DialogContent,
  DialogTrigger,
  EmptyState,
  Field,
  IdentityCell,
  Input,
  Menu,
  MenuContent,
  MenuItem,
  MenuLabel,
  MenuSeparator,
  MenuTrigger,
  Page,
  PageBody,
  PageHeader,
  RadioGroup,
  SearchInput,
  Section,
  Select,
  Tabs,
  TabsContent,
  TabsList,
  Toolbar,
  Tooltip,
  formatRelativeShort,
  type BadgeTone,
  type Column,
  type Member,
  type MemberRole,
} from "@hubchat/shared";
import { MoreHorizontal, ShieldAlert, UserMinus, UserPlus, UsersRound } from "lucide-react";
import { useState } from "react";
import { useWorkspace } from "../../app/workspace-context";
import { NOW, members, teams } from "../../data/fixtures";

const ROLE: Record<MemberRole, { label: string; tone: BadgeTone; detail: string }> = {
  owner: { label: "Owner", tone: "accent", detail: "Full control, including deleting the workspace." },
  admin: { label: "Admin", tone: "info", detail: "Manages people, surfaces, and integrations." },
  manager: { label: "Manager", tone: "neutral", detail: "Runs queues, SLAs, and reporting." },
  agent: { label: "Agent", tone: "neutral", detail: "Reads and replies to permitted conversations." },
  developer: { label: "Developer", tone: "system", detail: "Integrations only — no conversation access by default." },
  analyst: { label: "Analyst", tone: "neutral", detail: "Read-only reports and records." },
};

const PENDING_INVITES = [
  { id: "inv_1", email: "noor@northwind.cloud", role: "agent" as MemberRole, sent: "2026-07-24T09:00:00Z" },
  { id: "inv_2", email: "contract-dev@vendor.example", role: "developer" as MemberRole, sent: "2026-07-19T14:30:00Z" },
];

/** Members and invitations (§5, §6.1). */
export default function Members() {
  const { viewer } = useWorkspace();
  const [tab, setTab] = useState("active");
  const [query, setQuery] = useState("");

  const rows = members.filter((member) =>
    `${member.name} ${member.email}`.toLowerCase().includes(query.toLowerCase()),
  );

  const columns: Column<Member>[] = [
    {
      key: "name",
      header: "Member",
      cell: (member) => (
        <IdentityCell
          name={member.name}
          secondary={member.email}
          seed={member.id}
          size="sm"
          status={member.presence}
        />
      ),
      sortable: true,
    },
    {
      key: "role",
      header: "Role",
      width: "128px",
      cell: (member) => (
        <Tooltip content={ROLE[member.role].detail}>
          <span>
            <Badge tone={ROLE[member.role].tone}>{ROLE[member.role].label}</Badge>
          </span>
        </Tooltip>
      ),
      sortable: true,
    },
    {
      key: "teams",
      header: "Teams",
      width: "200px",
      hideBelow: "lg",
      cell: (member) => (
        <span className="flex flex-wrap gap-1">
          {member.teams.length === 0 ? (
            <span className="text-xs text-fg-disabled">None</span>
          ) : (
            member.teams.map((teamId) => (
              <Badge key={teamId} tone="neutral" variant="outline">
                {teams.find((team) => team.id === teamId)?.name ?? teamId}
              </Badge>
            ))
          )}
        </span>
      ),
    },
    {
      key: "accepting",
      header: "Availability",
      width: "130px",
      hideBelow: "md",
      cell: (member) =>
        member.accepting_conversations ? (
          <Badge tone="success">Accepting</Badge>
        ) : (
          <span className="text-xs text-fg-muted">Not routing</span>
        ),
    },
    {
      key: "last_seen_at",
      header: "Last seen",
      width: "100px",
      numeric: true,
      cell: (member) =>
        member.last_seen_at ? (
          <span className="text-xs text-fg-muted">
            {formatRelativeShort(member.last_seen_at, NOW)}
          </span>
        ) : (
          <span className="text-xs text-fg-disabled">never</span>
        ),
      sortable: true,
    },
  ];

  return (
    <Page>
      <PageHeader
        title="Members"
        description="Who can sign in to this workspace, and what they are permitted to do."
        actions={
          <Dialog>
            <DialogTrigger asChild>
              <Button variant="primary" size="sm" leading={<UserPlus />}>
                Invite people
              </Button>
            </DialogTrigger>
            <DialogContent
              title="Invite people"
              description="They receive an email with a single-use link that expires in 7 days."
              size="lg"
              footer={
                <>
                  <DialogClose asChild>
                    <Button variant="ghost" size="sm">
                      Cancel
                    </Button>
                  </DialogClose>
                  <Button variant="primary" size="sm">
                    Send invitations
                  </Button>
                </>
              }
            >
              <div className="space-y-4 pb-2">
                <Field
                  label="Email addresses"
                  description="One per line. Everyone in this batch receives the same role."
                >
                  <Input placeholder="teammate@northwind.cloud" />
                </Field>

                <Field label="Role">
                  <RadioGroup
                    variant="cards"
                    aria-label="Role"
                    defaultValue="agent"
                    options={(Object.keys(ROLE) as MemberRole[])
                      .filter((role) => role !== "owner")
                      .map((role) => ({
                        value: role,
                        label: ROLE[role].label,
                        description: ROLE[role].detail,
                      }))}
                  />
                </Field>

                <Field label="Add to teams" description="Optional. Controls which inboxes they can see.">
                  <Select
                    aria-label="Teams"
                    options={teams.map((team) => ({ value: team.id, label: team.name }))}
                  />
                </Field>
              </div>
            </DialogContent>
          </Dialog>
        }
        tabs={
          <Tabs value={tab} onValueChange={setTab}>
            <TabsList
              items={[
                { value: "active", label: "Active", count: members.length },
                { value: "invites", label: "Pending invites", count: PENDING_INVITES.length },
              ]}
            />
          </Tabs>
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
              placeholder="Name or email"
            />
          </div>
        }
      />

      <PageBody>
        <Tabs value={tab} onValueChange={setTab}>
          <TabsContent value="active">
            <Card>
              <CardBody className="p-0">
                <DataTable
                  aria-label="Members"
                  rows={rows}
                  columns={columns}
                  rowKey={(member) => member.id}
                  rowActions={(member) => (
                    <Menu>
                      <MenuTrigger asChild>
                        <Button
                          variant="ghost"
                          size="xs"
                          iconOnly
                          aria-label={`Actions for ${member.name}`}
                          leading={<MoreHorizontal />}
                        />
                      </MenuTrigger>
                      <MenuContent align="end" className="w-56">
                        <MenuLabel>Change role</MenuLabel>
                        {(Object.keys(ROLE) as MemberRole[])
                          .filter((role) => role !== "owner")
                          .map((role) => (
                            <MenuItem key={role}>{ROLE[role].label}</MenuItem>
                          ))}
                        <MenuSeparator />
                        <MenuItem>Manage teams…</MenuItem>
                        <MenuItem>View audit trail</MenuItem>
                        <MenuSeparator />
                        <MenuItem
                          icon={<UserMinus />}
                          destructive
                          disabled={member.role === "owner" || member.id === viewer.id}
                        >
                          Remove from workspace
                        </MenuItem>
                      </MenuContent>
                    </Menu>
                  )}
                  empty={
                    <EmptyState
                      icon={UsersRound}
                      title="No members match"
                      description="Try a different search."
                    />
                  }
                />
              </CardBody>
            </Card>

            <Callout tone="info" className="mt-4" icon={<ShieldAlert />}>
              Roles are bundles of capabilities. What a role can actually do is enforced in the
              service layer on every request — hiding a control in the interface is a courtesy, not
              the security boundary.
            </Callout>
          </TabsContent>

          <TabsContent value="invites">
            <Card>
              <CardBody className="p-0">
                {PENDING_INVITES.length === 0 ? (
                  <EmptyState icon={UserPlus} size="sm" title="No pending invitations" />
                ) : (
                  <ul className="divide-y divide-line-subtle">
                    {PENDING_INVITES.map((invite) => (
                      <li key={invite.id} className="flex items-center gap-3 px-4 py-3">
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-sm text-fg">{invite.email}</span>
                          <span className="block text-xs text-fg-muted">
                            Sent {formatRelativeShort(invite.sent, NOW)} ago
                          </span>
                        </span>
                        <Badge tone={ROLE[invite.role].tone}>{ROLE[invite.role].label}</Badge>
                        <Button variant="ghost" size="sm">
                          Resend
                        </Button>
                        <Button variant="danger-ghost" size="sm">
                          Revoke
                        </Button>
                      </li>
                    ))}
                  </ul>
                )}
              </CardBody>
            </Card>
          </TabsContent>
        </Tabs>
      </PageBody>
    </Page>
  );
}
