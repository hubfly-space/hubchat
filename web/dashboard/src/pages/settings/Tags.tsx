import {
  api,
  ApiError,
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
  Pagination,
  SearchInput,
  Section,
  Select,
  TagChip,
  Toolbar,
  cn,
  formatCompact,
  useMutation,
  useInfinite,
  type Column,
  type Paginated,
  type Tag,
} from "@hubchat/shared";
import { Combine, Plus, Tags as TagsIcon, Trash2 } from "lucide-react";
import { useState } from "react";

const COLORS = [1, 2, 3, 4, 5, 6] as const;

/** Workspace tags (§6.1). */
export default function Tags() {
  const [query, setQuery] = useState("");
  const [merging, setMerging] = useState<Tag | null>(null);
  const [deleting, setDeleting] = useState<Tag | null>(null);

  const tags = useInfinite<Tag>(["tags", query], (cursor, signal) => {
    const params = new URLSearchParams({ q: query, limit: "50" });
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<Tag>>(`/tags?${params.toString()}`, { signal });
  });
  const rows = tags.items;

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
      <PageHeader title="Tags" description="Free-form labels for conversations, tickets, customers, and feedback." actions={<CreateTagDialog />} />

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
          {tags.isLoading ? <p className="p-4 text-sm text-fg-muted">Loading tags…</p> : tags.error ? <Callout tone="danger">{tags.error instanceof ApiError ? tags.error.message : "Could not load tags."}</Callout> : (
            <>
              <Card>
                <CardBody className="p-0">
                  <DataTable
                    aria-label="Tags"
                    rows={rows}
                    columns={columns}
                    rowKey={(tag) => tag.id}
                    rowActions={(tag) => (
                      <div className="flex gap-0.5">
                        <Button
                          variant="ghost"
                          size="xs"
                          iconOnly
                          aria-label={`Merge ${tag.name}`}
                          leading={<Combine />}
                          onClick={() => setMerging(tag)}
                        />
                        <Button
                          variant="ghost"
                          size="xs"
                          iconOnly
                          aria-label={`Delete ${tag.name}`}
                          leading={<Trash2 />}
                          onClick={() => setDeleting(tag)}
                        />
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
              {tags.hasMore && <Pagination hasPrevious={false} hasNext onPrevious={() => undefined} onNext={() => void tags.fetchNext()} summary={`${rows.length} tags loaded`} />}
            </>
          )}
        </Section>
      </PageBody>

      {merging ? (
        <MergeTagDialog tag={merging} others={rows.filter((t) => t.id !== merging.id)} onClose={() => setMerging(null)} />
      ) : null}
      {deleting ? <DeleteTagDialog tag={deleting} onClose={() => setDeleting(null)} /> : null}
    </Page>
  );
}

function CreateTagDialog() {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [color, setColor] = useState<(typeof COLORS)[number]>(1);

  const create = useMutation<{ name: string; color: number }, unknown>(
    (body) => api.post("/tags", body),
    {
      invalidates: [["tags"]],
      onSuccess: () => {
        setOpen(false);
        setName("");
        setColor(1);
      },
    },
  );

  return (
    <Dialog open={open} onOpenChange={setOpen}>
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
            <Button
              variant="primary"
              size="sm"
              loading={create.isPending}
              disabled={!name.trim()}
              onClick={() => void create.mutate({ name: name.trim(), color }).catch(() => {})}
            >
              Create tag
            </Button>
          </>
        }
      >
        <div className="space-y-4 pb-2">
          {create.error ? (
            <Callout tone="danger">
              {create.error instanceof ApiError ? create.error.message : "Could not create that tag."}
            </Callout>
          ) : null}

          <Field label="Name" description="Renaming updates every record.">
            <Input mono placeholder="churn-risk" value={name} onChange={(event) => setName(event.target.value)} autoFocus />
          </Field>

          <Field
            label="Colour"
            description="Tags draw from the same six-slot palette as charts, so a workspace never accumulates a hundred hues."
          >
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
  );
}

function MergeTagDialog({ tag, others, onClose }: { tag: Tag; others: Tag[]; onClose: () => void }) {
  const [intoId, setIntoId] = useState(others[0]?.id ?? "");

  const merge = useMutation<string, unknown>((intoTagId) => api.post(`/tags/${tag.id}/merge`, { into_tag_id: intoTagId }), {
    invalidates: [["tags"]],
    onSuccess: onClose,
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={`Merge "${tag.name}"`}
        description="Every record tagged with this tag is repointed at the tag you choose below, then this tag is deleted."
        footer={
          <>
            <DialogClose asChild>
              <Button variant="ghost" size="sm">
                Cancel
              </Button>
            </DialogClose>
            <Button
              variant="primary"
              size="sm"
              loading={merge.isPending}
              disabled={!intoId}
              onClick={() => void merge.mutate(intoId).catch(() => {})}
            >
              Merge
            </Button>
          </>
        }
      >
        {merge.error ? (
          <Callout tone="danger" className="mb-3">
            {merge.error instanceof ApiError ? merge.error.message : "Could not merge that tag."}
          </Callout>
        ) : null}
        {others.length === 0 ? (
          <EmptyState size="sm" title="No other tags to merge into" />
        ) : (
          <Field label="Merge into">
            <Select
              aria-label="Merge into"
              value={intoId}
              onValueChange={setIntoId}
              options={others.map((other) => ({ value: other.id, label: other.name }))}
            />
          </Field>
        )}
      </DialogContent>
    </Dialog>
  );
}

function DeleteTagDialog({ tag, onClose }: { tag: Tag; onClose: () => void }) {
  const remove = useMutation<void, unknown>(() => api.delete(`/tags/${tag.id}`), {
    invalidates: [["tags"]],
    onSuccess: onClose,
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={`Delete "${tag.name}"?`}
        footer={
          <>
            <DialogClose asChild>
              <Button variant="ghost" size="sm">
                Cancel
              </Button>
            </DialogClose>
            <Button variant="danger" size="sm" loading={remove.isPending} onClick={() => void remove.mutate().catch(() => {})}>
              Delete tag
            </Button>
          </>
        }
      >
        <p className="text-sm text-fg-muted">
          Removes this tag from all {formatCompact(tag.usage_count)} records it is currently on. This cannot be
          undone.
        </p>
        {remove.error ? (
          <Callout tone="danger" className="mt-3">
            {remove.error instanceof ApiError ? remove.error.message : "Could not delete this tag."}
          </Callout>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
