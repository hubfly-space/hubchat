import {
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  Checkbox,
  EmptyState,
  Field,
  Input,
  Menu,
  MenuContent,
  MenuItem,
  MenuLabel,
  MenuTrigger,
  Page,
  Section,
  Select,
  Switch,
  Textarea,
  Toolbar,
  Tooltip,
  cn,
} from "@hubchat/shared";
import {
  AlignLeft,
  Calendar,
  ChevronDown,
  CircleDot,
  ClipboardList,
  GripVertical,
  Hash,
  ListChecks,
  Mail,
  Paperclip,
  Plus,
  Star,
  Trash2,
  Type,
} from "lucide-react";
import { useState } from "react";
import { useParams } from "react-router-dom";
import { forms, inboxes, tags, teams } from "../../data/fixtures";
import type { FormField } from "@hubchat/shared";

const FIELD_TYPES = [
  { type: "string", label: "Short text", icon: Type },
  { type: "text", label: "Long text", icon: AlignLeft },
  { type: "email", label: "Email", icon: Mail },
  { type: "integer", label: "Number", icon: Hash },
  { type: "date", label: "Date", icon: Calendar },
  { type: "enum", label: "Dropdown", icon: ChevronDown },
  { type: "multi_enum", label: "Multi-select", icon: ListChecks },
  { type: "boolean", label: "Checkbox", icon: CircleDot },
  { type: "file", label: "File upload", icon: Paperclip },
  { type: "rating", label: "Rating", icon: Star },
] as const;

/**
 * Form builder (§6.11).
 *
 * Editor on the left, live preview on the right. The preview is the actual
 * customer-facing renderer, not an approximation — a builder whose preview
 * lies is worse than no preview.
 */
