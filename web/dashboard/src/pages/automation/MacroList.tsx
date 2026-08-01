import {
  ApiError,
  Badge,
  Button,
  Card,
  CardBody,
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
  Pagination,
  SearchInput,
  Section,
  Select,
  Textarea,
  Toolbar,
  api,
  idempotencyKey,
  useAllPages,
  useInfinite,
  useMutation,
  useToast,
  type AutomationAction,
  type AutomationActionType,
  type Macro,
  type Paginated,
} from "@hubchat/shared";
import { ArrowDown, ArrowUp, ListChecks, Plus, Trash2, X, Zap } from "lucide-react";
import { useState } from "react";
import { ActionParams, ACTION_TYPES, actionLabel, type ActionDirectory } from "./AutomationActionEditor";
import { useWorkspace } from "../../app/workspace-context";

type MacroInput = {
  name: string;
  folder: string;
  scope: "personal" | "team" | "workspace";
  team_id: string;
  body: string;
  actions: AutomationAction[];
};
type MacroRecord = Macro & { team_id?: string | null };

const EMPTY_DRAFT: MacroInput = {
  name: "",
  folder: "",
  scope: "workspace",
  team_id: "",
  body: "",
  actions: [],
};

export default function MacroList() {
  const { workspace, members, teams, inboxes, tags } = useWorkspace();
  const toast = useToast();
  const [queryText, setQueryText] = useState("");
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Macro | null>(null);
  const [draft, setDraft] = useState<MacroInput>(EMPTY_DRAFT);
  const [deleteTarget, setDeleteTarget] = useState<Macro | null>(null);

  const macros = useInfinite<MacroRecord>(
    ["automation-macros", workspace.id, queryText],
    (cursor, signal) => api.get<Paginated<MacroRecord>>(`/automation/macros?q=${encodeURIComponent(queryText)}&limit=25${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal, workspaceId: workspace.id }),
  );
  const webhooks = useAllPages<{ id: string; url: string; enabled: boolean }>(
    ["webhooks", "macro-lookup", workspace.id],
    (cursor, signal) => api.get<Paginated<{ id: string; url: string; enabled: boolean }>>(`/webhooks?limit=200${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal, workspaceId: workspace.id }),
  );

  const save = useMutation<MacroInput, Macro>(
    (input) => editing
      ? api.patch<Macro>(`/automation/macros/${encodeURIComponent(editing.id)}`, input, { workspaceId: workspace.id, idempotencyKey: idempotencyKey() })
      : api.post<Macro>("/automation/macros", input, { workspaceId: workspace.id, idempotencyKey: idempotencyKey() }),
    {
      invalidates: [["automation-macros", workspace.id]],
      onSuccess: () => {
        setOpen(false);
        setEditing(null);
        setDraft(EMPTY_DRAFT);
        toast.toast({ title: editing ? "Macro updated" : "Macro created", description: "The deterministic action bundle is ready to use." });
      },
    },
  );
  const remove = useMutation<{ id: string }, void>(
    ({ id }) => api.delete(`/automation/macros/${encodeURIComponent(id)}`, { workspaceId: workspace.id, idempotencyKey: idempotencyKey() }),
    {
      invalidates: [["automation-macros", workspace.id]],
      onSuccess: () => {
        setDeleteTarget(null);
        toast.toast({ title: "Macro deleted", description: "It is no longer available in the composer." });
      },
    },
  );

  const directory: ActionDirectory = { members, teams, inboxes, tags, webhooks: webhooks.items };
  const startCreate = () => {
    setEditing(null);
    setDraft(EMPTY_DRAFT);
    setOpen(true);
  };
  const startEdit = (macro: MacroRecord) => {
    setEditing(macro);
    setDraft({
      name: macro.name,
      folder: macro.folder ?? "",
      scope: macro.scope,
      team_id: macro.team_id ?? "",
      body: macro.body ?? "",
      actions: macro.actions.map((action) => ({ ...action, params: { ...action.params } })),
    });
    setOpen(true);
  };
  const addAction = (type: AutomationActionType) => {
    setDraft((current) => ({
      ...current,
      actions: [...current.actions, { id: `macro_action_${Date.now().toString(36)}_${current.actions.length}`, type, params: {} }],
    }));
  };
  const updateAction = (index: number, action: AutomationAction) => setDraft((current) => ({ ...current, actions: current.actions.map((item, itemIndex) => itemIndex === index ? action : item) }));
  const moveAction = (index: number, direction: -1 | 1) => setDraft((current) => {
    const nextIndex = index + direction;
    if (nextIndex < 0 || nextIndex >= current.actions.length) return current;
    const actions = [...current.actions];
    const currentAction = actions[index];
    const nextAction = actions[nextIndex];
    if (!currentAction || !nextAction) return current;
    actions[index] = nextAction;
    actions[nextIndex] = currentAction;
    return { ...current, actions };
  });

  return (
    <Page>
      <PageHeader
        title="Macros"
        description="Bundles of actions an agent applies in one keystroke. Unlike rules, a macro only runs when a human triggers it."
        actions={<Dialog open={open} onOpenChange={setOpen}><DialogTrigger asChild><Button variant="primary" size="sm" leading={<Plus />} onClick={startCreate}>New macro</Button></DialogTrigger><DialogContent title={editing ? "Edit macro" : "Create macro"} description="Configure the reply and deterministic state changes. Execution still checks the acting member's capabilities." size="xl" footer={<><Button variant="ghost" size="sm" onClick={() => setOpen(false)}>Cancel</Button><Button variant="primary" size="sm" loading={save.isPending} disabled={!draft.name.trim() || (draft.scope === "team" && !draft.team_id)} onClick={() => void save.mutate({ ...draft, name: draft.name.trim(), folder: draft.folder.trim(), body: draft.body.trim() }).catch(() => {})}>{editing ? "Save changes" : "Create macro"}</Button></>}>
          <div className="space-y-5 pb-2">
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="Name" required><Input autoFocus value={draft.name} onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))} placeholder="Close and thank customer" /></Field>
              <Field label="Folder" description="Optional grouping used by search and the composer."><Input value={draft.folder} onChange={(event) => setDraft((current) => ({ ...current, folder: event.target.value }))} placeholder="Resolution" /></Field>
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="Scope"><Select value={draft.scope} onValueChange={(scope) => setDraft((current) => ({ ...current, scope: scope as MacroInput["scope"], team_id: scope === "team" ? current.team_id : "" }))} options={[{ value: "workspace", label: "Workspace" }, { value: "team", label: "Team" }, { value: "personal", label: "Personal" }]} /></Field>
              {draft.scope === "team" ? <Field label="Team" required><Select value={draft.team_id} onValueChange={(team_id) => setDraft((current) => ({ ...current, team_id }))} placeholder="Choose a team" options={teams.map((team) => ({ value: team.id, label: team.name }))} /></Field> : <div className="rounded-md border border-line-subtle bg-inset px-3 py-2 text-xs leading-normal text-fg-muted">Personal macros are owned by you. Workspace macros are visible to every member who can use automation.</div>}
            </div>
            <Field label="Reply text" description="Inserted as a customer reply before other actions. Leave blank for state-only macros."><Textarea rows={4} value={draft.body} onChange={(event) => setDraft((current) => ({ ...current, body: event.target.value }))} placeholder="Thanks for reaching out — this is now resolved." /></Field>
            <section className="space-y-3" aria-labelledby="macro-actions-heading">
              <div><h3 id="macro-actions-heading" className="text-sm font-medium text-fg">Actions</h3><p className="mt-1 text-xs text-fg-muted">Actions run top to bottom. All required capabilities are checked before the first mutation.</p></div>
              {draft.actions.length === 0 ? <div className="rounded-md border border-dashed border-line-strong px-3 py-4 text-center text-xs text-fg-muted">No state changes yet. Add an action below or use reply text only.</div> : <div className="space-y-2">{draft.actions.map((action, index) => <Card key={action.id}><CardBody className="space-y-2"><div className="flex items-center gap-2"><span className="flex size-5 items-center justify-center rounded-full bg-accent-subtle text-2xs font-semibold text-accent-text">{index + 1}</span><span className="min-w-0 flex-1 text-xs font-medium text-fg">{actionLabel(action.type)}</span><Button variant="ghost" size="sm" iconOnly aria-label="Move action up" disabled={index === 0} leading={<ArrowUp />} onClick={() => moveAction(index, -1)} /><Button variant="ghost" size="sm" iconOnly aria-label="Move action down" disabled={index === draft.actions.length - 1} leading={<ArrowDown />} onClick={() => moveAction(index, 1)} /><Button variant="ghost" size="sm" iconOnly aria-label={`Remove ${actionLabel(action.type)}`} leading={<X />} onClick={() => setDraft((current) => ({ ...current, actions: current.actions.filter((_, itemIndex) => itemIndex !== index) }))} /></div><ActionParams {...directory} action={action} onChange={(params) => updateAction(index, { ...action, params })} /></CardBody></Card>)}</div>}
              <Select key={draft.actions.length} placeholder="Add an action…" options={ACTION_TYPES.map((item) => ({ value: item.value, label: item.label, group: item.group }))} onValueChange={(type) => addAction(type as AutomationActionType)} />
            </section>
            {save.error !== undefined && <p className="text-sm text-danger">{save.error instanceof ApiError ? save.error.message : "Could not save this macro."}</p>}
          </div>
        </DialogContent></Dialog>}
      />
      <Toolbar leading={<div className="w-64"><SearchInput inputSize="sm" value={queryText} onChange={(event) => setQueryText(event.target.value)} onClear={() => setQueryText("")} placeholder="Search macros" /></div>} />
      <PageBody><Section>
        {macros.isLoading ? <p className="text-sm text-fg-muted">Loading macros…</p> : macros.error ? <EmptyState icon={ListChecks} title="Macros unavailable" description={macros.error instanceof ApiError ? macros.error.message : "Try again in a moment."} action={<Button variant="secondary" size="sm" onClick={macros.refetch}>Try again</Button>} /> : macros.items.length === 0 ? <EmptyState icon={ListChecks} title="No macros" description="Create a repeatable bundle of text and actions." action={<Button variant="secondary" size="sm" onClick={startCreate}>Create your first macro</Button>} /> : <div className="space-y-3">{macros.items.map((macro) => <Card key={macro.id}><CardBody><div className="flex flex-wrap items-start gap-4"><div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><button type="button" className="truncate text-left text-sm font-medium text-fg hover:underline" onClick={() => startEdit(macro)}>{macro.name}</button><Badge tone="neutral">{macro.scope}</Badge>{macro.folder && <Badge tone="neutral" variant="outline">{macro.folder}</Badge>}</div>{macro.body && <p className="mt-1.5 line-clamp-2 rounded-md border-l-2 border-line-strong bg-inset px-2.5 py-1.5 text-xs leading-normal text-fg-secondary">{macro.body}</p>}<div className="mt-2 flex flex-wrap items-center gap-1.5">{macro.actions.map((action) => <span key={action.id} className="inline-flex items-center gap-1 rounded-sm bg-fill px-1.5 py-0.5 text-2xs text-fg-secondary"><Zap className="size-2.5 text-accent-text" />{actionLabel(action.type)}</span>)}</div><p className="mt-2 text-2xs tabular text-fg-disabled">Used {macro.usage_count}×</p></div><div className="flex shrink-0 items-center gap-1"><Button variant="ghost" size="sm" onClick={() => startEdit(macro)}>Edit</Button><Button variant="ghost" size="sm" iconOnly aria-label={`Delete ${macro.name}`} leading={<Trash2 />} onClick={() => setDeleteTarget(macro)} /></div></div></CardBody></Card>)}</div>}
        <Pagination hasPrevious={false} hasNext={macros.hasMore} onPrevious={() => undefined} onNext={() => void macros.fetchNext()} summary={`${macros.items.length} macro${macros.items.length === 1 ? "" : "s"} loaded`} />
      </Section></PageBody>
      <ConfirmDialog open={deleteTarget !== null} onOpenChange={(next) => { if (!next) setDeleteTarget(null); }} title="Delete macro" description={`Delete “${deleteTarget?.name ?? "this macro"}”. Agents will no longer see it in the composer, and its usage history will be removed.`} confirmLabel="Delete macro" destructive loading={remove.isPending} onConfirm={() => { if (deleteTarget) void remove.mutate({ id: deleteTarget.id }).catch(() => {}); }} />
    </Page>
  );
}
