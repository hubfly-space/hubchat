import {
  Badge,
  Button,
  Card,
  CardBody,
  EmptyState,
  Page,
  PageBody,
  PageHeader,
  SearchInput,
  Section,
  Toolbar,
  formatCompact,
  formatRelativeShort,
} from "@hubchat/shared";
import { ListChecks, Pencil, Plus, Zap } from "lucide-react";
import { useState } from "react";
import { NOW, macros } from "../../data/fixtures";

/** Macros (§6.13) — one click, several actions. */
export default function MacroList() {
  const [query, setQuery] = useState("");
  const rows = macros.filter((macro) => macro.name.toLowerCase().includes(query.toLowerCase()));

  return (
    <Page>
      <PageHeader
        title="Macros"
        description="Bundles of actions an agent applies in one keystroke. Unlike rules, a macro only runs when a human triggers it."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />}>
            New macro
          </Button>
        }
      />

      <Toolbar
        leading={
          <div className="w-64">
            <SearchInput
              inputSize="sm"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              onClear={() => setQuery("")}
              placeholder="Search macros"
            />
          </div>
        }
      />

      <PageBody>
        <Section>
          {rows.length === 0 ? (
            <EmptyState
              icon={ListChecks}
              title="No macros"
              description="If your team types the same reply and then makes the same three field changes, that is a macro."
            />
          ) : (
            <div className="space-y-3">
              {rows.map((macro) => (
                <Card key={macro.id}>
                  <CardBody>
                    <div className="flex flex-wrap items-start gap-4">
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="truncate text-sm font-medium text-fg">{macro.name}</span>
                          <Badge tone="neutral">{macro.scope}</Badge>
                          {macro.folder && <Badge tone="neutral" variant="outline">{macro.folder}</Badge>}
                        </div>

                        {macro.body && (
                          <p className="mt-1.5 line-clamp-2 rounded-md border-l-2 border-line-strong bg-inset px-2.5 py-1.5 text-xs leading-normal text-fg-secondary">
                            {macro.body}
                          </p>
                        )}

                        <div className="mt-2 flex flex-wrap items-center gap-1.5">
                          {macro.actions.map((action) => (
                            <span
                              key={action.id}
                              className="inline-flex items-center gap-1 rounded-sm bg-fill px-1.5 py-0.5 text-2xs text-fg-secondary"
                            >
                              <Zap aria-hidden="true" className="size-2.5 text-accent-text" />
                              {action.type.replace(/_/g, " ")}
                            </span>
                          ))}
                        </div>

                        <p className="mt-2 text-2xs tabular text-fg-disabled">
                          Used {formatCompact(macro.usage_count)}× · updated{" "}
                          {formatRelativeShort(macro.updated_at, NOW)} ago
                        </p>
                      </div>

                      <Button variant="secondary" size="sm" leading={<Pencil />}>
                        Edit
                      </Button>
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
