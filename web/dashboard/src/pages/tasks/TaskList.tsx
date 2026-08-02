import {
  ApiError,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
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
  Select,
  Textarea,
  api,
  formatDate,
  idempotencyKey,
  useInfinite,
  useMutation,
  type Paginated,
  type Task,
  type TaskState,
} from "@hubchat/shared";
import { CheckSquare, Circle, Plus, RotateCcw, X } from "lucide-react";
import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";

const stateOptions = [
  { value: "", label: "All states" },
  { value: "open", label: "Open" },
  { value: "completed", label: "Completed" },
  { value: "cancelled", label: "Cancelled" },
];

function stateTone(state: TaskState) {
  if (state === "completed") return "success" as const;
  if (state === "cancelled") return "danger" as const;
  return "accent" as const;
}

export default function TaskList() {
  const [searchParams, setSearchParams] = useSearchParams();
  const { members } = useWorkspace();
  const queryText = searchParams.get("q") ?? "";
  const state = searchParams.get("state") ?? "";
  const assigneeID = searchParams.get("assignee_id") ?? "";
  const overdue = searchParams.get("overdue") === "true";
  const [createOpen, setCreateOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [createAssignee, setCreateAssignee] = useState("unassigned");

  const updateFilters = (changes: Record<string, string | null>) => {
    const next = new URLSearchParams(searchParams);
    for (const [key, value] of Object.entries(changes)) {
      if (value) next.set(key, value);
      else next.delete(key);
    }
    setSearchParams(next);
  };

  const tasks = useInfinite<Task>(
    ["tasks", state, assigneeID, queryText, overdue],
    (cursor, signal) => {
      const params = new URLSearchParams({ limit: "25" });
      if (cursor) params.set("cursor", cursor);
      if (state) params.set("state", state);
      if (assigneeID) params.set("assignee_id", assigneeID);
      if (queryText) params.set("q", queryText);
      if (overdue) params.set("overdue", "true");
      return api.get<Paginated<Task>>(`/tasks?${params.toString()}`, { signal });
    },
  );

  const update = useMutation<{ id: string; state: TaskState }, Task>(
    ({ id, state: nextState }) => api.patch<Task>(`/tasks/${encodeURIComponent(id)}`, { state: nextState }, { idempotencyKey: idempotencyKey() }),
    { invalidates: [["tasks", state, assigneeID, queryText, overdue]] },
  );
  const create = useMutation<{ title: string; description: string; assignee_id: string }, Task>(
    (input) => api.post<Task>("/tasks", input, { idempotencyKey: idempotencyKey() }),
    {
      invalidates: [["tasks", state, assigneeID, queryText, overdue]],
      onSuccess: () => {
        setTitle("");
        setDescription("");
        setCreateAssignee("unassigned");
        setCreateOpen(false);
      },
    },
  );

  const memberOptions = [
    { value: "unassigned", label: "Unassigned" },
    ...members.map((member) => ({ value: member.id, label: member.name })),
  ];

  return (
    <Page>
      <PageHeader
        title="Tasks"
        description="Durable follow-ups created by agents and deterministic automation."
        actions={
          <Dialog open={createOpen} onOpenChange={setCreateOpen}>
            <DialogTrigger asChild>
              <Button variant="primary" size="sm" leading={<Plus />}>New task</Button>
            </DialogTrigger>
            <DialogContent
              title="Create task"
              footer={
                <>
                  <Button variant="ghost" size="sm" onClick={() => setCreateOpen(false)}>Cancel</Button>
                  <Button
                    variant="primary"
                    size="sm"
                    loading={create.isPending}
                    disabled={!title.trim()}
                    onClick={() => void create.mutate({ title: title.trim(), description: description.trim(), assignee_id: createAssignee === "unassigned" ? "" : createAssignee }).catch(() => {})}
                  >
                    Create task
                  </Button>
                </>
              }
            >
              {Boolean(create.error) && <Callout tone="danger" className="mb-3">{create.error instanceof ApiError ? create.error.message : "Could not create the task."}</Callout>}
              <div className="space-y-4">
                <Field label="Title"><Input autoFocus value={title} onChange={(event) => setTitle(event.target.value)} placeholder="Follow up with the customer" /></Field>
                <Field label="Description"><Textarea rows={4} value={description} onChange={(event) => setDescription(event.target.value)} placeholder="Add the context an agent needs to complete this task." /></Field>
                <Field label="Assignee"><Select size="sm" value={createAssignee} onValueChange={setCreateAssignee} options={memberOptions} /></Field>
              </div>
            </DialogContent>
          </Dialog>
        }
      />

      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-line bg-surface px-3 py-2">
        <SearchInput inputSize="sm" className="w-64" value={queryText} onChange={(event) => updateFilters({ q: event.target.value })} onClear={() => updateFilters({ q: null })} placeholder="Search tasks" />
        <Select size="sm" value={state} onValueChange={(value) => updateFilters({ state: value || null })} options={stateOptions} aria-label="Task state" />
        <Select size="sm" value={assigneeID || "all"} onValueChange={(value) => updateFilters({ assignee_id: value === "all" ? null : value })} options={[{ value: "all", label: "All assignees" }, ...members.map((member) => ({ value: member.id, label: member.name }))]} aria-label="Task assignee" />
        <Button variant={overdue ? "secondary" : "ghost"} size="sm" leading={<Circle />} onClick={() => updateFilters({ overdue: overdue ? null : "true" })}>Overdue</Button>
      </div>

      <PageBody>
        {tasks.isLoading ? <p className="text-sm text-fg-muted">Loading tasks…</p> : tasks.error ? (
          <EmptyState icon={CheckSquare} title="Tasks unavailable" description={tasks.error instanceof ApiError ? tasks.error.message : "Try again in a moment."} action={<Button variant="secondary" size="sm" onClick={tasks.refetch}>Try again</Button>} />
        ) : tasks.items.length === 0 ? (
          <EmptyState icon={CheckSquare} title="No tasks yet" description="Create a follow-up or let an automation rule create one when support work needs attention." />
        ) : (
          <Card>
            <CardBody className="p-0">
              <ul className="divide-y divide-line-subtle">
                {tasks.items.map((item) => {
                  const isOpen = item.state === "open";
                  const nextState: TaskState = isOpen ? "completed" : "open";
                  const overdueTask = isOpen && item.due_at != null && new Date(item.due_at).getTime() < Date.now();
                  return (
                    <li key={item.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                      <Badge tone={stateTone(item.state)}>{item.state}</Badge>
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-sm font-medium text-fg">{item.title}</p>
                        {item.description && <p className="mt-1 line-clamp-2 text-xs text-fg-muted">{item.description}</p>}
                        <p className="mt-1 text-2xs text-fg-disabled">
                          {item.assignee_name || "Unassigned"}{item.subject_type ? ` · ${item.subject_type}` : ""}{item.created_by_name ? ` · created by ${item.created_by_name}` : ""}
                        </p>
                      </div>
                      <div className="flex shrink-0 items-center gap-3 text-right text-2xs text-fg-muted">
                        {item.due_at && <span className={overdueTask ? "text-danger-text" : undefined}>{overdueTask ? "Overdue · " : "Due · "}{formatDate(item.due_at)}</span>}
                        <span>{formatDate(item.created_at)}</span>
                      </div>
                      <div className="flex shrink-0 items-center gap-1">
                        <Button variant="ghost" size="sm" loading={update.isPending} onClick={() => void update.mutate({ id: item.id, state: nextState }).catch(() => {})} leading={isOpen ? <CheckSquare /> : <RotateCcw />}>
                          {isOpen ? "Complete" : "Reopen"}
                        </Button>
                        {isOpen && <Button variant="danger-ghost" size="sm" loading={update.isPending} onClick={() => void update.mutate({ id: item.id, state: "cancelled" }).catch(() => {})} leading={<X />} aria-label={`Cancel task ${item.title}`}>Cancel</Button>}
                      </div>
                    </li>
                  );
                })}
              </ul>
            </CardBody>
          </Card>
        )}
        <Pagination hasPrevious={false} hasNext={tasks.hasMore} onPrevious={() => undefined} onNext={() => void tasks.fetchNext()} summary={`${tasks.items.length} task${tasks.items.length === 1 ? "" : "s"} loaded`} />
      </PageBody>
    </Page>
  );
}
