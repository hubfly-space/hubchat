import {
  Badge,
  Button,
  Card,
  CardBody,
  EmptyState,
  Page,
  PageBody,
  PageHeader,
  Section,
  formatCompact,
  formatRelativeShort,
} from "@hubchat/shared";
import { Eye, EyeOff, Lightbulb, Lock, MessageSquare, Plus, ThumbsUp } from "lucide-react";
import { Link } from "react-router-dom";
import { NOW, feedbackBoards, feedbackItems } from "../../data/fixtures";

const VISIBILITY = {
  public: { label: "Public", tone: "success", icon: Eye },
  private: { label: "Private", tone: "neutral", icon: Lock },
  invite_only: { label: "Invite only", tone: "warning", icon: EyeOff },
} as const;

/** Feedback boards (§6.6). */
export default function BoardList() {
  return (
    <Page>
      <PageHeader
        title="Feedback boards"
        description="Structured product feedback with voting, moderation, and status updates — not a second inbox."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />}>
            New board
          </Button>
        }
      />

      <PageBody>
        <Section>
          {feedbackBoards.length === 0 ? (
            <EmptyState
              icon={Lightbulb}
              title="No boards yet"
              description="A board collects requests on one theme. Most teams start with a single public board and split it once volume justifies it."
              action={
                <Button variant="primary" size="sm" leading={<Plus />}>
                  Create a board
                </Button>
              }
            />
          ) : (
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
              {feedbackBoards.map((board) => {
                const visibility = VISIBILITY[board.visibility];
                const VisibilityIcon = visibility.icon;
                const topItems = feedbackItems
                  .filter((item) => item.board_id === board.id)
                  .sort((a, b) => b.vote_count - a.vote_count)
                  .slice(0, 3);

                return (
                  <Card key={board.id} interactive className="p-0">
                    <Link to={`/feedback/boards/${board.id}`} className="block">
                      <CardBody>
                        <div className="flex items-start justify-between gap-2">
                          <h3 className="text-sm font-semibold text-fg">{board.name}</h3>
                          <Badge tone={visibility.tone} leading={<VisibilityIcon />}>
                            {visibility.label}
                          </Badge>
                        </div>

                        <p className="mt-1 line-clamp-2 text-xs leading-normal text-fg-muted">
                          {board.description}
                        </p>

                        <div className="mt-3 flex items-center gap-3 text-2xs tabular text-fg-muted">
                          <span className="flex items-center gap-1">
                            <Lightbulb aria-hidden="true" className="size-3" />
                            {formatCompact(board.item_count)} items
                          </span>
                          {board.allow_voting && (
                            <span className="flex items-center gap-1">
                              <ThumbsUp aria-hidden="true" className="size-3" />
                              voting
                            </span>
                          )}
                          {board.allow_comments && (
                            <span className="flex items-center gap-1">
                              <MessageSquare aria-hidden="true" className="size-3" />
                              comments
                            </span>
                          )}
                        </div>

                        {topItems.length > 0 && (
                          <ul className="mt-3 space-y-1 border-t border-line-subtle pt-3">
                            {topItems.map((item) => (
                              <li
                                key={item.id}
                                className="flex items-center gap-2 text-xs text-fg-secondary"
                              >
                                <span className="w-8 shrink-0 text-right tabular text-fg-muted">
                                  {item.vote_count}
                                </span>
                                <span className="min-w-0 flex-1 truncate">{item.title}</span>
                              </li>
                            ))}
                          </ul>
                        )}

                        <p className="mt-3 text-2xs text-fg-disabled">
                          Created {formatRelativeShort(board.created_at, NOW)} ago ·{" "}
                          {board.moderation === "none"
                            ? "no moderation"
                            : `${board.moderation}-moderation`}
                        </p>
                      </CardBody>
                    </Link>
                  </Card>
                );
              })}
            </div>
          )}
        </Section>
      </PageBody>
    </Page>
  );
}
