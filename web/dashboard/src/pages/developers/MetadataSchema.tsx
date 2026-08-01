import {
  api,
  ApiError,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  Checkbox,
  DataTable,
  Dialog,
  DialogContent,
  EmptyState,
  Field,
  Input,
  Page,
  PageBody,
  PageHeader,
  Section,
  Select,
  Textarea,
  Tooltip,
  useInfinite,
  useMutation,
  type AttributeDefinition,
  type AttributeType,
  type Column,
  type MetadataSource,
  type Paginated,
} from "@hubchat/shared";
import { Braces, EyeOff, Pencil, Plus, ShieldAlert, Trash2 } from "lucide-react";
import { useState } from "react";

const SOURCES: { value: MetadataSource; label: string }[] = [
  { value: "js_sdk", label: "JavaScript SDK" },
  { value: "rest_api", label: "REST API" },
  { value: "identity_token", label: "Signed identity token" },
  { value: "widget_init", label: "Widget initialisation" },
  { value: "portal_profile", label: "Portal profile" },
  { value: "form", label: "Form submission" },
  { value: "url_params", label: "URL parameters" },
  { value: "local_storage", label: "Local storage keys" },
  { value: "cookie", label: "Cookies" },
];

const TYPES: { value: AttributeType; label: string }[] = [
  { value: "string", label: "String" },
  { value: "integer", label: "Integer" },
  { value: "decimal", label: "Decimal" },
  { value: "boolean", label: "Boolean" },
  { value: "timestamp", label: "Timestamp" },
  { value: "date", label: "Date" },
  { value: "enum", label: "Enum" },
  { value: "string_list", label: "String list" },
  { value: "url", label: "URL" },
  { value: "json", label: "JSON object" },
];

/**
 * Metadata schema (§6.10). The allowlist. Nothing reaches a customer or
 * company record unless it is declared here — enforced server-side, not
 * just by this UI's own form.
 */
export default function MetadataSchema() {
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<AttributeDefinition | null>(null);

  const definitions = useInfinite<AttributeDefinition>(["attribute-definitions", "customer"], (cursor, signal) => {
    const params = new URLSearchParams({ entity_type: "customer", limit: "50" });
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<AttributeDefinition>>(`/attribute-definitions?${params.toString()}`, { signal });
  });
  const rows = definitions.items;

  const archive = useMutation<string, unknown>(
    (id) => api.delete(`/attribute-definitions/${id}`),
    { invalidates: [["attribute-definitions", "customer"]] },
  );

  const columns: Column<AttributeDefinition>[] = [
    {
      key: "key",
      header: "Key",
      cell: (field) => (
        <div className="min-w-0">
          <p className="truncate font-mono text-xs text-fg">{field.key}</p>
          <p className="truncate text-xs text-fg-muted">{field.label}</p>
        </div>
      ),
      sortable: true,
    },
    {
      key: "type",
      header: "Type",
      width: "116px",
      cell: (field) => <Badge tone="neutral">{TYPES.find((t) => t.value === field.type)?.label ?? field.type}</Badge>,
    },
    {
      key: "sources",
      header: "Accepted from",
      width: "220px",
      hideBelow: "lg",
      cell: (field) => (
        <Tooltip content={field.allowed_sources.join(", ") || "None — this key is never accepted"}>
          <span className="flex flex-wrap gap-1">
            {field.allowed_sources.slice(0, 2).map((source) => (
              <Badge key={source} tone="neutral" variant="outline">
                {source}
              </Badge>
            ))}
            {field.allowed_sources.length > 2 && (
              <span className="text-2xs text-fg-muted">+{field.allowed_sources.length - 2}</span>
            )}
            {field.allowed_sources.length === 0 && <span className="text-2xs text-fg-disabled">none</span>}
          </span>
        </Tooltip>
      ),
    },
    {
      key: "searchable",
      header: "Searchable",
      width: "104px",
      align: "center",
      hideBelow: "md",
      cell: (field) => (field.searchable ? <Badge tone="success">Yes</Badge> : <span className="text-xs text-fg-disabled">No</span>),
    },
    {
      key: "sensitive",
      header: "Sensitive",
      width: "116px",
      cell: (field) =>
        field.sensitive ? (
          <Tooltip content="Masked in the UI, excluded from search and export, and audited on reveal">
            <span>
              <Badge tone="warning" leading={<EyeOff />}>
                Sensitive
              </Badge>
            </span>
          </Tooltip>
        ) : (
          <span className="text-xs text-fg-disabled">—</span>
        ),
    },
  ];

  return (
    <Page>
      <PageHeader
        title="Metadata schema"
        description="The allowlist of custom attributes Hubchat will accept, and which sources may set each one."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />} onClick={() => setCreating(true)}>
            New attribute
          </Button>
        }
      />

      <PageBody>
        <Callout tone="warning" className="mb-5" icon={<ShieldAlert />}>
          Marking an attribute sensitive changes behaviour immediately: existing values are masked in the UI
          unless the viewer holds customer.read_sensitive, and revealing one is recorded in the audit log.
        </Callout>

        <Section title="Attributes">
          <Card>
            <CardBody className="p-0">
              {definitions.error ? <div className="p-4"><EmptyState icon={Braces} title="Metadata schema unavailable" description="Could not load attribute definitions." action={<Button variant="secondary" size="sm" onClick={definitions.refetch}>Try again</Button>} /></div> : <>
                <DataTable
                  aria-label="Metadata attributes"
                  rows={rows}
                  columns={columns}
                  rowKey={(field) => field.id}
                  loading={definitions.isLoading}
                  rowActions={(field) => (
                    <div className="flex items-center gap-0.5">
                      <Button variant="ghost" size="xs" iconOnly aria-label="Edit attribute" leading={<Pencil />} onClick={() => setEditing(field)} />
                      <Button
                        variant="ghost"
                        size="xs"
                        iconOnly
                        aria-label="Archive attribute"
                        leading={<Trash2 />}
                        onClick={() => void archive.mutate(field.id).catch(() => {})}
                      />
                    </div>
                  )}
                  empty={
                    <EmptyState
                      icon={Braces}
                      title="No attributes defined"
                      description="Declare the attributes your application will send before wiring up the SDK."
                    />
                  }
                />
                {definitions.hasMore && <div className="flex justify-center border-t border-line-subtle p-3"><Button variant="secondary" size="sm" loading={definitions.isFetching} onClick={() => void definitions.fetchNext()}>Load more attributes</Button></div>}
              </>}
            </CardBody>
          </Card>
        </Section>

        <Section title="Setting attributes">
          <p className="mb-2 text-xs text-fg-muted">
            Only keys declared above, from a pipeline listed in that key's accepted sources, are ever written.
          </p>
        </Section>
      </PageBody>

      {creating && <AttributeDialog onClose={() => setCreating(false)} />}
      {editing && <AttributeDialog existing={editing} onClose={() => setEditing(null)} />}
    </Page>
  );
}

