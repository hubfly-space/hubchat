import {
  Avatar,
  Badge,
  Button,
  Card,
  CardBody,
  EmptyState,
  Menu,
  MenuContent,
  MenuItem,
  MenuLabel,
  MenuTrigger,
  Page,
  PageBody,
  PageHeader,
  SearchInput,
  SegmentedControl,
  Toolbar,
  cn,
  formatRelativeShort,
  type BadgeTone,
  type FeedbackStatus,
} from "@hubchat/shared";
import { ChevronUp, Combine, Lightbulb, MessageSquare, MoreHorizontal, Settings2 } from "lucide-react";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";
import { NOW, feedbackBoards, feedbackItems } from "../../data/fixtures";

export const STATUS_META: Record<FeedbackStatus, { label: string; tone: BadgeTone }> = {
  open: { label: "Open", tone: "neutral" },
  reviewing: { label: "Reviewing", tone: "info" },
  planned: { label: "Planned", tone: "accent" },
  in_progress: { label: "In progress", tone: "warning" },
  completed: { label: "Completed", tone: "success" },
  declined: { label: "Declined", tone: "danger" },
};

/** A single feedback board (§6.6). */
export default function BoardDetail() {
  const { boardId } = useParams();
  const { customerById } = useWorkspace();
  const [sort, setSort] = useState<"votes" | "recent">("votes");
  const [query, setQuery] = useState("");

  const board = feedbackBoards.find((item) => item.id === boardId);

  if (!board) {
    return (
      <Page>
        <EmptyState icon={Lightbulb} size="lg" title="Board not found" />
      </Page>
    );
  }

  const items = feedbackItems
    .filter((item) => item.board_id === board.id)
    .filter((item) => item.title.toLowerCase().includes(query.toLowerCase()))
    .sort((a, b) =>
      sort === "votes" ? b.vote_count - a.vote_count : b.created_at.localeCompare(a.created_at),
    );

  return (
    <Page>
      <PageHeader
        breadcrumbs={[{ label: "Feedback", href: "/feedback" }, { label: board.name }]}
        title={board.name}
        description={board.description ?? undefined}
        meta={<Badge tone={board.visibility === "public" ? "success" : "neutral"}>{board.visibility}</Badge>}
        actions={
          <>
            <Button variant="secondary" size="sm" leading={<Settings2 />}>
              Board settings
            </Button>
            <Button variant="primary" size="sm">
              Add item
            </Button>
          </>
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
              placeholder="Search this board"
            />
          </div>
        }
        trailing={
          <SegmentedControl
            aria-label="Sort"
            value={sort}
            onValueChange={setSort}
            options={[
              { value: "votes", label: "Top voted" },
              { value: "recent", label: "Newest" },
            ]}
          />
        }
      />

      <PageBody>
        <div className="space-y-2">
          {items.map((item) => {
            const submitter = customerById(item.submitter_id);
            const status = STATUS_META[item.status];

            return (
              <Card key={item.id} className="p-0">
                <CardBody className="flex items-start gap-4">
                  {/* Vote column — the primary signal, so it leads the row. */}
                  <button
                    type="button"
                    aria-label={`Upvote ${item.title}`}
                    className={cn(
                      "flex w-12 shrink-0 flex-col items-center gap-0.5 rounded-md border px-2 py-1.5",
                      "transition-colors",
                      item.viewer_has_voted
                        ? "border-accent-border bg-accent-subtle text-accent-text"
                        : "border-line text-fg-secondary hover:border-line-strong hover:bg-fill",
                    )}
                  >
                    <ChevronUp aria-hidden="true" className="size-4" />
                    <span className="text-sm font-semibold tabular">{item.vote_count}</span>
                  </button>

                  <div className="min-w-0 flex-1">
                    <div className="flex items-start justify-between gap-3">
                      <Link
                        to={`/feedback/items/${item.id}`}
                        className="min-w-0 text-sm font-medium text-fg hover:underline"
                      >
                        {item.title}
                      </Link>
                      <Badge tone={status.tone}>{status.label}</Badge>
                    </div>

                    {item.description && (
                      <p className="mt-1 line-clamp-2 text-xs leading-normal text-fg-muted">
                        {item.description}
                      </p>
                    )}

                    <div className="mt-2 flex items-center gap-3 text-2xs text-fg-muted">
                      {submitter && (
                        <span className="flex items-center gap-1.5">
                          <Avatar name={submitter.name} seed={submitter.id} size="2xs" />
                          {submitter.name}
                        </span>
                      )}
                      <span className="flex items-center gap-1">
                        <MessageSquare aria-hidden="true" className="size-3" />
                        {item.comment_count}
                      </span>
                      <span>{item.subscriber_count} following</span>
                      <span>{formatRelativeShort(item.created_at, NOW)} ago</span>
                    </div>
                  </div>

                  <Menu>
                    <MenuTrigger asChild>
                      <Button
                        variant="ghost"
                        size="xs"
                        iconOnly
                        aria-label="Item actions"
                        leading={<MoreHorizontal />}
                      />
                    </MenuTrigger>
                    <MenuContent align="end">
                      <MenuLabel>Set status</MenuLabel>
                      {(Object.keys(STATUS_META) as FeedbackStatus[]).map((value) => (
                        <MenuItem key={value}>{STATUS_META[value].label}</MenuItem>
                      ))}
                      <MenuItem icon={<Combine />}>Merge into another item…</MenuItem>
                    </MenuContent>
                  </Menu>
                </CardBody>
              </Card>
            );
          })}

          {items.length === 0 && (
            <EmptyState
              icon={Lightbulb}
              title="Nothing on this board yet"
              description="Items arrive from the widget, the portal, the API, or an agent turning part of a conversation into a request."
            />
          )}
        </div>
      </PageBody>
    </Page>
  );
}
