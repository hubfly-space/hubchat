import {
  ApiError,
  Badge,
  Button,
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
  Section,
  Switch,
  api,
  idempotencyKey,
  useInfinite,
  useMutation,
  type Paginated,
} from "@hubchat/shared";
import { Plus, Workflow } from "lucide-react";
import { Link } from "react-router-dom";
import { useState } from "react";

type Action = { id: string; type: string; params: Record<string, unknown> };
type Rule = { id: string; name: string; description: string; trigger: string; enabled: boolean; version: number; run_count_24h: number; error_count_24h: number; last_run_at: string | null; conditions: Record<string, unknown>; actions: Action[] };
type RuleInput = { name: string; trigger: string; conditions: Record<string, never>; actions: never[]; enabled: boolean };

export default function RuleList() {
  const query = useInfinite<Rule>(
    ["automation-rules"],
    (cursor, signal) => {
      const params = new URLSearchParams({ limit: "50" });
      if (cursor) params.set("cursor", cursor);
      return api.get<Paginated<Rule>>(`/automation/rules?${params.toString()}`, { signal });
    },
  );
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [trigger, setTrigger] = useState("conversation.created");
  const create = useMutation<RuleInput, Rule>((input) => api.post("/automation/rules", input, { idempotencyKey: idempotencyKey() }), { invalidates: [["automation-rules"]], onSuccess: () => { setOpen(false); setName(""); } });
  const toggle = useMutation<{ rule: Rule; enabled: boolean }, Rule>(({ rule, enabled }) => api.patch(`/automation/rules/${encodeURIComponent(rule.id)}`, { name: rule.name, description: rule.description, trigger: rule.trigger, conditions: rule.conditions, actions: rule.actions, enabled }), { invalidates: [["automation-rules"]] });
  const rules = query.items;

  return <Page><PageHeader title="Automation rules" description="Deterministic event rules with explicit conditions, actions, versions, and execution logs." actions={<Dialog open={open} onOpenChange={setOpen}><DialogTrigger asChild><Button variant="primary" size="sm" leading={<Plus />}>New rule</Button></DialogTrigger><DialogContent title="Create automation rule" footer={<><Button variant="ghost" size="sm" onClick={() => setOpen(false)}>Cancel</Button><Button variant="primary" size="sm" loading={create.isPending} disabled={!name.trim()} onClick={() => void create.mutate({ name: name.trim(), trigger, conditions: {}, actions: [], enabled: false }).catch(() => {})}>Create rule</Button></>}><div className="space-y-4"><Field label="Name"><Input autoFocus value={name} onChange={(event) => setName(event.target.value)} placeholder="Tag urgent conversations" /></Field><Field label="Trigger"><select className="h-9 w-full rounded-md border border-line bg-surface px-3 text-sm text-fg" value={trigger} onChange={(event) => setTrigger(event.target.value)}><option value="conversation.created">Conversation created</option><option value="message.received">Message received</option><option value="ticket.created">Ticket created</option><option value="form.submitted">Form submitted</option><option value="sla.breached">SLA breached</option></select></Field>{Boolean(create.error) && <p className="text-sm text-danger">Could not create rule.</p>}</div></DialogContent></Dialog>} /><PageBody><Section>{query.isLoading ? <p className="text-sm text-fg-muted">Loading rules…</p> : query.error ? <EmptyState icon={Workflow} title="Automation unavailable" description={query.error instanceof ApiError ? query.error.message : "Try again in a moment."} action={<Button variant="secondary" onClick={query.refetch}>Try again</Button>} /> : rules.length === 0 ? <EmptyState icon={Workflow} title="No automation rules" description="Create a rule when you are ready to make one deterministic change on a known event." /> : <div className="space-y-3">{rules.map((rule) => <Card key={rule.id}><CardBody className="flex flex-wrap items-center gap-4"><div className="min-w-0 flex-1"><div className="flex items-center gap-2"><Link to={`/automation/rules/${rule.id}`} className="truncate text-sm font-medium text-fg hover:underline">{rule.name}</Link><Badge tone={rule.enabled ? "success" : "neutral"}>{rule.enabled ? "Active" : "Draft"}</Badge></div><p className="mt-1 text-xs text-fg-muted">When {rule.trigger} · {rule.actions.length} action{rule.actions.length === 1 ? "" : "s"} · v{rule.version}</p></div><div className="flex items-center gap-4 text-xs tabular text-fg-muted"><span>{rule.run_count_24h} runs / 24h</span>{rule.error_count_24h > 0 && <Badge tone="danger">{rule.error_count_24h} errors</Badge>}</div><Switch checked={rule.enabled} onCheckedChange={(enabled) => void toggle.mutate({ rule, enabled }).catch(() => {})} aria-label={`Enable ${rule.name}`} /></CardBody></Card>)}</div>}</Section></PageBody><Pagination hasPrevious={false} hasNext={query.hasMore} onPrevious={() => undefined} onNext={() => void query.fetchNext()} summary={`${rules.length} rule${rules.length === 1 ? "" : "s"} loaded`} /></Page>;
}