function AttributeDialog({ existing, onClose }: { existing?: AttributeDefinition; onClose: () => void }) {
  const [label, setLabel] = useState(existing?.label ?? "");
  const [key, setKey] = useState(existing?.key ?? "");
  const [type, setType] = useState<AttributeType>(existing?.type ?? "string");
  const [description, setDescription] = useState(existing?.description ?? "");
  const [options, setOptions] = useState((existing?.options ?? []).join(", "));
  const [sources, setSources] = useState<Set<MetadataSource>>(new Set(existing?.allowed_sources ?? []));
  const [sensitive, setSensitive] = useState(existing?.sensitive ?? false);
  const [searchable, setSearchable] = useState(existing?.searchable ?? false);

  const keyEdited = key !== "" && key !== deriveKey(label);
  const effectiveKey = existing ? existing.key : keyEdited ? key : deriveKey(label);
  const needsOptions = type === "enum";
  const parsedOptions = options.split(",").map((o) => o.trim()).filter(Boolean);

  const toggleSource = (source: MetadataSource) => {
    setSources((current) => {
      const next = new Set(current);
      if (next.has(source)) next.delete(source);
      else next.add(source);
      return next;
    });
  };

  const save = useMutation<void, AttributeDefinition>(
    () => {
      const body = {
        entity_type: "customer",
        key: effectiveKey,
        label,
        type,
        description: description || null,
        options: needsOptions ? parsedOptions : [],
        allowed_sources: [...sources],
        sensitive,
        searchable,
      };
      return existing
        ? api.patch<AttributeDefinition>(`/attribute-definitions/${existing.id}`, body)
        : api.post<AttributeDefinition>("/attribute-definitions", body);
    },
    { invalidates: [["attribute-definitions", "customer"]], onSuccess: onClose },
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={existing ? "Edit attribute" : "Define an attribute"}
        description="Attributes not declared here are rejected at ingestion."
        footer={
          <Button
            variant="primary"
            size="sm"
            loading={save.isPending}
            disabled={!label.trim() || !effectiveKey}
            onClick={() => void save.mutate().catch(() => {})}
          >
            {existing ? "Save changes" : "Create attribute"}
          </Button>
        }
      >
        {save.error ? (
          <Callout tone="danger" className="mb-3">
            {save.error instanceof ApiError ? save.error.message : "Could not save this attribute."}
          </Callout>
        ) : null}

        <div className="space-y-4 pb-2">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Key" hint={existing ? "Immutable once created." : undefined}>
              <Input mono value={effectiveKey} onChange={(e) => setKey(e.target.value)} placeholder="subscription_plan" disabled={!!existing} />
            </Field>
            <Field label="Display name">
              <Input value={label} onChange={(e) => setLabel(e.target.value)} placeholder="Subscription plan" autoFocus />
            </Field>
          </div>

          <Field label="Type">
            <Select value={type} onValueChange={(v) => setType(v as AttributeType)} options={TYPES} disabled={!!existing} aria-label="Type" />
          </Field>

          {needsOptions && (
            <Field label="Options" hint="Comma-separated.">
              <Input value={options} onChange={(e) => setOptions(e.target.value)} placeholder="starter, growth, enterprise" />
            </Field>
          )}

          <Field label="Description">
            <Textarea rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />
          </Field>

          <Field label="Accepted sources" description="A value arriving from any other source is dropped and logged.">
            <div className="grid gap-2 sm:grid-cols-2">
              {SOURCES.map((source) => (
                <Checkbox key={source.value} label={source.label} checked={sources.has(source.value)} onCheckedChange={() => toggleSource(source.value)} />
              ))}
            </div>
          </Field>

          <Field label="Handling">
            <div className="space-y-3">
              <Checkbox
                label="Searchable"
                description="Indexed for global search. Costs write throughput; enable only where you will actually search."
                checked={searchable}
                onCheckedChange={(c) => setSearchable(c === true)}
              />
              <Checkbox
                label="Sensitive"
                description="Masked in the interface, excluded from search and export, and every reveal is written to the audit log."
                checked={sensitive}
                onCheckedChange={(c) => setSensitive(c === true)}
              />
            </div>
          </Field>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function deriveKey(label: string): string {
  return label
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
}
