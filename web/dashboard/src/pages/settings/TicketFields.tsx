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
  invalidate,
  useMutation,
  useQuery,
  type Column,
  type FieldDefinition,
  type FieldType,
} from "@hubchat/shared";
import { ArrowDown, ArrowUp, EyeOff, ListChecks, Lock, Pencil, Plus, Trash2 } from "lucide-react";
import { useState } from "react";

const FIELD_TYPES: { value: FieldType; label: string }[] = [
  { value: "string", label: "Text" },
  { value: "text", label: "Long text" },
  { value: "integer", label: "Integer" },
  { value: "decimal", label: "Decimal" },
  { value: "boolean", label: "Yes / no" },
  { value: "date", label: "Date" },
  { value: "timestamp", label: "Date & time" },
  { value: "enum", label: "Single choice" },
  { value: "multi_enum", label: "Multiple choice" },
  { value: "string_list", label: "List of text" },
  { value: "url", label: "URL" },
  { value: "email", label: "Email" },
  { value: "phone", label: "Phone" },
];

/** Custom ticket fields (§6.10). */
export default function TicketFields() {
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<FieldDefinition | null>(null);

  const fields = useQuery<{ data: FieldDefinition[] }>(
    ["field-definitions", "ticket"],
    (signal) => api.get(`/field-definitions?entity_type=ticket`, { signal }),
  );
  const definitions = fields.data?.data ?? [];

  const archive = useMutation<string, unknown>(
    (id) => api.delete(`/field-definitions/${id}`),
    { invalidates: [["field-definitions", "ticket"]] },
  );

  const move = async (index: number, direction: -1 | 1) => {
    const target = index + direction;
    if (target < 0 || target >= definitions.length) return;
    const reordered = [...definitions];
    const [item] = reordered.splice(index, 1);
    reordered.splice(target, 0, item!);
    await api.put("/field-definitions/reorder", { ordered_ids: reordered.map((d) => d.id) });
    invalidate(["field-definitions", "ticket"]);
  };

  const columns: Column<FieldDefinition>[] = [
    {
      key: "label",
      header: "Field",
      cell: (field) => (
        <div className="min-w-0">
          <p className="truncate text-sm text-fg">{field.label}</p>
          <p className="truncate font-mono text-2xs text-fg-muted">{field.key}</p>
        </div>
      ),
    },
    {
      key: "type",
      header: "Type",
      width: "130px",
      cell: (field) => <Badge tone="neutral">{FIELD_TYPES.find((t) => t.value === field.type)?.label ?? field.type}</Badge>,
    },
    {
      key: "visibility",
      header: "Visibility",
      width: "140px",
      cell: (field) =>
        field.visibility === "internal" ? (
          <Tooltip content="Never rendered on the portal, and excluded from customer-facing API responses">
            <span>
              <Badge tone="warning" leading={<Lock />}>
                Internal
              </Badge>
            </span>
          </Tooltip>
        ) : (
          <Badge tone="success">Customer visible</Badge>
        ),
    },
    {
      key: "required",
      header: "Required",
      width: "100px",
      align: "center",
      hideBelow: "md",
      cell: (field) =>
        field.required ? <Badge tone="accent">Required</Badge> : <span className="text-xs text-fg-disabled">Optional</span>,
    },
    {
      key: "sensitive",
      header: "Sensitive",
      width: "110px",
      hideBelow: "lg",
      cell: (field) =>
        field.sensitive ? (
          <Badge tone="warning" leading={<EyeOff />}>
            Masked
          </Badge>
        ) : (
          <span className="text-xs text-fg-disabled">—</span>
        ),
    },
  ];

  return (
    <Page>
      <PageHeader
        title="Ticket fields"
        description="Structured data captured on every ticket, beyond the built-in title, status, and priority."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />} onClick={() => setCreating(true)}>
            New field
          </Button>
        }
      />

      <PageBody width="narrow">
        <Callout tone="info" className="mb-4">
          Field keys are referenced by automation conditions, form definitions, and the API. The key is set once at
          creation and cannot be changed afterward — archive the field and create a new one if the key itself needs
          to change.
        </Callout>

        <Section title="Fields" description="Order here is the order agents and customers see.">
          <Card>
            <CardBody className="p-0">
              <DataTable
                aria-label="Ticket fields"
                rows={definitions}
                columns={columns}
                rowKey={(field) => field.id}
                loading={fields.isLoading}
                rowActions={(field) => {
                  const index = definitions.findIndex((d) => d.id === field.id);
                  return (
                  <div className="flex items-center gap-0.5">
                    <Button
                      variant="ghost"
                      size="xs"
                      iconOnly
                      aria-label="Move up"
                      disabled={index === 0}
                      leading={<ArrowUp />}
                      onClick={() => void move(index, -1)}
                    />
                    <Button
                      variant="ghost"
                      size="xs"
                      iconOnly
                      aria-label="Move down"
                      disabled={index === definitions.length - 1}
                      leading={<ArrowDown />}
                      onClick={() => void move(index, 1)}
                    />
                    <Button variant="ghost" size="xs" iconOnly aria-label="Edit field" leading={<Pencil />} onClick={() => setEditing(field)} />
                    <Button
                      variant="ghost"
                      size="xs"
                      iconOnly
                      aria-label="Archive field"
                      leading={<Trash2 />}
                      onClick={() => void archive.mutate(field.id).catch(() => {})}
                    />
                  </div>
                  );
                }}
                empty={
                  <EmptyState
                    icon={ListChecks}
                    title="No custom fields"
                    description="Add one when your team starts asking the same follow-up question every time."
                  />
                }
              />
            </CardBody>
          </Card>
        </Section>
      </PageBody>

      {creating && <FieldDialog onClose={() => setCreating(false)} />}
      {editing && <FieldDialog existing={editing} onClose={() => setEditing(null)} />}
    </Page>
  );
}

