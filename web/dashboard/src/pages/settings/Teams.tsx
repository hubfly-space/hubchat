import {
  api,
  ApiError,
  AvatarGroup,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  Dialog,
  DialogClose,
  DialogContent,
  DialogTrigger,
  EmptyState,
  Field,
  Input,
  Page,
  PageBody,
  PageHeader,
  QueryBoundary,
  Section,
  Select,
  SettingsRow,
  Textarea,
  cn,
  invalidate,
  useMutation,
  useQuery,
  type Member,
  type RoutingStrategy,
  type Team,
} from "@hubchat/shared";
import { Plus, Trash2, Users } from "lucide-react";
import { useState } from "react";
import { useWorkspace } from "../../app/workspace-context";

const ROUTING_OPTIONS: { value: RoutingStrategy; label: string; description: string }[] = [
  { value: "manual", label: "Manual", description: "Sits in the team queue until someone claims it." },
  { value: "round_robin", label: "Round robin", description: "Even distribution, in order." },
  { value: "least_active", label: "Least active", description: "To whoever holds the fewest open threads." },
  { value: "team_queue", label: "Team queue", description: "Shared queue, no individual assignment." },
  { value: "customer_owner", label: "Customer owner", description: "To the account owner when there is one." },
  { value: "weighted", label: "Weighted", description: "Deterministic weights per member." },
];

/** Teams and routing (§6.12). */
export default function Teams() {
  const [activeId, setActiveId] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const teams = useQuery<{ data: Team[] }>(["teams"], (signal) => api.get("/teams", { signal }));
  const members = useQuery<{ data: Member[] }>(["members"], (signal) => api.get("/members", { signal }));

  const list = teams.data?.data ?? [];
  const active = list.find((team) => team.id === activeId) ?? list[0] ?? null;

  return (
    <Page>
      <PageHeader
        title="Teams"
        description="Groups that share a routing strategy and can be assigned work as a unit."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />} onClick={() => setCreating(true)}>
            New team
          </Button>
        }
      />

      <PageBody>
        <QueryBoundary query={teams}>
          {() =>
            list.length === 0 ? (
              <EmptyState icon={Users} title="No teams yet" description="Create one to start routing work." />
            ) : (
              <div className="grid gap-5 lg:grid-cols-[240px_minmax(0,1fr)]">
                <nav aria-label="Teams" className="space-y-1">
                  {list.map((team) => (
                    <button
                      key={team.id}
                      type="button"
                      onClick={() => setActiveId(team.id)}
                      className={cn(
                        "flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm transition-colors",
                        team.id === active?.id
                          ? "bg-accent-subtle font-medium text-fg"
                          : "text-fg-secondary hover:bg-fill hover:text-fg",
                      )}
                    >
                      <Users aria-hidden="true" className="size-3.5 shrink-0 text-fg-muted" />
                      <span className="min-w-0 flex-1 truncate">{team.name}</span>
                      <span className="text-2xs tabular text-fg-muted">{team.member_ids.length}</span>
                    </button>
                  ))}
                </nav>

                <div className="min-w-0">
                  {active ? (
                    <TeamDetail team={active} members={members.data?.data ?? []} onDeleted={() => setActiveId(null)} />
                  ) : null}
                </div>
              </div>
            )
          }
        </QueryBoundary>
      </PageBody>

      {creating ? (
        <CreateTeamDialog
          onClose={() => setCreating(false)}
          onCreated={(team) => {
            setActiveId(team.id);
            setCreating(false);
          }}
        />
      ) : null}
    </Page>
  );
}

function CreateTeamDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (team: Team) => void }) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");

  const create = useMutation<{ name: string; description: string }, Team>(
    ({ name: n, description: d }) =>
      api.post<Team>("/teams", { name: n, description: d || null, routing_strategy: "manual", member_ids: [] }),
    {
      onSuccess: (team) => {
        invalidate(["teams"]);
        onCreated(team);
      },
    },
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title="New team"
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
              loading={create.isPending}
              disabled={!name.trim()}
              onClick={() => void create.mutate({ name: name.trim(), description: description.trim() }).catch(() => {})}
            >
              Create team
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-4">
          {create.error ? (
            <Callout tone="danger">
              {create.error instanceof ApiError ? create.error.message : "Could not create that team."}
            </Callout>
          ) : null}
          <Field label="Name" htmlFor="team-name">
            <Input id="team-name" inputSize="sm" value={name} onChange={(event) => setName(event.target.value)} autoFocus />
          </Field>
          <Field label="Description" htmlFor="team-description">
            <Textarea id="team-description" rows={2} value={description} onChange={(event) => setDescription(event.target.value)} />
          </Field>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function TeamDetail({ team, members, onDeleted }: { team: Team; members: Member[]; onDeleted: () => void }) {
  const { memberById } = useWorkspace();
  const [name, setName] = useState(team.name);
  const [description, setDescription] = useState(team.description ?? "");
  const [leadId, setLeadId] = useState(team.lead_id ?? "");
  const [routing, setRouting] = useState<RoutingStrategy>(team.routing_strategy);
  const [addingMember, setAddingMember] = useState(false);
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  const save = useMutation<{ name: string; description: string | null; lead_id: string | null; routing_strategy: RoutingStrategy }, unknown>(
    (body) => api.patch(`/teams/${team.id}`, body),
    { invalidates: [["teams"]] },
  );
  const remove = useMutation<void, unknown>(() => api.delete(`/teams/${team.id}`), {
    invalidates: [["teams"]],
    onSuccess: onDeleted,
  });
  const removeMember = useMutation<string, unknown>(
    (memberId) => api.delete(`/teams/${team.id}/members/${memberId}`),
    { invalidates: [["teams"]] },
  );

  const dirty =
    name.trim() !== team.name ||
    description.trim() !== (team.description ?? "") ||
    leadId !== (team.lead_id ?? "") ||
    routing !== team.routing_strategy;

  return (
    <>
      <Section
        title={team.name}
        description={team.description ?? undefined}
        actions={
          <Button
            variant="danger-ghost"
            size="sm"
            leading={<Trash2 />}
            onClick={() => setConfirmingDelete(true)}
          >
            Delete team
          </Button>
        }
      >
        <Card>
          <CardBody className="space-y-3 pt-0">
            {save.error ? (
              <Callout tone="danger">
                {save.error instanceof ApiError ? save.error.message : "Could not save this team."}
              </Callout>
            ) : null}
            {save.isSuccess ? <Callout tone="success">Saved.</Callout> : null}

            <SettingsRow label="Name" htmlFor="edit-team-name">
              <Input id="edit-team-name" inputSize="sm" value={name} onChange={(event) => setName(event.target.value)} />
            </SettingsRow>

            <SettingsRow label="Description">
              <Textarea rows={2} value={description} onChange={(event) => setDescription(event.target.value)} />
            </SettingsRow>

            <SettingsRow label="Team lead" description="Escalation target and default approver.">
              <Select
                size="sm"
                value={leadId || undefined}
                onValueChange={setLeadId}
                aria-label="Team lead"
                options={team.member_ids.map((id) => ({ value: id, label: memberById(id)?.name ?? id }))}
              />
            </SettingsRow>

            <SettingsRow
              label="Routing strategy"
              description="How work is distributed when a rule assigns to this team rather than a person."
            >
              <Select
                size="sm"
                value={routing}
                onValueChange={(value) => setRouting(value as RoutingStrategy)}
                aria-label="Routing strategy"
                options={ROUTING_OPTIONS}
              />
            </SettingsRow>

            <div className="flex justify-end pt-1">
              <Button
                variant="primary"
                size="sm"
                disabled={!dirty}
                loading={save.isPending}
                onClick={() =>
                  void save.mutate({
                    name: name.trim(),
                    description: description.trim() || null,
                    lead_id: leadId || null,
                    routing_strategy: routing,
                  })
                }
              >
                Save changes
              </Button>
            </div>
          </CardBody>
        </Card>
      </Section>

      <Section
        title="Members"
        actions={
          <Button variant="secondary" size="sm" leading={<Plus />} onClick={() => setAddingMember(true)}>
            Add member
          </Button>
        }
      >
        <Card>
          <CardBody className="p-0">
            {removeMember.error ? (
              <Callout tone="danger" className="m-3">
                {removeMember.error instanceof ApiError ? removeMember.error.message : "Could not remove that member."}
              </Callout>
            ) : null}
            {team.member_ids.length === 0 ? (
              <EmptyState icon={Users} size="sm" title="No members" />
            ) : (
              <ul className="divide-y divide-line-subtle">
                {team.member_ids.map((id) => {
                  const member = memberById(id);
                  if (!member) return null;
                  return (
                    <li key={id} className="flex items-center gap-3 px-4 py-2.5">
                      <AvatarGroup size="sm" people={[{ id: member.id, name: member.name }]} />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm text-fg">{member.name}</span>
                        <span className="block truncate text-xs text-fg-muted">{member.email}</span>
                      </span>
                      {member.id === team.lead_id && <Badge tone="accent">Lead</Badge>}
                      <Button
                        variant="ghost"
                        size="xs"
                        iconOnly
                        aria-label={`Remove ${member.name}`}
                        leading={<Trash2 />}
                        onClick={() => void removeMember.mutate(id).catch(() => {})}
                      />
                    </li>
                  );
                })}
              </ul>
            )}
          </CardBody>
        </Card>
      </Section>

      {addingMember ? (
        <AddMemberDialog
          team={team}
          candidates={members.filter((member) => !team.member_ids.includes(member.id))}
          onClose={() => setAddingMember(false)}
        />
      ) : null}

      {confirmingDelete ? (
        <Dialog open onOpenChange={(open) => !open && setConfirmingDelete(false)}>
          <DialogContent
            title={`Delete ${team.name}?`}
            footer={
              <>
                <DialogClose asChild>
                  <Button variant="ghost" size="sm">
                    Cancel
                  </Button>
                </DialogClose>
                <Button
                  variant="danger"
                  size="sm"
                  loading={remove.isPending}
                  onClick={() => void remove.mutate().catch(() => {})}
                >
                  Delete team
                </Button>
              </>
            }
          >
            <p className="text-sm text-fg-muted">
              Conversations and inboxes assigned to this team are unassigned rather than deleted. This cannot be
              undone.
            </p>
            {remove.error ? (
              <Callout tone="danger" className="mt-3">
                {remove.error instanceof ApiError ? remove.error.message : "Could not delete this team."}
              </Callout>
            ) : null}
          </DialogContent>
        </Dialog>
      ) : null}
    </>
  );
}

function AddMemberDialog({ team, candidates, onClose }: { team: Team; candidates: Member[]; onClose: () => void }) {
  const [memberId, setMemberId] = useState(candidates[0]?.id ?? "");

  const add = useMutation<string, unknown>((id) => api.put(`/teams/${team.id}/members/${id}`), {
    invalidates: [["teams"]],
    onSuccess: onClose,
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={`Add a member to ${team.name}`}
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
              loading={add.isPending}
              disabled={!memberId}
              onClick={() => void add.mutate(memberId).catch(() => {})}
            >
              Add
            </Button>
          </>
        }
      >
        {add.error ? (
          <Callout tone="danger" className="mb-3">
            {add.error instanceof ApiError ? add.error.message : "Could not add that member."}
          </Callout>
        ) : null}
        {candidates.length === 0 ? (
          <EmptyState size="sm" title="Everyone is already on this team" />
        ) : (
          <Field label="Member">
            <Select
              aria-label="Member"
              value={memberId}
              onValueChange={setMemberId}
              options={candidates.map((member) => ({ value: member.id, label: `${member.name} (${member.email})` }))}
            />
          </Field>
        )}
      </DialogContent>
    </Dialog>
  );
}
