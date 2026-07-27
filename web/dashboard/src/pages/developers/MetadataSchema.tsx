import {
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  Checkbox,
  CodeBlock,
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
  Select,
  Switch,
  Tooltip,
  type Column,
  type FieldDefinition,
} from "@hubchat/shared";
import { Braces, EyeOff, Plus, ShieldAlert } from "lucide-react";
import { fieldDefinitions } from "../../data/fixtures";

const SOURCES = [
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

/**
 * Metadata schema (§6.10).
 *
 * The allowlist. Nothing reaches a customer record unless it is declared here,
 * which is what makes §3.3 ("collection must be explicit, allowlisted,
 * documented, and controllable") enforceable rather than aspirational.
 */
export default function MetadataSchema() {
  const columns: Column<FieldDefinition>[] = [
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
      cell: (field) => <Badge tone="neutral">{field.type}</Badge>,
    },
    {
      key: "sources",
      header: "Accepted from",
      width: "220px",
      hideBelow: "lg",
      cell: (field) => (
        <Tooltip content={field.allowed_sources.join(", ")}>
          <span className="flex flex-wrap gap-1">
            {field.allowed_sources.slice(0, 2).map((source) => (
              <Badge key={source} tone="neutral" variant="outline">
                {source}
              </Badge>
            ))}
            {field.allowed_sources.length > 2 && (
              <span className="text-2xs text-fg-muted">+{field.allowed_sources.length - 2}</span>
            )}
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
      cell: (field) =>
        field.searchable ? (
          <Badge tone="success">Yes</Badge>
        ) : (
          <span className="text-xs text-fg-disabled">No</span>
        ),
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
          <Dialog>
            <DialogTrigger asChild>
              <Button variant="primary" size="sm" leading={<Plus />}>
                New attribute
              </Button>
            </DialogTrigger>
            <DialogContent
              title="Define an attribute"
              description="Attributes not declared here are rejected at ingestion with a 422."
              size="lg"
              footer={
                <>
                  <DialogClose asChild>
                    <Button variant="ghost" size="sm">
                      Cancel
                    </Button>
                  </DialogClose>
                  <Button variant="primary" size="sm">
                    Create attribute
                  </Button>
                </>
              }
            >
              <div className="space-y-4 pb-2">
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="Key" description="Immutable once data exists.">
                    <Input mono placeholder="subscription_plan" />
                  </Field>
                  <Field label="Display name">
                    <Input placeholder="Subscription plan" />
                  </Field>
                </div>

                <Field label="Type">
                  <Select
                    aria-label="Type"
                    options={[
                      { value: "string", label: "String" },
                      { value: "integer", label: "Integer" },
                      { value: "decimal", label: "Decimal" },
                      { value: "boolean", label: "Boolean" },
                      { value: "timestamp", label: "Timestamp" },
                      { value: "date", label: "Date" },
                      { value: "enum", label: "Enum" },
                      { value: "string_list", label: "String list" },
                      { value: "url", label: "URL" },
                      { value: "json", label: "JSON object", description: "Size and depth limited" },
                    ]}
                  />
                </Field>

                <Field
                  label="Accepted sources"
                  description="A value arriving from any other source is dropped and logged."
                >
                  <div className="grid gap-2 sm:grid-cols-2">
                    {SOURCES.map((source) => (
                      <Checkbox key={source.value} label={source.label} />
                    ))}
                  </div>
                </Field>

                <Field label="Handling">
                  <div className="space-y-3">
                    <Switch
                      label="Searchable"
                      description="Indexed for global search. Costs write throughput; enable only where you will actually search."
                    />
                    <Switch
                      label="Sensitive"
                      description="Masked in the interface, excluded from search and export, and every reveal is written to the audit log."
                    />
                  </div>
                </Field>
              </div>
            </DialogContent>
          </Dialog>
        }
      />

      <PageBody>
        <Callout tone="warning" className="mb-5" icon={<ShieldAlert />}>
          Marking an attribute sensitive changes behaviour retroactively: existing values are masked
          immediately, dropped from the search index on the next rebuild, and excluded from every
          future export.
        </Callout>

        <Section title="Attributes">
          <Card>
            <CardBody className="p-0">
              <DataTable
                aria-label="Metadata attributes"
                rows={fieldDefinitions}
                columns={columns}
                rowKey={(field) => field.id}
                empty={
                  <EmptyState
                    icon={Braces}
                    title="No attributes defined"
                    description="Declare the attributes your application will send before wiring up the SDK."
                  />
                }
              />
            </CardBody>
          </Card>
        </Section>

        <Section title="Privacy controls" description="Applied across every ingestion path (§12).">
          <Card>
            <CardBody className="space-y-4">
              <Switch
                label="Reject undeclared keys"
                description="Recommended. When off, unknown keys are dropped silently instead of returning an error — harder to debug, and easy to leave broken."
                defaultChecked
              />
              <Switch
                label="Anonymise IP addresses"
                description="Stores only the /24 network for IPv4 and /48 for IPv6."
                defaultChecked
              />
              <Field
                label="Blocked key patterns"
                description="Keys matching any of these are refused regardless of the schema."
              >
                <Input mono defaultValue="*password*, *secret*, *token*, *ssn*, *card_number*" />
              </Field>
              <Field label="Maximum payload size" description="Per event, after JSON encoding.">
                <Input type="number" suffix="KB" defaultValue={32} className="max-w-32" />
              </Field>
            </CardBody>
          </Card>
        </Section>

        <Section title="Setting attributes">
          <CodeBlock
            language="javascript"
            code={`// Browser — only keys whose accepted sources include js_sdk are applied.
Hubchat('update', {
  plan: 'enterprise',
  seats: 240,
  region: 'eu',
});`}
          />
          <CodeBlock
            className="mt-2"
            language="bash"
            code={`# Server — the only path permitted to set sensitive attributes.
curl -X PATCH https://support.northwind.cloud/api/v1/customers/cus_mariana \\
  -H "Authorization: Bearer hc_live_9f2a…" \\
  -d '{"attributes":{"tax_id":"PT123456789"}}'`}
          />
        </Section>
      </PageBody>
    </Page>
  );
}
