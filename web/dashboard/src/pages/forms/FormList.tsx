import {
  Badge,
  Button,
  Card,
  CardBody,
  CopyField,
  EmptyState,
  Page,
  PageBody,
  PageHeader,
  Section,
  formatCompact,
  formatRelativeShort,
} from "@hubchat/shared";
import { ClipboardList, Plus, Settings2 } from "lucide-react";
import { Link } from "react-router-dom";
import { NOW, forms } from "../../data/fixtures";

/** Intake forms (§6.11). */
export default function FormList() {
  return (
    <Page>
      <PageHeader
        title="Forms"
        description="Reusable intake for bug reports, refunds, access requests, and anything else with a fixed shape."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />}>
            New form
          </Button>
        }
      />

      <PageBody>
        <Section>
          {forms.length === 0 ? (
            <EmptyState
              icon={ClipboardList}
              title="No forms yet"
              description="A form turns a vague message into a structured ticket with the fields you actually need."
            />
          ) : (
            <div className="space-y-3">
              {forms.map((form) => (
                <Card key={form.id}>
                  <CardBody>
                    <div className="flex flex-wrap items-start gap-4">
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <Link
                            to={`/forms/${form.id}`}
                            className="truncate text-sm font-medium text-fg hover:underline"
                          >
                            {form.name}
                          </Link>
                          <Badge tone={form.access === "public" ? "success" : "neutral"}>
                            {form.access}
                          </Badge>
                          {!form.enabled && <Badge tone="warning">Disabled</Badge>}
                        </div>
                        <p className="mt-1 text-xs text-fg-muted">
                          Creates a {form.purpose} · {form.fields.length} fields ·{" "}
                          {formatCompact(form.submission_count)} submissions · updated{" "}
                          {formatRelativeShort(form.updated_at, NOW)} ago
                        </p>
                      </div>

                      <Button variant="secondary" size="sm" leading={<Settings2 />} asChild>
                        <Link to={`/forms/${form.id}`}>Edit</Link>
                      </Button>
                    </div>

                    <div className="mt-3 max-w-lg">
                      <CopyField
                        label="URL"
                        value={`https://help.northwind.cloud/f/${form.slug}`}
                      />
                    </div>
                  </CardBody>
                </Card>
              ))}
            </div>
          )}
        </Section>
      </PageBody>
    </Page>
  );
}
