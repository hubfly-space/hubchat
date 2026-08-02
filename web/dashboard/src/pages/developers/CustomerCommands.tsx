import {
  api,
  Button,
  Card,
  CardBody,
  Dialog,
  DialogContent,
  EmptyState,
  Field,
  Input,
  Page,
  PageBody,
  PageHeader,
  Pagination,
  Switch,
  Textarea,
  useMutation,
  useInfinite,
  type Paginated,
} from "@hubchat/shared";
import { Braces, Pencil, Plus } from "lucide-react";
import { useState } from "react";

type Binding = {
  id: string;
  name: string;
  description: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
};

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "The command binding could not be saved.";
}

export default function CustomerCommands() {
  const query = useInfinite<Binding>(["customer-command-bindings"], (cursor, signal) => api.get<Paginated<Binding>>(`/customer-command-bindings?limit=25${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }));
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [editing, setEditing] = useState<Binding | null>(null);

  const create = useMutation<{ name: string; description: string }, Binding>(
    (input) => api.post<Binding>("/customer-command-bindings", input),
    { invalidates: [["customer-command-bindings"]] },
  );
  const update = useMutation<{ id: string; name: string; description: string; enabled: boolean }, Binding>(
    ({ id, ...input }) => api.patch<Binding>(`/customer-command-bindings/${encodeURIComponent(id)}`, input),
    { invalidates: [["customer-command-bindings"]], onSuccess: () => setEditing(null) },
  );

  const openEdit = (binding: Binding) => {
    setEditing(binding);
    setName(binding.name);
    setDescription(binding.description);
  };

  const resetForm = () => {
    setName("");
    setDescription("");
    setEditing(null);
  };

  return (
    <Page>
      <PageHeader
        title="Customer commands"
        description="Safe, named browser actions that agents can invoke during a conversation."
        actions={<Button leading={<Plus />} onClick={() => { setName(""); setDescription(""); setEditing({ id: "", name: "", description: "", enabled: true, created_at: "", updated_at: "" }); }}>New binding</Button>}
      />
      <PageBody className="space-y-5">
        <Card>
          <CardBody className="space-y-3">
            <div className="flex items-center gap-2 text-sm font-medium"><Braces className="size-4 text-fg-muted" />Host integration contract</div>
            <p className="text-xs text-fg-muted">The website registers the same name with <code>Hubchat('bind', …)</code>. Hubchat never evaluates scripts or sends arbitrary JavaScript. Disable a binding before changing the host implementation.</p>
            <code className="block rounded-md bg-inset p-3 text-xs text-fg-secondary">Hubchat("bind", &#123; name: "reload_page", handler: () =&gt; window.location.reload() &#125;)</code>
          </CardBody>
        </Card>

        <Card>
          <CardBody className="p-0">
            {query.isLoading ? <p className="p-4 text-sm text-fg-muted">Loading bindings…</p> : query.error ? <EmptyState icon={Braces} title="Bindings unavailable" description="Could not load customer commands." action={<Button variant="secondary" onClick={query.refetch}>Try again</Button>} /> : query.items.length ? (
              <ul className="divide-y divide-line-subtle">
                {query.items.map((item) => <li key={item.id} className="flex items-center gap-3 px-4 py-3">
                  <div className="min-w-0 flex-1"><div className="flex items-center gap-2"><code className="text-sm text-fg">{item.name}</code><span className="text-2xs text-fg-muted">{item.enabled ? "Enabled" : "Disabled"}</span></div><p className="mt-1 truncate text-xs text-fg-muted">{item.description || "No description"}</p></div>
                  <Switch checked={item.enabled} onCheckedChange={(enabled) => void update.mutate({ id: item.id, name: item.name, description: item.description, enabled }).catch(() => {})} aria-label={`${item.enabled ? "Disable" : "Enable"} ${item.name}`} />
                  <Button variant="ghost" size="sm" iconOnly leading={<Pencil />} aria-label={`Edit ${item.name}`} onClick={() => openEdit(item)} />
                </li>)}
              </ul>
            ) : <EmptyState icon={Braces} title="No command bindings" description="Create one for a safe customer-side action." />}
            {query.hasMore && <Pagination hasPrevious={false} hasNext onPrevious={() => undefined} onNext={() => void query.fetchNext()} summary={`${query.items.length} bindings loaded`} />}
          </CardBody>
        </Card>
      </PageBody>

      <Dialog open={Boolean(editing)} onOpenChange={(open) => { if (!open) resetForm(); }}>
        <DialogContent title={editing?.id ? "Edit command binding" : "Create command binding"} footer={<><Button variant="ghost" onClick={resetForm}>Cancel</Button><Button loading={create.isPending || update.isPending} disabled={!name.trim()} onClick={() => { if (editing?.id) void update.mutate({ id: editing.id, name: name.trim(), description: description.trim(), enabled: editing.enabled }).catch(() => {}); else void create.mutate({ name: name.trim(), description: description.trim() }).then(resetForm).catch(() => {}); }}>{editing?.id ? "Save changes" : "Create binding"}</Button></>}>
          <div className="space-y-4"><Field label="Name" description="Letters, numbers, hyphens, and underscores; this is the host-side event name."><Input autoFocus value={name} onChange={(event) => setName(event.target.value)} placeholder="reload_page" disabled={Boolean(editing?.id)} /></Field><Field label="Description"><Textarea value={description} onChange={(event) => setDescription(event.target.value)} placeholder="Reload the customer page for diagnostics" rows={3} /></Field>{(create.error !== undefined || update.error !== undefined) && <p className="text-xs text-danger">{errorMessage(create.error ?? update.error)}</p>}</div>
        </DialogContent>
      </Dialog>
    </Page>
  );
}
