import {
  ApiError,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  EmptyState,
  Page,
  PageBody,
  PageHeader,
  Section,
  Switch,
  api,
  formatDuration,
  useMutation,
  useQuery,
} from "@hubchat/shared";
import { Timer } from "lucide-react";
import { Link, useParams } from "react-router-dom";

type Target = { id: string; priority: string; first_response_minutes: number | null; next_response_minutes: number | null; resolution_minutes: number | null };
type Policy = { id: string; name: string; description: string; enabled: boolean; calendar_id: string | null; pause_states: string[]; warning_threshold_percent: number; targets: Target[]; applies_to: Record<string, unknown>; escalation_actions: Record<string, unknown>[] };

/** Live SLA policy editor. Configuration writes currently expose the safe enabled toggle; target/calendar editing remains server-owned until update validation is expanded. */
export default function PolicyEditor() {
  const { policyId } = useParams();
  const query = useQuery<Policy>(policyId ? ["sla-policy", policyId] : null, (signal) => api.get(`/sla/policies/${encodeURIComponent(policyId ?? "")}`, { signal }), { enabled: Boolean(policyId) });
  const toggle = useMutation<{ enabled: boolean }, Policy>((input) => api.patch(`/sla/policies/${encodeURIComponent(policyId ?? "")}`, input), { invalidates: [["sla-policies"], ["sla-policy", policyId ?? ""]] });

  if (!policyId || query.error instanceof ApiError && query.error.isNotFound) return <Page><EmptyState icon={Timer} size="lg" title="Policy not found" description="The policy may have been removed or belongs to another workspace." action={<Button variant="secondary" asChild><Link to="/sla/policies">Back to policies</Link></Button>} /></Page>;
  if (query.isLoading) return <Page><PageHeader title="SLA policy" /><PageBody><p className="text-sm text-fg-muted">Loading live policy…</p></PageBody></Page>;
  if (query.error || !query.data) return <Page><EmptyState icon={Timer} size="lg" title="Policy unavailable" description="Could not load this SLA policy." action={<Button variant="secondary" onClick={query.refetch}>Try again</Button>} /></Page>;
  const policy = query.data;

  return <Page>
    <PageHeader breadcrumbs={[{ label: "SLA policies", href: "/sla/policies" }, { label: policy.name }]} title={policy.name} description={policy.description || "Response and resolution targets measured in business hours."} meta={<Badge tone={policy.enabled ? "success" : "neutral"}>{policy.enabled ? "Active" : "Disabled"}</Badge>} actions={<Switch checked={policy.enabled} onCheckedChange={(enabled) => void toggle.mutate({ enabled }).catch(() => {})} aria-label="Enable SLA policy" />} />
    <PageBody width="narrow">
      {Boolean(toggle.error) && <Callout tone="danger" className="mb-5">Could not update this policy. {toggle.error instanceof ApiError ? toggle.error.message : "Try again."}</Callout>}
      <Section title="Targets" description="Measured in business hours against the configured calendar.">
        <Card><CardBody className="p-0"><table className="w-full text-sm"><thead><tr className="border-b border-line"><th className="px-4 py-2 text-left text-2xs font-semibold uppercase tracking-caps text-fg-muted">Priority</th><th className="px-4 py-2 text-right text-2xs font-semibold uppercase tracking-caps text-fg-muted">First response</th><th className="px-4 py-2 text-right text-2xs font-semibold uppercase tracking-caps text-fg-muted">Next response</th><th className="px-4 py-2 text-right text-2xs font-semibold uppercase tracking-caps text-fg-muted">Resolution</th></tr></thead><tbody>{policy.targets.map((target) => <tr key={target.id} className="border-b border-line-subtle last:border-b-0"><td className="px-4 py-2 capitalize text-fg">{target.priority}</td><td className="px-4 py-2 text-right tabular text-fg-secondary">{target.first_response_minutes == null ? "—" : formatDuration(target.first_response_minutes * 60)}</td><td className="px-4 py-2 text-right tabular text-fg-secondary">{target.next_response_minutes == null ? "—" : formatDuration(target.next_response_minutes * 60)}</td><td className="px-4 py-2 text-right tabular text-fg-secondary">{target.resolution_minutes == null ? "—" : formatDuration(target.resolution_minutes * 60)}</td></tr>)}</tbody></table></CardBody></Card>
      </Section>
      <Section title="Clock">
        <Card><CardBody className="space-y-3 text-sm"><p><span className="text-fg-muted">Calendar:</span> {policy.calendar_id ?? "24/7 UTC default"}</p><p><span className="text-fg-muted">Pause states:</span> {policy.pause_states.length ? policy.pause_states.join(", ") : "Never"}</p><p><span className="text-fg-muted">Warning threshold:</span> {policy.warning_threshold_percent}%</p></CardBody></Card>
      </Section>
      <Section title="Scope" description="The server applies this policy only within the current workspace."><Card><CardBody><pre className="overflow-auto text-xs text-fg-secondary">{JSON.stringify(policy.applies_to ?? {}, null, 2)}</pre></CardBody></Card></Section>
    </PageBody>
  </Page>;
}
