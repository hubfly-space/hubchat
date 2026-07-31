import {
  ApiError,
  Badge,
  Button,
  Card,
  CardBody,
  EmptyState,
  Callout,
  Dialog,
  DialogContent,
  Field,
  Input,
  Page,
  PageBody,
  PageHeader,
  Section,
  Tooltip,
  api,
  idempotencyKey,
  formatRelativeShort,
  useMutation,
  useQuery,
} from "@hubchat/shared";
import { CheckCircle2, ExternalLink, Globe, Plus, Settings2 } from "lucide-react";
import { Link } from "react-router-dom";
import { useState } from "react";

type PortalRow = {
  id: string;
  name: string;
  subdomain: string;
  theme: Record<string, unknown>;
  features: Record<string, unknown>;
  enabled: boolean;
  updated_at?: string;
};

/** Customer portals (§6.5). Configuration is loaded from the tenant API. */
export default function PortalList() {
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [subdomain, setSubdomain] = useState("");
  const query = useQuery<{ data: PortalRow[] }>(["portals"], (signal) => api.get("/portals", { signal }));
  const create = useMutation<void, PortalRow>(() => api.post<PortalRow>("/portals", { name, subdomain }, { idempotencyKey: idempotencyKey() }), { invalidates: [["portals"]], onSuccess: () => { setName(""); setSubdomain(""); setCreating(false); } });
  const portals = query.data?.data ?? [];

  return (
    <Page>
      <PageHeader
        title="Portals"
        description="Hosted, branded sites where customers submit tickets, track history, and browse help."
        actions={<Button variant="primary" size="sm" leading={<Plus />} onClick={() => setCreating(true)}>New portal</Button>}
      />
      <PageBody>
        <Section>
          {query.isLoading ? <div className="py-12 text-center text-sm text-fg-muted">Loading portals…</div> : query.isError ? (
            <div className="py-12 text-center text-sm text-danger">{query.error instanceof ApiError ? query.error.message : "Could not load portals."}<div><Button className="mt-4" variant="secondary" size="sm" onClick={query.refetch}>Try again</Button></div></div>
          ) : portals.length === 0 ? (
            <EmptyState icon={Globe} title="No portals yet" description="Create a portal through the workspace API, then customise its branding and customer permissions." />
          ) : (
            <div className="space-y-3">
              {portals.map((portal) => {
                const accent = typeof portal.theme.accent === "string" ? portal.theme.accent : "#3B6EF6";
                const features = Object.entries(portal.features).filter(([, enabled]) => enabled).map(([key]) => key.replace(/_/g, " ")).join(" · ");
                return <Card key={portal.id}><CardBody><div className="flex flex-wrap items-start gap-4">
                  <span aria-hidden="true" className="mt-0.5 size-9 shrink-0 rounded-lg border border-line-strong" style={{ backgroundColor: accent }} />
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2"><Link to={`/channels/portals/${portal.id}`} className="truncate text-sm font-medium text-fg hover:underline">{portal.name}</Link>{!portal.enabled && <Badge tone="warning">Disabled</Badge>}</div>
                    <div className="mt-1.5 flex flex-wrap items-center gap-2"><a href={`/portal/?portal=${encodeURIComponent(portal.id)}`} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 font-mono text-xs text-accent-text hover:underline">{portal.subdomain}<ExternalLink aria-hidden="true" className="size-3" /></a><Tooltip content="Portal configuration is stored in PostgreSQL"><span><Badge tone={portal.enabled ? "success" : "neutral"} leading={portal.enabled ? <CheckCircle2 /> : undefined}>{portal.enabled ? "Enabled" : "Disabled"}</Badge></span></Tooltip></div>
                    <p className="mt-2 text-xs text-fg-muted">{features || "No sections configured"}{portal.updated_at ? ` · updated ${formatRelativeShort(portal.updated_at)} ago` : ""}</p>
                  </div>
                  <Button variant="secondary" size="sm" leading={<Settings2 />} asChild><Link to={`/channels/portals/${portal.id}`}>Customise</Link></Button>
                </div></CardBody></Card>;
              })}
            </div>
          )}
        </Section>
      </PageBody>
      <Dialog open={creating} onOpenChange={setCreating}>
        <DialogContent title="New portal" description="Create the customer-facing shell first; branding and sections can be customised next." footer={<Button variant="primary" size="sm" loading={create.isPending} disabled={!name.trim() || !subdomain.trim()} onClick={() => void create.mutate().catch(() => {})}>Create portal</Button>}>
          {Boolean(create.error) && <Callout tone="danger" className="mb-3">{create.error instanceof ApiError ? create.error.message : "Could not create this portal."}</Callout>}
          <div className="flex flex-col gap-3 pb-4"><Field label="Name"><Input value={name} onChange={(event) => setName(event.target.value)} placeholder="Northwind Help Centre" autoFocus /></Field><Field label="Subdomain" description="Lowercase letters, numbers, and hyphens."><Input value={subdomain} onChange={(event) => setSubdomain(event.target.value.toLowerCase())} placeholder="northwind" /></Field></div>
        </DialogContent>
      </Dialog>
    </Page>
  );
}