export default function FormBuilder() {
  const { formId } = useParams();
  const form = forms.find((item) => item.id === formId) ?? forms[0]!;
  const [fields, setFields] = useState<FormField[]>(form.fields);
  const [activeId, setActiveId] = useState<string | null>(form.fields[0]?.id ?? null);

  const active = fields.find((field) => field.id === activeId);

  const addField = (type: (typeof FIELD_TYPES)[number]["type"]) => {
    const field: FormField = {
      id: `ff_${Math.random().toString(36).slice(2, 8)}`,
      key: `field_${fields.length + 1}`,
      label: "Untitled field",
      type,
      placeholder: null,
      description: null,
      options: type === "enum" || type === "multi_enum" ? ["Option one", "Option two"] : null,
      required: false,
      default_value: null,
      condition: null,
      validation: null,
    };
    setFields((current) => [...current, field]);
    setActiveId(field.id);
  };

  return (
    <Page>
      <Toolbar
        className="h-topbar py-0"
        leading={
          <>
            <span className="truncate text-sm font-medium text-fg">{form.name}</span>
            <Badge tone={form.enabled ? "success" : "warning"}>
              {form.enabled ? "Live" : "Disabled"}
            </Badge>
          </>
        }
        trailing={
          <>
            <Button variant="ghost" size="sm">
              Preview
            </Button>
            <Button variant="secondary" size="sm">
              Discard
            </Button>
            <Button variant="primary" size="sm">
              Save
            </Button>
          </>
        }
      />

      <div className="flex min-h-0 flex-1">
        {/* Field list ---------------------------------------------------- */}
        <div className="flex w-list shrink-0 flex-col overflow-y-auto border-r border-line bg-surface">
          <div className="p-3">
            <p className="mb-2 text-2xs font-semibold uppercase tracking-caps text-fg-muted">
              Fields
            </p>

            <ul className="flex flex-col gap-1">
              {fields.map((field) => {
                const meta = FIELD_TYPES.find((item) => item.type === field.type);
                const Icon = meta?.icon ?? Type;
                return (
                  <li key={field.id}>
                    <button
                      type="button"
                      onClick={() => setActiveId(field.id)}
                      className={cn(
                        "flex w-full items-center gap-2 rounded-md border px-2 py-2 text-left transition-colors",
                        field.id === activeId
                          ? "border-accent-border bg-accent-subtle"
                          : "border-transparent hover:bg-fill",
                      )}
                    >
                      <GripVertical aria-hidden="true" className="size-3.5 shrink-0 text-fg-disabled" />
                      <Icon aria-hidden="true" className="size-3.5 shrink-0 text-fg-muted" />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-xs text-fg">{field.label}</span>
                        <span className="block truncate font-mono text-2xs text-fg-muted">
                          {field.key}
                        </span>
                      </span>
                      {field.required && <span className="text-danger-text">*</span>}
                      {field.condition && (
                        <Tooltip content="Conditional — only shown when its rule passes">
                          <Badge tone="system">if</Badge>
                        </Tooltip>
                      )}
                    </button>
                  </li>
                );
              })}
            </ul>

            {fields.length === 0 && (
              <EmptyState
                icon={ClipboardList}
                size="sm"
                title="No fields yet"
                description="Add the first question below."
              />
            )}

            <Menu>
              <MenuTrigger asChild>
                <Button variant="secondary" size="sm" fullWidth className="mt-3" leading={<Plus />}>
                  Add field
                </Button>
              </MenuTrigger>
              <MenuContent className="w-56">
                <MenuLabel>Field type</MenuLabel>
                {FIELD_TYPES.map((item) => (
                  <MenuItem
                    key={item.type}
                    icon={<item.icon />}
                    onSelect={() => addField(item.type)}
                  >
                    {item.label}
                  </MenuItem>
                ))}
              </MenuContent>
            </Menu>
          </div>
        </div>

        {/* Field editor -------------------------------------------------- */}
        <div className="min-w-0 flex-1 overflow-y-auto bg-canvas">
          <div className="mx-auto max-w-2xl px-6 py-6">
            {active ? (
              <>
                <Section title="Field" description={`Key: ${active.key}`}>
                  <Card>
                    <CardBody className="space-y-4">
                      <Field label="Label" htmlFor="field-label">
                        <Input id="field-label" defaultValue={active.label} />
                      </Field>

                      <Field
                        label="Key"
                        htmlFor="field-key"
                        description="Used in the API payload and in automation conditions. Avoid changing it after launch."
                      >
                        <Input id="field-key" mono defaultValue={active.key} />
                      </Field>

                      <Field label="Help text" htmlFor="field-help">
                        <Textarea id="field-help" rows={2} defaultValue={active.description ?? ""} />
                      </Field>

                      {active.options && (
                        <Field label="Options" description="One per line.">
                          <Textarea rows={4} defaultValue={active.options.join("\n")} />
                        </Field>
                      )}

                      <Checkbox label="Required" defaultChecked={active.required} />
                    </CardBody>
                  </Card>
                </Section>

                <Section
                  title="Conditional display"
                  description="Show this field only when an earlier answer matches."
                >
                  <Card>
                    <CardBody>
                      {active.condition ? (
                        <Callout tone="system">
                          Shown when <code className="font-mono">{active.condition.field}</code>{" "}
                          {active.condition.operator.replace(/_/g, " ")}{" "}
                          <code className="font-mono">{String(active.condition.value)}</code>
                        </Callout>
                      ) : (
                        <Button variant="secondary" size="sm" leading={<Plus />}>
                          Add a condition
                        </Button>
                      )}
                    </CardBody>
                  </Card>
                </Section>

                <div className="flex justify-end">
                  <Button
                    variant="danger-ghost"
                    size="sm"
                    leading={<Trash2 />}
                    onClick={() => {
                      setFields((current) => current.filter((field) => field.id !== active.id));
                      setActiveId(null);
                    }}
                  >
                    Delete field
                  </Button>
                </div>
              </>
            ) : (
              <>
                <Section title="Routing" description="Where submissions land, and how they are labelled.">
                  <Card>
                    <CardBody className="space-y-4">
                      <Field label="Inbox" htmlFor="routing-inbox">
                        <Select
                          id="routing-inbox"
                          defaultValue={form.routing.inbox_id ?? undefined}
                          options={inboxes.map((inbox) => ({ value: inbox.id, label: inbox.name }))}
                          aria-label="Inbox"
                        />
                      </Field>
                      <Field label="Team" htmlFor="routing-team">
                        <Select
                          id="routing-team"
                          defaultValue={form.routing.team_id ?? undefined}
                          options={teams.map((team) => ({ value: team.id, label: team.name }))}
                          aria-label="Team"
                        />
                      </Field>
                      <Field label="Automatic tags">
                        <div className="flex flex-wrap gap-1.5">
                          {tags.slice(0, 4).map((tag) => (
                            <Checkbox
                              key={tag.id}
                              label={tag.name}
                              defaultChecked={form.routing.tag_ids.includes(tag.id)}
                            />
                          ))}
                        </div>
                      </Field>
                    </CardBody>
                  </Card>
                </Section>

                <Section title="Access & protection">
                  <Card>
                    <CardBody className="space-y-4">
                      <Switch
                        label="Require the customer to be signed in"
                        description="Anonymous submissions are rejected."
                        defaultChecked={form.access === "authenticated"}
                      />
                      <Switch
                        label="Spam protection"
                        description="Rate limits by IP and requires a proof-of-work token on public forms."
                        defaultChecked={form.spam_protection}
                      />
                    </CardBody>
                  </Card>
                </Section>
              </>
            )}
          </div>
        </div>

        {/* Live preview -------------------------------------------------- */}
        <aside className="hidden w-[360px] shrink-0 overflow-y-auto border-l border-line bg-sunken p-4 xl:block">
          <p className="mb-3 text-2xs font-semibold uppercase tracking-caps text-fg-muted">
            Customer preview
          </p>

          <Card variant="raised">
            <CardHeader title={form.name} description="Fields marked with * are required." />
            <CardBody className="space-y-4">
              {fields.map((field) => (
                <Field
                  key={field.id}
                  label={field.label}
                  required={field.required}
                  description={field.description ?? undefined}
                >
                  {field.type === "text" ? (
                    <Textarea rows={3} placeholder={field.placeholder ?? ""} />
                  ) : field.type === "enum" ? (
                    <Select
                      options={(field.options ?? []).map((option) => ({
                        value: option,
                        label: option,
                      }))}
                      aria-label={field.label}
                    />
                  ) : field.type === "file" ? (
                    <div className="rounded-md border border-dashed border-line px-3 py-6 text-center text-xs text-fg-muted">
                      Drop a file or click to browse
                    </div>
                  ) : (
                    <Input placeholder={field.placeholder ?? ""} />
                  )}
                </Field>
              ))}

              <Button variant="primary" size="md" fullWidth>
                Submit
              </Button>
            </CardBody>
          </Card>
        </aside>
      </div>
    </Page>
  );
}
