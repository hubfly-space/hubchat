import {
  api,
  ApiError,
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
  Pagination,
  Pagination,
  RadioGroup,
  SearchInput,
  Switch,
  Tabs,
  TabsContent,
  TabsList,
  Toolbar,
  Tooltip,
  formatRelativeShort,
  invalidate,
  useInfinite,
  useMutation,
  useQuery,
  type BadgeTone,
  type Column,
  type Member,
  type MemberRole,
  type Paginated,
  type Paginated,
  type Team,
} from "@hubchat/shared";
import { MoreHorizontal, ShieldAlert, UserMinus, UserPlus, UsersRound } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";

const ROLE: Record<MemberRole, { label: string; tone: BadgeTone; detail: string }> = {
  owner: { label: "Owner", tone: "accent", detail: "Full control, including deleting the workspace." },
  admin: { label: "Admin", tone: "info", detail: "Manages people, surfaces, and integrations." },
  manager: { label: "Manager", tone: "neutral", detail: "Runs queues, SLAs, and reporting." },
  agent: { label: "Agent", tone: "neutral", detail: "Reads and replies to permitted conversations." },
  developer: { label: "Developer", tone: "system", detail: "Integrations only — no conversation access by default." },
  analyst: { label: "Analyst", tone: "neutral", detail: "Read-only reports and records." },
};

type Invite = {
  id: string;
  email: string;
  role: MemberRole;
  expires_at: string;
  created_at: string;
};

