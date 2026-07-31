import {
  ApiError,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  Checkbox,
  ConfirmDialog,
  Dialog,
  DialogContent,
  DialogTrigger,
  EmptyState,
  Field,
  Input,
  Page,
  PageBody,
  PageHeader,
  Section,
  useMutation,
  useQuery,
  api,
  idempotencyKey,
} from "@hubchat/shared";
import { Inbox, Plus, Save, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import type { Inbox as InboxRecord, Team } from "@hubchat/shared";
import { useWorkspace } from "../../app/workspace-context";

type InboxPayload = {
  name: string;
  slug: string;
  description: string | null;
  channels: string[];
  team_ids: string[];
};

const CHANNELS = ["widget", "portal", "email", "form", "api", "manual"] as const;

function errorMessage(error: unknown, fallback: string) {
  return error instanceof ApiError ? error.message : fallback;
}

/** Live inbox configuration (§6.1, §6.12). */
export default function InboxSettings() {
  const { teams } = useWorkspace();
  const query = useQuery<{ data: InboxRecord[] }>(["inboxes"], (signal) => api.get("/inboxes", { signal }));
  const inboxes = query.data?.data ?? [];
  const [activeId, setActiveId] = useState<string | null>(null);
  const active = inboxes.find((inbox) => inbox.id === activeId) ?? inboxes[0];
  const [draft, setDraft] = useState<InboxRecord | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [newName, setNewName] = useState("");
  const [newSlug, setNewSlug] = useState("");
  const [deleting, setDeleting] = useState<InboxRecord | null>(null);

  useEffect(() => {
    if (!activeId && active) setActiveId(active.id);
  }, [active, activeId]);
  useEffect(() => {
    setDraft(active ? { ...active, channels: [...active.channels], team_ids: [...active.team_ids] } : null);
  }, [active]);

  const create = useMutation<InboxPayload, InboxRecord>(
    (input) => api.post("/inboxes", input, { idempotencyKey: idempotencyKey() }),
    {
      invalidates: [["inboxes"], ["bootstrap"]],
      onSuccess: (value) => {
        setCreateOpen(false);
        setNewName("");
        setNewSlug("");
        setActiveId(value.id);
      },
    },
  );
  const update = useMutation<InboxPayload, InboxRecord>(
    (input) => api.patch(`/inboxes/${encodeURIComponent(active?.id ?? "")}`, input, { idempotencyKey: idempotencyKey() }),
    { invalidates: [["inboxes"], ["bootstrap"]], onSuccess: (value) => setDraft(value) },
  );
  const setDefault = useMutation<void, void>(
    () => api.put(`/inboxes/${encodeURIComponent(active?.id ?? "")}/default`, undefined, { idempotencyKey: idempotencyKey() }),
    { invalidates: [["inboxes"], ["bootstrap"]] },
  );
  const remove = useMutation<string, void>(
    (id) => api.delete(`/inboxes/${encodeURIComponent(id)}`, { idempotencyKey: idempotencyKey() }),
    { invalidates: [["inboxes"], ["bootstrap"]], onSuccess: () => setDeleting(null) },
  );

  const updateDraft = (updater: (current: InboxRecord) => InboxRecord) => setDraft((current) => (current ? updater(current) : current));
  const toggleChannel = (value: InboxRecord["channels"][number]) => {
    updateDraft((current) => ({ ...current, channels: current.channels.includes(value) ? current.channels.filter((item) => item !== value) : [...current.channels, value] }));
  };
  const toggleTeam = (value: string) => {
    updateDraft((current) => ({ ...current, team_ids: current.team_ids.includes(value) ? current.team_ids.filter((item) => item !== value) : [...current.team_ids, value] }));
  };
  const save = () => {
    if (!draft) return;
    void update.mutate({ name: draft.name.trim(), slug: draft.slug, description: draft.description?.trim() || null, channels: draft.channels, team_ids: draft.team_ids }).catch(() => {});
  };

  return (
    <Page>
      <PageHeader
        title="Inboxes"
        description="Destinations for conversations and tickets. Split by product, department, or brand."
        actions={
          <Dialog open={createOpen} onOpenChange={setCreateOpen}>
            <DialogTrigger asChild><Button variant="primary" size="sm" leading={<Plus />}>New inbox</Button></DialogTrigger>
            <DialogContent title="Create inbox" footer={<><Button variant="ghost" size="sm" onClick={() => setCreateOpen(false)}>Cancel</Button><Button variant="primary" size="sm" loading={create.isPending} disabled={!newName.trim() || !newSlug.trim()} onClick={() => void create.mutate({ name: newName.trim(), slug: newSlug.trim().toLowerCase(), description: null, channels: ["manual"], team_ids: [] }).catch(() => {})}>Create inbox</Button></>}>
              <div className="space-y-4">
                <Field label="Name"><Input autoFocus value={newName} onChange={(event) => setNewName(event.target.value)} placeholder="Customer support" /></Field>
                <Field label="Slug" description="Lowercase letters, numbers, and hyphens."><Input mono value={newSlug} onChange={(event) => setNewSlug(event.target.value.replace(/[^a-zA-Z0-9-]/g, "").toLowerCase())} placeholder="support" /></Field>
                {Boolean(create.error) && <p className="text-sm text-danger">{errorMessage(create.error, "Could not create this inbox.")}</p>}
              </div>
            </DialogContent>
          </Dialog>
        }
      />
      <PageBody>
        {query.isLoading ? <p className="text-sm text-fg-muted">Loading inboxes…</p> : query.error ? <EmptyState icon={Inbox} title="Inboxes unavailable" description={errorMessage(query.error, "Could not load inboxes.")} action={<Button variant="secondary" onClick={query.refetch}>Try again</Button>} /> : !active || !draft ? <EmptyState icon={Inbox} title="No inboxes configured" description="Create an inbox to receive conversations and tickets." /> : (
          <div className="grid gap-5 lg:grid-cols-[240px_minmax(0,1fr)]">
            <nav aria-label="Inboxes" className="space-y-1">{inboxes.map((inbox) => <button key={inbox.id} type="button" onClick={() => setActiveId(inbox.id)} className={`flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm ${inbox.id === active.id ? "bg-accent-subtle font-medium text-fg" : "text-fg-secondary hover:bg-fill hover:text-fg"}`}><Inbox aria-hidden="true" className="size-3.5 shrink-0 text-fg-muted" /><span className="min-w-0 flex-1 truncate">{inbox.name}</span>{inbox.is_default && <Badge tone="neutral">Default</Badge>}</button>)}</nav>
            <div className="min-w-0">
              <div className="mb-5 flex flex-wrap items-end gap-3"><Field label="Name" className="min-w-56 flex-1"><Input value={draft.name} onChange={(event) => updateDraft((current) => ({ ...current, name: event.target.value }))} /></Field><Field label="Slug" description="Read-only after creation."><Input mono value={draft.slug} readOnly /></Field><Button variant="primary" size="sm" leading={<Save />} loading={update.isPending} onClick={save}>Save changes</Button></div>
              {Boolean(update.error) && <p className="mb-4 text-sm text-danger">{errorMessage(update.error, "Could not save this inbox.")}</p>}
              <Section title="Details"><Card><CardBody className="pt-0"><div className="border-b border-line-subtle py-3"><Field label="Description"><Input value={draft.description ?? ""} onChange={(event) => updateDraft((current) => ({ ...current, description: event.target.value }))} placeholder="Where customer conversations are handled." /></Field></div><div className="flex items-center justify-between gap-4 py-3"><div><p className="text-sm text-fg">Default inbox</p><p className="mt-0.5 text-xs text-fg-muted">New conversations without an explicit destination use this inbox.</p></div><Button variant={draft.is_default ? "secondary" : "ghost"} size="sm" disabled={draft.is_default || setDefault.isPending} onClick={() => void setDefault.mutate(undefined).catch(() => {})}>{draft.is_default ? "Default" : "Make default"}</Button></div></CardBody></Card></Section>
              <Section title="Channels" description="Only selected sources can deliver into this inbox."><Card><CardBody className="grid gap-3 sm:grid-cols-2">{CHANNELS.map((channel) => <label key={channel} className="flex items-center gap-2 text-sm capitalize"><Checkbox checked={draft.channels.includes(channel)} onCheckedChange={() => toggleChannel(channel)} />{channel}</label>)}</CardBody></Card></Section>
              <Section title="Team access" description="Members outside these teams cannot see this inbox. The server applies this scope to queries."><Card><CardBody className="grid gap-3 sm:grid-cols-2">{teams.length ? teams.map((team: Team) => <label key={team.id} className="flex items-center gap-2 text-sm"><Checkbox checked={draft.team_ids.includes(team.id)} onCheckedChange={() => toggleTeam(team.id)} />{team.name}</label>) : <p className="text-sm text-fg-muted">No teams configured.</p>}</CardBody></Card></Section>
              <Section title="Routing"><Callout tone="info">Inbox-level assignment strategy is not configured by this API. Use automation rules, team queues, and manual assignment for deterministic routing.</Callout></Section>
              <Section title="SLA policy" description="Attach a policy from the SLA settings area. The current inbox API exposes the active policy as read-only here."><Card><CardBody><span className="text-sm text-fg-secondary">{draft.sla_policy_id ?? "Not configured"}</span></CardBody></Card></Section>
              <Section title="Danger zone"><Card className="border-danger-border"><CardHeader title="Delete this inbox" description={`${draft.open_count} open conversations would need to be moved first. Deleting an inbox does not delete its history.`} actions={<Button variant="danger" size="sm" leading={<Trash2 />} disabled={draft.is_default} onClick={() => setDeleting(draft)}>Delete</Button>} /></Card></Section>
            </div>
          </div>
        )}
      </PageBody>
      <ConfirmDialog open={deleting !== null} onOpenChange={(open) => !open && setDeleting(null)} title="Delete this inbox?" description={deleting ? `Delete ${deleting.name}? Open conversations must be moved first.` : ""} confirmLabel="Delete inbox" destructive loading={remove.isPending} onConfirm={() => { if (deleting) void remove.mutate(deleting.id).catch(() => {}); }} />
    </Page>
  );
}
