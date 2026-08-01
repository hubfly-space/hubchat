import {
  ApiError,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  EmptyState,
  Input,
  Page,
  PageBody,
  PageHeader,
  Section,
  Switch,
  api,
  useAllPages,
  useMutation,
  useQuery,
  type Paginated,
} from "@hubchat/shared";
import { Timer } from "lucide-react";
import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";

type Target = { id: string; priority: string; first_response_minutes: number | null; next_response_minutes: number | null; resolution_minutes: number | null };
type Policy = { id: string; name: string; description: string; enabled: boolean; calendar_id: string | null; pause_states: string[]; warning_threshold_percent: number; targets: Target[]; applies_to: Record<string, unknown>; escalation_actions: Record<string, unknown>[] };

/** Live SLA policy editor. Target changes are saved as one validated policy update. */
export default function PolicyEditor() {
  const { policyId } = useParams();
  const query = useQuery<Policy>(policyId ? ["sla-policy", policyId] : null, (signal) => api.get(`/sla/policies/${encodeURIComponent(policyId ?? "")}`, { signal }), { enabled: Boolean(policyId) });
  const calendars = useAllPages<{ id: string; name: string }>(["sla-calendars", "lookup"], (cursor, signal) => api.get<Paginated<{ id: string; name: string }>>(`/sla/calendars?limit=200${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal }));
  const [draft, setDraft] = useState<Policy | null>(null);
  useEffect(() => { if (query.data) setDraft(query.data); }, [query.data]);
  const toggle = useMutation<{ enabled: boolean }, Policy>((input) => api.patch(`/sla/policies/${encodeURIComponent(policyId ?? "")}`, input), { invalidates: [["sla-policies"], ["sla-policy", policyId ?? ""]], onSuccess: (value) => setDraft(value) });
  const save = useMutation<Record<string, unknown>, Policy>((input) => api.patch(`/sla/policies/${encodeURIComponent(policyId ?? "")}`, input), { invalidates: [["sla-policies"], ["sla-policy", policyId ?? ""]], onSuccess: (value) => setDraft(value) });

  if (!policyId || query.error instanceof ApiError && query.error.isNotFound) return <Page><EmptyState icon={Timer} size="lg" title="Policy not found" description="The policy may have been removed or belongs to another workspace." action={<Button variant="secondary" asChild><Link to="/sla/policies">Back to policies</Link></Button>} /></Page>;
  if (query.isLoading) return <Page><PageHeader title="SLA policy" /><PageBody><p className="text-sm text-fg-muted">Loading live policy…</p></PageBody></Page>;
  if (query.error || !query.data) return <Page><EmptyState icon={Timer} size="lg" title="Policy unavailable" description="Could not load this SLA policy." action={<Button variant="secondary" onClick={query.refetch}>Try again</Button>} /></Page>;
  const policy = draft ?? query.data;
  if (!policy) return null;

  const updateTarget = (id: string, field: "first_response_minutes" | "next_response_minutes" | "resolution_minutes", value: string) => {
    setDraft((current) => current && ({ ...current, targets: current.targets.map((target) => target.id === id ? { ...target, [field]: value === "" ? null : Math.max(0, Number(value)) } : target) }));
  };
  const savePolicy = () => void save.mutate({
    name: policy.name,
    description: policy.description,
    calendar_id: policy.calendar_id ?? "",
    pause_states: policy.pause_states,
    warning_threshold_percent: policy.warning_threshold_percent,
    escalation_actions: policy.escalation_actions,
    applies_to: policy.applies_to,
    targets: policy.targets.map(({ priority, first_response_minutes, next_response_minutes, resolution_minutes }) => ({ priority, first_response_minutes, next_response_minutes, resolution_minutes })),
    enabled: policy.enabled,
  }).catch(() => {});

  return <Page>
    <PageHeader breadcrumbs={[{ label: "SLA policies", href: "/sla/policies" }, { label: policy.name }]} title={policy.name} description={policy.description || "Response and resolution targets measured in business hours."} meta={<Badge tone={policy.enabled ? "success" : "neutral"}>{policy.enabled ? "Active" : "Disabled"}</Badge>} actions={<div className="flex items-center gap-3"><Button variant="primary" size="sm" loading={save.isPending} onClick={savePolicy}>Save changes</Button><Switch checked={policy.enabled} onCheckedChange={(enabled) => void toggle.mutate({ enabled }).catch(() => {})} aria-label="Enable SLA policy" /></div>} />
    <PageBody width="narrow">
      {Boolean(toggle.error || save.error) && <Callout tone="danger" className="mb-5">Could not update this policy. {toggle.error instanceof ApiError ? toggle.error.message : save.error instanceof ApiError ? save.error.message : "Try again."}</Callout>}
      <Section title="Targets" description="Measured in business hours against the configured calendar.">
        <Card><CardBody className="p-0"><div className="overflow-x-auto"><table className="w-full min-w-[680px] text-sm"><thead><tr className="border-b border-line"><th className="px-4 py-2 text-left text-2xs font-semibold uppercase tracking-caps text-fg-muted">Priority</th><th className="px-4 py-2 text-right text-2xs font-semibold uppercase tracking-caps text-fg-muted">First response (min)</th><th className="px-4 py-2 text-right text-2xs font-semibold uppercase tracking-caps text-fg-muted">Next response (min)</th><th className="px-4 py-2 text-right text-2xs font-semibold uppercase tracking-caps text-fg-muted">Resolution (min)</th></tr></thead><tbody>{policy.targets.map((target) => <tr key={target.id} className="border-b border-line-subtle last:border-b-0"><td className="px-4 py-2 capitalize text-fg">{target.priority}</td>{(["first_response_minutes", "next_response_minutes", "resolution_minutes"] as const).map((field) => <td key={field} className="px-4 py-2"><Input inputSize="sm" type="number" min={0} value={target[field] ?? ""} onChange={(event) => updateTarget(target.id, field, event.target.value)} aria-label={`${target.priority} ${field.replaceAll("_", " ")}`} /></td>)}</tr>)}</tbody></table></div></CardBody></Card>
      </Section>
      <Section title="Clock">
        <Card><CardBody className="space-y-4 text-sm"><label className="block"><span className="mb-1 block text-xs text-fg-muted">Calendar</span><select className="h-9 w-full rounded-md border border-line bg-surface px-3 text-sm text-fg" value={policy.calendar_id ?? ""} onChange={(event) => setDraft({ ...policy, calendar_id: event.target.value || null })}><option value="">24/7 UTC default</option>{calendars.items.map((calendar) => <option key={calendar.id} value={calendar.id}>{calendar.name}</option>)}</select></label><label className="block"><span className="mb-1 block text-xs text-fg-muted">Pause states (comma separated)</span><Input value={policy.pause_states.join(", ")} onChange={(event) => setDraft({ ...policy, pause_states: event.target.value.split(",").map((value) => value.trim()).filter(Boolean) })} /></label><label className="block"><span className="mb-1 block text-xs text-fg-muted">Warning threshold (%)</span><Input type="number" min={1} max={100} value={policy.warning_threshold_percent} onChange={(event) => setDraft({ ...policy, warning_threshold_percent: Math.min(100, Math.max(1, Number(event.target.value) || 1)) })} /></label></CardBody></Card>
      </Section>
      <Section title="Scope" description="The server applies this policy only within the current workspace."><Card><CardBody><pre className="overflow-auto text-xs text-fg-secondary">{JSON.stringify(policy.applies_to ?? {}, null, 2)}</pre></CardBody></Card></Section>
    </PageBody>
  </Page>;
}
