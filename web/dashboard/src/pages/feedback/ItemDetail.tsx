import {
  Avatar,
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  DetailRow,
  EmptyState,
  Page,
  PageBody,
  PageHeader,
  Section,
  Select,
  Textarea,
  cn,
  formatRelativeShort,
  type FeedbackStatus,
} from "@hubchat/shared";
import { Bell, ChevronUp, Combine, Lightbulb, Link2, Send } from "lucide-react";
import { useState } from "react";
import { useParams } from "react-router-dom";
import { useWorkspace } from "../../app/workspace-context";
import { NOW, feedbackBoards, feedbackItems } from "../../data/fixtures";
import { STATUS_META } from "./BoardDetail";

export default function ItemDetail() {
  const { itemId } = useParams();
  const { customerById, companyById } = useWorkspace();
  const [status, setStatus] = useState<FeedbackStatus>("open");
  const [update, setUpdate] = useState("");

  const item = feedbackItems.find((entry) => entry.id === itemId);

  if (!item) {
    return (
      <Page>
        <EmptyState icon={Lightbulb} size="lg" title="Feedback item not found" />
      </Page>
    );
  }

  const board = feedbackBoards.find((entry) => entry.id === item.board_id);
  const submitter = customerById(item.submitter_id);
  const company = companyById(item.company_id);
  const meta = STATUS_META[item.status];

  return (
    <Page>
      <PageHeader
        breadcrumbs={[
          { label: "Feedback", href: "/feedback" },
          { label: board?.name ?? "Board", href: `/feedback/boards/${item.board_id}` },
          { label: item.title },
        ]}
        title={item.title}
        meta={<Badge tone={meta.tone}>{meta.label}</Badge>}
        actions={
          <>
            <Button variant="secondary" size="sm" leading={<Link2 />}>
              Link a conversation
            </Button>
            <Button variant="secondary" size="sm" leading={<Combine />}>
              Merge
            </Button>
          </>
        }
      />

      <PageBody width="full">
        <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_300px]">
          <div className="min-w-0 space-y-5">
            <Card>
              <CardBody className="flex gap-4">
                <div
                  className={cn(
                    "flex w-14 shrink-0 flex-col items-center gap-0.5 rounded-md border px-2 py-2",
                    "border-line text-fg-secondary",
                  )}
                >
                  <ChevronUp aria-hidden="true" className="size-4" />
                  <span className="text-md font-semibold tabular">{item.vote_count}</span>
                  <span className="text-2xs text-fg-muted">votes</span>
                </div>

                <div className="min-w-0 flex-1">
                  {submitter && (
                    <p className="mb-2 flex items-center gap-2 text-xs text-fg-muted">
                      <Avatar name={submitter.name} seed={submitter.id} size="xs" />
                      <span className="text-fg-secondary">{submitter.name}</span>
                      {company && <span>· {company.name}</span>}
                      <span>· {formatRelativeShort(item.created_at, NOW)} ago</span>
                    </p>
                  )}
                  <p className="whitespace-pre-wrap text-sm leading-normal text-fg">
                    {item.description || "No description was provided."}
                  </p>
                </div>
              </CardBody>
            </Card>

            <Section title="Post a status update">
              <Callout tone="info" className="mb-3">
                Status updates are emailed to all {item.subscriber_count} subscribers and shown on
                the public board. Changing status without a message sends the status change alone.
              </Callout>

              <Card>
                <CardBody>
                  <div className="mb-3 w-52">
                    <Select
                      size="sm"
                      value={status}
                      onValueChange={(value) => setStatus(value as FeedbackStatus)}
                      aria-label="New status"
                      options={(Object.keys(STATUS_META) as FeedbackStatus[]).map((value) => ({
                        value,
                        label: STATUS_META[value].label,
                      }))}
                    />
                  </div>
                  <Textarea
                    autoResize
                    rows={3}
                    value={update}
                    onChange={(event) => setUpdate(event.target.value)}
                    placeholder="Explain what changed and roughly when customers can expect it."
                    aria-label="Status update message"
                  />
                  <div className="mt-2 flex justify-end">
                    <Button variant="primary" size="sm" trailing={<Send />} disabled={!update.trim()}>
                      Post update
                    </Button>
                  </div>
                </CardBody>
              </Card>
            </Section>

            <Section title={`Comments (${item.comment_count})`}>
              <Card>
                <CardBody className="p-0">
                  <ul className="divide-y divide-line-subtle">
                    {[
                      { id: 1, name: "Mariana Costa", official: false, body: "This would save our ops team about an hour a day during handover." },
                      { id: 2, name: "Rui Ferreira", official: true, body: "Picking this up in the next cycle. We will start with reassignment and add bulk tagging after." },
                      { id: 3, name: "Daniel Osei", official: false, body: "+1 — we hit this every Monday morning." },
                    ].map((comment) => (
                      <li key={comment.id} className="flex gap-3 px-4 py-3">
                        <Avatar name={comment.name} size="sm" />
                        <div className="min-w-0 flex-1">
                          <p className="flex items-center gap-2 text-xs">
                            <span className="font-medium text-fg">{comment.name}</span>
                            {comment.official && <Badge tone="accent">Team</Badge>}
                          </p>
                          <p className="mt-1 text-sm leading-normal text-fg-secondary">
                            {comment.body}
                          </p>
                        </div>
                      </li>
                    ))}
                  </ul>
                </CardBody>
              </Card>
            </Section>
          </div>

          <aside className="space-y-4">
            <Card>
              <CardHeader title="Details" />
              <CardBody>
                <dl>
                  <DetailRow label="Board">{board?.name}</DetailRow>
                  <DetailRow label="Type">{item.type.replace(/_/g, " ")}</DetailRow>
                  <DetailRow label="Visibility">{item.visibility}</DetailRow>
                  <DetailRow label="Votes">{item.vote_count}</DetailRow>
                  <DetailRow label="Subscribers">{item.subscriber_count}</DetailRow>
                  <DetailRow label="Created">
                    {formatRelativeShort(item.created_at, NOW)} ago
                  </DetailRow>
                </dl>
              </CardBody>
            </Card>

            <Card>
              <CardHeader title="Subscribers" description="Notified on every status change." />
              <CardBody>
                <Button variant="secondary" size="sm" fullWidth leading={<Bell />}>
                  Notify {item.subscriber_count} subscribers
                </Button>
              </CardBody>
            </Card>
          </aside>
        </div>
      </PageBody>
    </Page>
  );
}
