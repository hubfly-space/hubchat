import {
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CodeBlock,
  ConfirmDialog,
  DataTable,
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
  Section,
  Checkbox,
  Tooltip,
  formatDate,
  formatRelativeShort,
  type ApiKey,
  type Column,
} from "@hubchat/shared";
import { KeyRound, Plus, ShieldAlert, Trash2 } from "lucide-react";
import { useState } from "react";
import { useWorkspace } from "../../app/workspace-context";
import { NOW, apiKeys } from "../../data/fixtures";

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
  const [revoking, setRevoking] = useState<ApiKey | null>(null);

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
          <span className="text-xs text-fg-muted">{formatRelativeShort(key.last_used_at, NOW)}</span>
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
          <Dialog>
            <DialogTrigger asChild>
              <Button variant="primary" size="sm" leading={<Plus />}>
                New key
              </Button>
            </DialogTrigger>
            <DialogContent
              title="Create an API key"
              description="Grant only the scopes this integration actually needs. You can rotate or revoke it at any time."
              size="lg"
              footer={
                <>
                  <DialogClose asChild>
                    <Button variant="ghost" size="sm">
                      Cancel
                    </Button>
                  </DialogClose>
                  <Button variant="primary" size="sm">
                    Create key
                  </Button>
                </>
              }
            >
              <div className="space-y-4 pb-2">
                <Field label="Name" description="Where this key will be used.">
                  <Input placeholder="Production backend" />
                </Field>

                <Field
                  label="Expiry"
                  description="A key with no expiry never rotates itself. Prefer 90 days for anything you can automate."
                >
                  <Input type="date" />
                </Field>

                <Field label="Scopes">
                  <div className="grid gap-2 sm:grid-cols-2">
                    {SCOPES.map((scope) => (
                      <Checkbox
                        key={scope.value}
                        label={scope.label}
                        description={scope.value}
                      />
                    ))}
                  </div>
                </Field>

                <Callout tone="warning" icon={<ShieldAlert />}>
                  The full key is displayed exactly once, immediately after creation. Hubchat stores
                  only a hash and cannot recover it for you.
                </Callout>
              </div>
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
                rows={apiKeys}
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
            </CardBody>
          </Card>
        </Section>

        <Section title="Using a key">
          <CodeBlock
            language="bash"
            code={`curl https://support.northwind.cloud/api/v1/conversations \\
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
        onConfirm={() => setRevoking(null)}
      />
    </Page>
  );
}
