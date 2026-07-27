import {
  Avatar,
  Badge,
  Button,
  Card,
  CardBody,
  Page,
  PageBody,
  PageHeader,
  Switch,
  cn,
  formatCompact,
  type FeedbackStatus,
} from "@hubchat/shared";
import { ChevronUp, Megaphone, MessageSquare } from "lucide-react";
import { Link } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";
import { feedbackItems } from "../../data/fixtures";
import { STATUS_META } from "./BoardDetail";

/** The three columns customers see on a public roadmap (§6.6). */
const COLUMNS: FeedbackStatus[] = ["planned", "in_progress", "completed"];

export default function Roadmap() {
  const { customerById } = useWorkspace();

  return (
    <Page>
      <PageHeader
        title="Roadmap"
        description="A public view of what is planned, in progress, and shipped. Moving an item between columns notifies its subscribers."
        actions={
          <>
            <Switch label="Publish publicly" defaultChecked />
            <Button variant="secondary" size="sm" leading={<Megaphone />}>
              Publish changelog entry
            </Button>
          </>
        }
      />

      <PageBody width="full">
        <div className="grid gap-4 lg:grid-cols-3">
          {COLUMNS.map((status) => {
            const items = feedbackItems
              .filter((item) => item.status === status)
              .sort((a, b) => b.vote_count - a.vote_count);
            const meta = STATUS_META[status];

            return (
              <section key={status} className="flex min-w-0 flex-col">
                <header className="mb-3 flex items-center gap-2">
                  <Badge tone={meta.tone} dot>
                    {meta.label}
                  </Badge>
                  <span className="text-xs tabular text-fg-muted">{items.length}</span>
                </header>

                <div className="flex flex-col gap-2">
                  {items.map((item) => {
                    const submitter = customerById(item.submitter_id);
                    return (
                      <Card key={item.id} interactive className="p-0">
                        <Link to={`/feedback/items/${item.id}`} className="block">
                          <CardBody className="p-3">
                            <p className="text-sm font-medium leading-snug text-fg">{item.title}</p>
                            {item.description && (
                              <p className="mt-1 line-clamp-2 text-xs leading-normal text-fg-muted">
                                {item.description}
                              </p>
                            )}
                            <div className="mt-2.5 flex items-center gap-3 text-2xs text-fg-muted">
                              <span
                                className={cn(
                                  "flex items-center gap-1 rounded-sm px-1.5 py-0.5 tabular",
                                  "bg-fill text-fg-secondary",
                                )}
                              >
                                <ChevronUp aria-hidden="true" className="size-3" />
                                {formatCompact(item.vote_count)}
                              </span>
                              <span className="flex items-center gap-1">
                                <MessageSquare aria-hidden="true" className="size-3" />
                                {item.comment_count}
                              </span>
                              {submitter && (
                                <span className="ml-auto">
                                  <Avatar name={submitter.name} seed={submitter.id} size="2xs" />
                                </span>
                              )}
                            </div>
                          </CardBody>
                        </Link>
                      </Card>
                    );
                  })}

                  {items.length === 0 && (
                    <div className="rounded-lg border border-dashed border-line px-3 py-8 text-center text-xs text-fg-muted">
                      Nothing here yet
                    </div>
                  )}
                </div>
              </section>
            );
          })}
        </div>
      </PageBody>
    </Page>
  );
}
