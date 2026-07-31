import {
  Badge,
  ApiError,
  Button,
  Callout,
  Card,
  CardBody,
  CodeBlock,
  ConfirmDialog,
  DataTable,
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
  Checkbox,
  Tooltip,
  api,
  idempotencyKey,
  formatDate,
  formatRelativeShort,
  useMutation,
  useQuery,
  type ApiKey,
  type Column,
} from "@hubchat/shared";
import { KeyRound, Plus, ShieldAlert, Trash2 } from "lucide-react";
import { useState } from "react";
import { useWorkspace } from "../../app/workspace-context";

const SCOPES = [
  { value: "conversation.read", label: "Read conversations" },
  { value: "conversation.reply", label: "Reply to conversations" },
  { value: "customer.read", label: "Read customers" },
  { value: "customer.read_sensitive", label: "Read sensitive customer fields" },
  { value: "ticket.manage", label: "Manage tickets" },
  { value: "report.read", label: "Read reports" },
  { value: "integration.manage", label: "Manage integrations" },
];

/** API keys (§6.16). */
export default function ApiKeys() {
  const { memberById } = useWorkspace();
  const deploymentOrigin = window.location.origin;
  const [revoking, setRevoking] = useState<ApiKey | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [name, setName] = useState("");
  const [expiresAt, setExpiresAt] = useState("");
  const [scopes, setScopes] = useState<string[]>(["conversation.read"]);
  const [createdToken, setCreatedToken] = useState<string | null>(null);
  const query = useQuery<{ data: ApiKey[] }>(["api-keys"], (signal) => api.get("/api-keys", { signal }));
  const create = useMutation<{ name: string; scopes: string[]; expires_at: string }, { token: string }>(
    (input) => api.post<{ token: string }>("/api-keys", input, { idempotencyKey: idempotencyKey() }),
    { invalidates: [["api-keys"]], onSuccess: (result) => setCreatedToken(result.token) },
  );
  const revoke = useMutation<string, void>(
    (id) => api.delete(`/api-keys/${id}`),
    { invalidates: [["api-keys"]], onSuccess: () => setRevoking(null) },
  );
  const keys = query.data?.data ?? [];

  const columns: Column<ApiKey>[] = [
    {
      key: "name",
      header: "Key",
      cell: (key) => (
        <div className="min-w-0">
          <p className="truncate text-sm text-fg">{key.name}</p>
          <p className="truncate font-mono text-xs text-fg-muted">{key.prefix}…</p>
        </div>
      ),
    },
    {
      key: "scopes",
      header: "Scopes",
      width: "220px",
      hideBelow: "lg",
      cell: (key) => (
        <Tooltip content={key.scopes.join(", ")}>
          <span className="flex flex-wrap gap-1">
            {key.scopes.slice(0, 2).map((scope) => (
              <Badge key={scope} tone="neutral" variant="outline">
                {scope}
              </Badge>
            ))}
            {key.scopes.length > 2 && (
              <span className="text-2xs text-fg-muted">+{key.scopes.length - 2}</span>
            )}
          </span>
        </Tooltip>
      ),
    },
    {
      key: "created_by",
      header: "Created by",
      width: "140px",
      hideBelow: "xl",
      cell: (key) => (
        <span className="text-xs text-fg-secondary">{memberById(key.created_by)?.name ?? "—"}</span>
      ),
    },
    {
      key: "last_used_at",
      header: "Last used",
      width: "104px",
      numeric: true,
      cell: (key) =>
        key.last_used_at ? (
          <span className="text-xs text-fg-muted">{formatRelativeShort(key.last_used_at)}</span>
        ) : (
          <span className="text-xs text-fg-disabled">never</span>
        ),
      sortable: true,
    },
    {
      key: "expires_at",
      header: "Expires",
      width: "110px",
      hideBelow: "md",
      cell: (key) =>
        key.revoked_at ? (
          <Badge tone="danger">Revoked</Badge>
        ) : key.expires_at ? (
          <span className="text-xs text-fg-muted">{formatDate(key.expires_at)}</span>
        ) : (
          <span className="text-xs text-fg-disabled">Never</span>
        ),
    },
  ];

  return (
    <Page>
      <PageHeader
        title="API keys"
        description="Workspace-scoped credentials for server-to-server calls. Keys are hashed at rest and shown once."
        actions={
          <Dialog open={createOpen} onOpenChange={(open) => { setCreateOpen(open); if (!open) { setCreatedToken(null); create.reset(); } }}>
            <DialogTrigger asChild><Button variant="primary" size="sm" leading={<Plus />}>New key</Button></DialogTrigger>
            <DialogContent
              title={createdToken ? "API key created" : "Create an API key"}
              description="Grant only the scopes this integration actually needs. You can rotate or revoke it at any time."
              size="lg"
              footer={
                <>
                  <Button variant="ghost" size="sm" onClick={() => setCreateOpen(false)}>{createdToken ? "Done" : "Cancel"}</Button>
                  {!createdToken && <Button variant="primary" size="sm" loading={create.isPending} disabled={!name.trim() || scopes.length === 0} onClick={() => void create.mutate({ name: name.trim(), scopes, expires_at: expiresAt }).catch(() => {})}>Create key</Button>}
                </>
              }
            >
              {createdToken ? <div className="space-y-4 pb-2"><Callout tone="success">Copy this token now. It will not be shown again.</Callout><CodeBlock language="text" code={createdToken} /></div> : <div className="space-y-4 pb-2">
                {Boolean(create.error) && <Callout tone="danger">{create.error instanceof Error ? create.error.message : "Could not create this key."}</Callout>}
                <Field label="Name" description="Where this key will be used.">
                  <Input value={name} onChange={(event) => setName(event.target.value)} placeholder="Production backend" autoFocus />
                </Field>

                <Field
                  label="Expiry"
                  description="A key with no expiry never rotates itself. Prefer 90 days for anything you can automate."
                >
                  <Input type="date" value={expiresAt} onChange={(event) => setExpiresAt(event.target.value)} />
                </Field>

                <Field label="Scopes">
                  <div className="grid gap-2 sm:grid-cols-2">
                    {SCOPES.map((scope) => (
                        <Checkbox
                          key={scope.value}
                          label={scope.label}
                          description={scope.value}
                          checked={scopes.includes(scope.value)}
                          onCheckedChange={(checked) => setScopes((current) => checked === true ? [...new Set([...current, scope.value])] : current.filter((item) => item !== scope.value))}
                      />
                    ))}
                  </div>
                </Field>

                <Callout tone="warning" icon={<ShieldAlert />}>
                  The full key is displayed exactly once, immediately after creation. Hubchat stores
                  only a hash and cannot recover it for you.
                </Callout>
              </div>}
            </DialogContent>
          </Dialog>
        }
      />

      <PageBody>
        <Section>
          <Card>
            <CardBody className="p-0">
              <DataTable
                aria-label="API keys"
                rows={keys}
                columns={columns}
                rowKey={(key) => key.id}
                rowActions={(key) =>
                  key.revoked_at ? null : (
                    <Button
                      variant="ghost"
                      size="xs"
                      iconOnly
                      aria-label={`Revoke ${key.name}`}
                      leading={<Trash2 />}
                      onClick={() => setRevoking(key)}
                    />
                  )
                }
                empty={
                  <EmptyState
                    icon={KeyRound}
                    title="No API keys"
                    description="Create one when you are ready to call the REST API from your own services."
                  />
                }
              />
              {query.isLoading && <p className="p-4 text-sm text-fg-muted">Loading API keys…</p>}
              {query.isError && <p className="p-4 text-sm text-danger">{query.error instanceof ApiError ? query.error.message : "Could not load API keys."}</p>}
            </CardBody>
          </Card>
        </Section>

        <Section title="Using a key">
          <CodeBlock
            language="bash"
            code={`curl ${deploymentOrigin}/api/v1/conversations \\
  -H "Authorization: Bearer hc_live_9f2a…" \\
  -H "Idempotency-Key: $(uuidgen)" \\
  -H "Content-Type: application/json"`}
          />
          <p className="mt-2 text-xs text-fg-muted">
            Every response carries <code className="font-mono">X-Request-Id</code> and rate-limit
            headers. Include an idempotency key on any request that creates something.
          </p>
        </Section>
      </PageBody>

      <ConfirmDialog
        open={revoking !== null}
        onOpenChange={(open) => !open && setRevoking(null)}
        title="Revoke this key?"
        description={
          <>
            Requests using <span className="font-mono text-fg">{revoking?.prefix}…</span> will start
            failing with 401 immediately. This cannot be undone — you will need to issue a new key
            and update whatever uses it.
          </>
        }
        confirmLabel="Revoke key"
        destructive
        onConfirm={() => revoking && void revoke.mutate(revoking.id).catch(() => {})}
      />
    </Page>
  );
}
