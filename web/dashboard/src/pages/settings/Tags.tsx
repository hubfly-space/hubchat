import {
  Button,
  Callout,
  Card,
  CardBody,
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
  SearchInput,
  Section,
  TagChip,
  Toolbar,
  cn,
  formatCompact,
  type Column,
  type Tag,
} from "@hubchat/shared";
import { Combine, Plus, Tags as TagsIcon, Trash2 } from "lucide-react";
import { useState } from "react";
import { tags } from "../../data/fixtures";

const COLORS = [1, 2, 3, 4, 5, 6] as const;

/** Workspace tags (§6.1). */
export default function Tags() {
  const [query, setQuery] = useState("");
  const [color, setColor] = useState<(typeof COLORS)[number]>(1);

  const rows = tags.filter((tag) => tag.name.includes(query.toLowerCase()));

  const columns: Column<Tag>[] = [
    {
      key: "name",
      header: "Tag",
      cell: (tag) => <TagChip label={tag.name} color={tag.color} />,
      sortable: true,
    },
    {
      key: "usage_count",
      header: "Used on",
      width: "120px",
      numeric: true,
      cell: (tag) => `${formatCompact(tag.usage_count)} records`,
      sortable: true,
    },
  ];

  return (
    <Page>
      <PageHeader
        title="Tags"
        description="Free-form labels for conversations, tickets, customers, and feedback."
        actions={
          <Dialog>
            <DialogTrigger asChild>
              <Button variant="primary" size="sm" leading={<Plus />}>
                New tag
              </Button>
            </DialogTrigger>
            <DialogContent
              title="Create a tag"
              description="Tags are workspace-wide. Anyone who can tag a record can use them."
              footer={
                <>
                  <DialogClose asChild>
                    <Button variant="ghost" size="sm">
                      Cancel
                    </Button>
                  </DialogClose>
                  <Button variant="primary" size="sm">
                    Create tag
                  </Button>
                </>
              }
            >
              <div className="space-y-4 pb-2">
                <Field label="Name" description="Lowercase, hyphenated. Renaming updates every record.">
                  <Input mono placeholder="churn-risk" />
                </Field>

                <Field label="Colour" description="Tags draw from the same six-slot palette as charts, so a workspace never accumulates a hundred hues.">
                  <div className="flex gap-2">
                    {COLORS.map((slot) => (
                      <button
                        key={slot}
                        type="button"
                        onClick={() => setColor(slot)}
                        aria-label={`Colour ${slot}`}
                        className={cn(
                          "size-8 rounded-md border-2 transition-transform hover:scale-105",
                          color === slot ? "border-line-loud" : "border-transparent",
                        )}
                        style={{ backgroundColor: `var(--hc-chart-${slot})` }}
                      />
                    ))}
                  </div>
                </Field>
              </div>
            </DialogContent>
          </Dialog>
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
              placeholder="Search tags"
            />
          </div>
        }
      />

      <PageBody width="narrow">
        <Callout tone="info" className="mb-4">
          Deleting a tag removes it from every record it is on. Merging is usually what you want —
          it repoints every usage at the surviving tag and keeps the history intact.
        </Callout>

        <Section>
          <Card>
            <CardBody className="p-0">
              <DataTable
                aria-label="Tags"
                rows={rows}
                columns={columns}
                rowKey={(tag) => tag.id}
                rowActions={() => (
                  <div className="flex gap-0.5">
                    <Button variant="ghost" size="xs" iconOnly aria-label="Merge" leading={<Combine />} />
                    <Button variant="ghost" size="xs" iconOnly aria-label="Delete" leading={<Trash2 />} />
                  </div>
                )}
                empty={
                  <EmptyState
                    icon={TagsIcon}
                    title="No tags"
                    description="Tags earn their keep once you have enough volume to need slicing. There is no rush."
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
