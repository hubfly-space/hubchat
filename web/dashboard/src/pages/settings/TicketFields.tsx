import {
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  DataTable,
  EmptyState,
  Page,
  PageBody,
  PageHeader,
  Section,
  Tooltip,
  type Column,
  type FieldDefinition,
} from "@hubchat/shared";
import { EyeOff, GripVertical, ListChecks, Lock, Pencil, Plus } from "lucide-react";
import { fieldDefinitions } from "../../data/fixtures";

/** Custom ticket fields (§6.3). */
export default function TicketFields() {
  const columns: Column<FieldDefinition>[] = [
    {
      key: "label",
      header: "Field",
      cell: (field) => (
        <div className="flex min-w-0 items-center gap-2">
          <GripVertical aria-hidden="true" className="size-3.5 shrink-0 cursor-grab text-fg-disabled" />
          <div className="min-w-0">
            <p className="truncate text-sm text-fg">{field.label}</p>
            <p className="truncate font-mono text-2xs text-fg-muted">{field.key}</p>
          </div>
        </div>
      ),
    },
    {
      key: "type",
      header: "Type",
      width: "120px",
      cell: (field) => <Badge tone="neutral">{field.type}</Badge>,
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
        field.required ? (
          <Badge tone="accent">Required</Badge>
        ) : (
          <span className="text-xs text-fg-disabled">Optional</span>
        ),
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
          <Button variant="primary" size="sm" leading={<Plus />}>
            New field
          </Button>
        }
      />

      <PageBody width="narrow">
        <Callout tone="info" className="mb-4">
          Field keys are referenced by automation conditions, form definitions, and the API. Renaming
          the label is free; changing the key is not, and Hubchat will warn you before allowing it.
        </Callout>

        <Section title="Fields" description="Order here is the order agents and customers see.">
          <Card>
            <CardBody className="p-0">
              <DataTable
                aria-label="Ticket fields"
                rows={fieldDefinitions}
                columns={columns}
                rowKey={(field) => field.id}
                rowActions={() => (
                  <Button variant="ghost" size="xs" iconOnly aria-label="Edit field" leading={<Pencil />} />
                )}
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
    </Page>
  );
}
