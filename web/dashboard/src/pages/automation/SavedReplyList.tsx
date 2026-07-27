import {
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  DataTable,
  EmptyState,
  Kbd,
  Page,
  PageBody,
  PageHeader,
  SearchInput,
  Section,
  Toolbar,
  formatCompact,
  type Column,
  type SavedReply,
} from "@hubchat/shared";
import { MessageSquareReply, Pencil, Plus } from "lucide-react";
import { useState } from "react";
import { savedReplies } from "../../data/fixtures";

/** Saved replies (§6.13) — text only, no side effects. */
export default function SavedReplyList() {
  const [query, setQuery] = useState("");
  const rows = savedReplies.filter((reply) =>
    `${reply.name} ${reply.body}`.toLowerCase().includes(query.toLowerCase()),
  );

  const columns: Column<SavedReply>[] = [
    {
      key: "name",
      header: "Name",
      cell: (reply) => (
        <div className="min-w-0">
          <p className="truncate text-sm text-fg">{reply.name}</p>
          <p className="truncate text-xs text-fg-muted">{reply.body}</p>
        </div>
      ),
      sortable: true,
    },
    {
      key: "shortcut",
      header: "Shortcut",
      width: "110px",
      cell: (reply) =>
        reply.shortcut ? (
          <code className="rounded-xs bg-fill px-1 py-0.5 font-mono text-2xs text-fg-secondary">
            {reply.shortcut}
          </code>
        ) : (
          <span className="text-xs text-fg-disabled">—</span>
        ),
    },
    {
      key: "folder",
      header: "Folder",
      width: "130px",
      hideBelow: "md",
      cell: (reply) => reply.folder ?? <span className="text-fg-disabled">—</span>,
    },
    {
      key: "scope",
      header: "Scope",
      width: "110px",
      cell: (reply) => <Badge tone="neutral">{reply.scope}</Badge>,
    },
    {
      key: "usage_count",
      header: "Used",
      width: "80px",
      numeric: true,
      cell: (reply) => formatCompact(reply.usage_count),
      sortable: true,
    },
  ];

  return (
    <Page>
      <PageHeader
        title="Saved replies"
        description="Reusable text snippets. Insert with the composer's shortcut menu or by typing the slash command."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />}>
            New reply
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
              placeholder="Search replies"
            />
          </div>
        }
      />

      <PageBody>
        <Callout tone="info" className="mb-4">
          Replies support variables: <code className="font-mono">{"{{customer.name}}"}</code>,{" "}
          <code className="font-mono">{"{{ticket.number}}"}</code>,{" "}
          <code className="font-mono">{"{{agent.first_name}}"}</code>. An unresolved variable blocks
          sending rather than rendering as literal braces to the customer.
        </Callout>

        <Section>
          <Card>
            <CardBody className="p-0">
              <DataTable
                aria-label="Saved replies"
                rows={rows}
                columns={columns}
                rowKey={(reply) => reply.id}
                rowActions={() => (
                  <Button variant="ghost" size="xs" iconOnly aria-label="Edit" leading={<Pencil />} />
                )}
                empty={
                  <EmptyState
                    icon={MessageSquareReply}
                    title="No saved replies"
                    description="Start with the three sentences your team types most."
                  />
                }
              />
            </CardBody>
          </Card>
        </Section>

        <p className="mt-4 flex items-center gap-1.5 text-xs text-fg-muted">
          Open the reply picker from any composer with <Kbd keys="mod+/" />
        </p>
      </PageBody>
    </Page>
  );
}