function FieldDialog({ existing, onClose }: { existing?: FieldDefinition; onClose: () => void }) {
  const [label, setLabel] = useState(existing?.label ?? "");
  const [key, setKey] = useState(existing?.key ?? "");
  const [type, setType] = useState<FieldType>(existing?.type ?? "string");
  const [description, setDescription] = useState(existing?.description ?? "");
  const [options, setOptions] = useState((existing?.options ?? []).join(", "));
  const [required, setRequired] = useState(existing?.required ?? false);
  const [visibility, setVisibility] = useState<"internal" | "public">(existing?.visibility ?? "internal");
  const [sensitive, setSensitive] = useState(existing?.sensitive ?? false);
  const [searchable, setSearchable] = useState(existing?.searchable ?? false);

  const keyEdited = key !== "" && key !== deriveKey(label);
  const effectiveKey = existing ? existing.key : keyEdited ? key : deriveKey(label);

  const needsOptions = type === "enum" || type === "multi_enum";
  const parsedOptions = options
    .split(",")
    .map((o) => o.trim())
    .filter(Boolean);

  const save = useMutation<void, FieldDefinition>(
    () => {
      const body = {
        entity_type: "ticket",
        key: effectiveKey,
        label,
        type,
        description: description || null,
        options: needsOptions ? parsedOptions : [],
        required,
        visibility,
        sensitive,
        searchable,
      };
      return existing
        ? api.patch<FieldDefinition>(`/field-definitions/${existing.id}`, body)
        : api.post<FieldDefinition>("/field-definitions", body);
    },
    {
      invalidates: [["field-definitions", "ticket"]],
      onSuccess: onClose,
    },
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={existing ? "Edit field" : "New field"}
        footer={
          <Button
            variant="primary"
            size="sm"
            loading={save.isPending}
            disabled={!label.trim() || !effectiveKey}
            onClick={() => void save.mutate().catch(() => {})}
          >
            {existing ? "Save changes" : "Create field"}
          </Button>
        }
      >
        {save.error ? (
          <Callout tone="danger" className="mb-3">
            {save.error instanceof ApiError ? save.error.message : "Could not save this field."}
          </Callout>
        ) : null}

        <div className="flex flex-col gap-3">
          <Field label="Label">
            <Input value={label} onChange={(e) => setLabel(e.target.value)} placeholder="Account ID" autoFocus />
          </Field>
          <Field label="Key" hint={existing ? "Immutable once created." : "Auto-generated from the label; edit if you need something different."}>
            <Input
              value={effectiveKey}
              onChange={(e) => setKey(e.target.value)}
              disabled={!!existing}
              className="font-mono"
            />
          </Field>
          <Field label="Type">
            <Select
              value={type}
              onValueChange={(v) => setType(v as FieldType)}
              options={FIELD_TYPES.map((t) => ({ value: t.value, label: t.label }))}
              disabled={!!existing}
              aria-label="Type"
            />
          </Field>
          {needsOptions && (
            <Field label="Options" hint="Comma-separated.">
              <Input value={options} onChange={(e) => setOptions(e.target.value)} placeholder="low, medium, high" />
            </Field>
          )}
          <Field label="Description">
            <Textarea rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />
          </Field>
          <Field label="Visibility">
            <Select
              value={visibility}
              onValueChange={(v) => setVisibility(v as "internal" | "public")}
              options={[
                { value: "internal", label: "Internal — agents only" },
                { value: "public", label: "Public — visible to the customer" },
              ]}
              aria-label="Visibility"
            />
          </Field>
          <Checkbox label="Required" checked={required} onCheckedChange={(c) => setRequired(c === true)} />
          <Checkbox
            label="Sensitive"
            description="Masked in the UI and excluded from search and export."
            checked={sensitive}
            onCheckedChange={(c) => setSensitive(c === true)}
          />
          <Checkbox label="Searchable" checked={searchable} onCheckedChange={(c) => setSearchable(c === true)} />
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