/** Members and invitations (§5, §6.1). */
export default function Members() {
  const { viewer } = useWorkspace();
  const navigate = useNavigate();
  const [tab, setTab] = useState("active");
  const [query, setQuery] = useState("");
  const [managingTeamsFor, setManagingTeamsFor] = useState<Member | null>(null);

  const members = useInfinite<Member>(["members", query], (cursor, signal) => {
    const params = new URLSearchParams({ q: query, limit: "50" });
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<Member>>(`/members?${params.toString()}`, { signal });
  });
  const teams = useQuery<{ data: Team[] }>(["teams"], (signal) => api.get("/teams", { signal }));
  const invites = useInfinite<Invite>(["invites"], (cursor, signal) => api.get<Paginated<Invite>>(`/invites?limit=25${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }));

  const setRole = useMutation<{ id: string; role: string }, unknown>(
    ({ id, role }) => api.patch(`/members/${id}/role`, { role }),
    { invalidates: [["members"]] },
  );
  const removeMember = useMutation<string, unknown>(
    (id) => api.delete(`/members/${id}`),
    { invalidates: [["members"]] },
  );
  const revokeInvite = useMutation<string, unknown>(
    (id) => api.delete(`/invites/${id}`),
    { invalidates: [["invites"]] },
  );

  const teamById = new Map((teams.data?.data ?? []).map((team) => [team.id, team]));

  const rows = members.items;

  const columns: Column<Member>[] = [
    {
      key: "name",
      header: "Member",
      cell: (member) => (
        <IdentityCell name={member.name} secondary={member.email} seed={member.id} size="sm" status={member.presence} />
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
                {teamById.get(teamId)?.name ?? teamId}
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
          <span className="text-xs text-fg-muted">{formatRelativeShort(member.last_seen_at, new Date())}</span>
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
        actions={<InviteDialog />}
        tabs={
          <Tabs value={tab} onValueChange={setTab}>
            <TabsList
              items={[
                { value: "active", label: "Active", count: rows.length },
                { value: "invites", label: "Pending invites", count: invites.items.length },
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
        {setRole.error ? (
          <Callout tone="danger" className="mb-4">
            {setRole.error instanceof ApiError ? setRole.error.message : "Could not change that member's role."}
          </Callout>
        ) : null}
        {removeMember.error ? (
          <Callout tone="danger" className="mb-4">
            {removeMember.error instanceof ApiError ? removeMember.error.message : "Could not remove that member."}
          </Callout>
        ) : null}

        <Tabs value={tab} onValueChange={setTab}>
          <TabsContent value="active">
            {members.isLoading ? <p className="p-4 text-sm text-fg-muted">Loading members…</p> : members.error ? <Callout tone="danger">{members.error instanceof ApiError ? members.error.message : "Could not load members."}</Callout> : (
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
                                <MenuItem
                                  key={role}
                                  disabled={member.role === "owner"}
                                  onSelect={() => void setRole.mutate({ id: member.id, role }).catch(() => {})}
                                >
                                  {ROLE[role].label}
                                </MenuItem>
                              ))}
                            <MenuSeparator />
                            <MenuItem onSelect={() => setManagingTeamsFor(member)}>Manage teams…</MenuItem>
                            <MenuItem onSelect={() => navigate(`/settings/audit?actor_id=${member.id}`)}>
                              View audit trail
                            </MenuItem>
                            <MenuSeparator />
                            <MenuItem
                              icon={<UserMinus />}
                              destructive
                              disabled={member.role === "owner" || member.id === viewer.id}
                              onSelect={() => void removeMember.mutate(member.id).catch(() => {})}
                            >
                              Remove from workspace
                            </MenuItem>
                          </MenuContent>
                        </Menu>
                      )}
                      empty={<EmptyState icon={UsersRound} title="No members match" description="Try a different search." />}
                    />
                  </CardBody>
                  {members.hasMore && <Pagination hasPrevious={false} hasNext onPrevious={() => undefined} onNext={() => void members.fetchNext()} summary={`${rows.length} members loaded`} />}
                </Card>
              )}

            <Callout tone="info" className="mt-4" icon={<ShieldAlert />}>
              Roles are bundles of capabilities. What a role can actually do is enforced in the
              service layer on every request — hiding a control in the interface is a courtesy, not
              the security boundary.
            </Callout>
          </TabsContent>

          <TabsContent value="invites">
            {invites.isLoading ? <p className="p-4 text-sm text-fg-muted">Loading invitations…</p> : invites.error ? <Callout tone="danger">{invites.error instanceof ApiError ? invites.error.message : "Could not load invitations."}</Callout> : (
                <Card>
                  <CardBody className="p-0">
                    {invites.items.length === 0 ? (
                      <EmptyState icon={UserPlus} size="sm" title="No pending invitations" />
                    ) : (
                      <ul className="divide-y divide-line-subtle">
                        {invites.items.map((invite) => (
                          <li key={invite.id} className="flex items-center gap-3 px-4 py-3">
                            <span className="min-w-0 flex-1">
                              <span className="block truncate text-sm text-fg">{invite.email}</span>
                              <span className="block text-xs text-fg-muted">
                                Sent {formatRelativeShort(invite.created_at, new Date())} ago
                              </span>
                            </span>
                            <Badge tone={ROLE[invite.role].tone}>{ROLE[invite.role].label}</Badge>
                            <Button
                              variant="danger-ghost"
                              size="sm"
                              loading={revokeInvite.isPending}
                              onClick={() => void revokeInvite.mutate(invite.id).catch(() => {})}
                            >
                              Revoke
                            </Button>
                          </li>
                        ))}
                      </ul>
                    )}
                  </CardBody>
                  {invites.hasMore && <Pagination hasPrevious={false} hasNext onPrevious={() => undefined} onNext={() => void invites.fetchNext()} summary={`${invites.items.length} invitations loaded`} />}
                </Card>
              )}
          </TabsContent>
        </Tabs>
      </PageBody>

      {managingTeamsFor ? (
        <ManageTeamsDialog member={managingTeamsFor} teams={teams.data?.data ?? []} onClose={() => setManagingTeamsFor(null)} />
      ) : null}
    </Page>
  );
}

function InviteDialog() {
  const [open, setOpen] = useState(false);
  const [emails, setEmails] = useState("");
  const [role, setRole] = useState<MemberRole>("agent");

  const invite = useMutation<{ emails: string[]; role: MemberRole }, void>(
    async ({ emails: list, role: r }) => {
      for (const email of list) {
        await api.post("/invites", { email, role: r });
      }
    },
    {
      onSuccess: () => {
        invalidate(["invites"]);
        setOpen(false);
        setEmails("");
      },
    },
  );

  const parsedEmails = emails
    .split(/[\n,]/)
    .map((value) => value.trim())
    .filter(Boolean);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
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
            <Button
              variant="primary"
              size="sm"
              loading={invite.isPending}
              disabled={parsedEmails.length === 0}
              onClick={() => void invite.mutate({ emails: parsedEmails, role }).catch(() => {})}
            >
              Send invitations
            </Button>
          </>
        }
      >
        <div className="space-y-4 pb-2">
          {invite.error ? (
            <Callout tone="danger">
              {invite.error instanceof ApiError ? invite.error.message : "Could not send those invitations."}
            </Callout>
          ) : null}

          <Field label="Email addresses" description="One per line. Everyone in this batch receives the same role.">
            <Input
              placeholder="teammate@example.com"
              value={emails}
              onChange={(event) => setEmails(event.target.value)}
            />
          </Field>

          <Field label="Role">
            <RadioGroup
              variant="cards"
              aria-label="Role"
              value={role}
              onValueChange={(value) => setRole(value as MemberRole)}
              options={(Object.keys(ROLE) as MemberRole[])
                .filter((r) => r !== "owner")
                .map((r) => ({ value: r, label: ROLE[r].label, description: ROLE[r].detail }))}
            />
          </Field>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function ManageTeamsDialog({ member, teams, onClose }: { member: Member; teams: Team[]; onClose: () => void }) {
  const toggle = useMutation<{ teamId: string; add: boolean }, unknown>(
    ({ teamId, add }) =>
      add ? api.put(`/teams/${teamId}/members/${member.id}`) : api.delete(`/teams/${teamId}/members/${member.id}`),
    {
      onSuccess: () => {
        invalidate(["teams"]);
        invalidate(["members"]);
      },
    },
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={`Teams for ${member.name}`}
        footer={
          <Button variant="primary" size="sm" onClick={onClose}>
            Done
          </Button>
        }
      >
        <div className="flex flex-col gap-1">
          {toggle.error ? (
            <Callout tone="danger" className="mb-2">
              {toggle.error instanceof ApiError ? toggle.error.message : "Could not update team membership."}
            </Callout>
          ) : null}

          {teams.length === 0 ? (
            <EmptyState size="sm" title="No teams yet" description="Create a team first, from Settings → Teams." />
          ) : (
            teams.map((team) => {
              const isMember = team.member_ids.includes(member.id);
              return (
                <div key={team.id} className="flex items-center justify-between gap-3 rounded-md px-2 py-2 hover:bg-inset">
                  <span className="text-sm text-fg">{team.name}</span>
                  <Switch
                    checked={isMember}
                    onCheckedChange={(checked) => void toggle.mutate({ teamId: team.id, add: checked }).catch(() => {})}
                    aria-label={`Member of ${team.name}`}
                  />
                </div>
              );
            })
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
