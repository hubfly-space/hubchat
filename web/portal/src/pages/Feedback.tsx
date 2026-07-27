import {
  Badge,
  Button,
  Card,
  CardBody,
  Dialog,
  DialogClose,
  DialogContent,
  DialogTrigger,
  Field,
  Input,
  SegmentedControl,
  Textarea,
  cn,
  type BadgeTone,
} from "@hubchat/shared";
import { ChevronUp, MessageSquare, Plus } from "lucide-react";
import { useState } from "react";
import { feedback } from "../data";

const STATUS: Record<string, { label: string; tone: BadgeTone }> = {
  open: { label: "Open", tone: "neutral" },
  reviewing: { label: "Reviewing", tone: "info" },
  planned: { label: "Planned", tone: "accent" },
  in_progress: { label: "In progress", tone: "warning" },
  completed: { label: "Shipped", tone: "success" },
};

/**
 * Public roadmap and feedback board.
 *
 * Voting is optimistic and local here; against the real API it posts and
 * reconciles. Vote counts are the whole point of the page, so they must respond
 * instantly — a spinner on a vote button reads as a broken button.
 */
export default function Feedback() {
  const [view, setView] = useState<"top" | "roadmap">("top");
  const [votes, setVotes] = useState<Record<string, boolean>>(
    Object.fromEntries(feedback.map((item) => [item.id, item.voted])),
  );

  const toggleVote = (id: string) =>
    setVotes((current) => ({ ...current, [id]: !current[id] }));

  const countFor = (item: (typeof feedback)[number]) =>
    item.votes + (votes[item.id] ? 1 : 0) - (item.voted ? 1 : 0);

  const columns = ["planned", "in_progress", "completed"] as const;

  return (
    <div>
      <header className="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tighter text-fg">Roadmap & ideas</h1>
          <p className="mt-1.5 max-w-xl text-sm leading-normal text-fg-muted">
            Vote on what matters to you. We read every submission, and subscribers hear directly
            when something moves.
          </p>
        </div>

        <Dialog>
          <DialogTrigger asChild>
            <Button variant="primary" size="sm" leading={<Plus />}>
              Suggest something
            </Button>
          </DialogTrigger>
          <DialogContent
            title="Suggest an improvement"
            description="Describe the problem you are hitting rather than the solution you have in mind — it usually leads somewhere better."
            footer={
              <>
                <DialogClose asChild>
                  <Button variant="ghost" size="sm">
                    Cancel
                  </Button>
                </DialogClose>
                <Button variant="primary" size="sm">
                  Submit
                </Button>
              </>
            }
          >
            <div className="space-y-4 pb-2">
              <Field label="Title" required>
                <Input placeholder="Bulk reassign conversations" />
              </Field>
              <Field label="What are you trying to do?" required>
                <Textarea rows={4} placeholder="During handover I have to reassign twenty threads one at a time…" />
              </Field>
            </div>
          </DialogContent>
        </Dialog>
      </header>

      <div className="mb-5">
        <SegmentedControl
          aria-label="View"
          value={view}
          onValueChange={setView}
          options={[
            { value: "top", label: "Top voted" },
            { value: "roadmap", label: "Roadmap" },
          ]}
        />
      </div>

      {view === "top" ? (
        <ul className="space-y-2">
          {[...feedback]
            .sort((a, b) => countFor(b) - countFor(a))
            .map((item) => {
              const status = STATUS[item.status]!;
              const voted = votes[item.id] ?? false;

              return (
                <li key={item.id}>
                  <Card>
                    <CardBody className="flex items-start gap-4">
                      <button
                        type="button"
                        onClick={() => toggleVote(item.id)}
                        aria-pressed={voted}
                        aria-label={`${voted ? "Remove vote from" : "Vote for"} ${item.title}`}
                        className={cn(
                          "flex w-14 shrink-0 flex-col items-center gap-0.5 rounded-md border px-2 py-2",
                          "transition-colors",
                          voted
                            ? "border-accent-border bg-accent-subtle text-accent-text"
                            : "border-line text-fg-secondary hover:border-line-strong hover:bg-fill",
                        )}
                      >
                        <ChevronUp aria-hidden="true" className="size-4" />
                        <span className="text-sm font-semibold tabular">{countFor(item)}</span>
                      </button>

                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-start justify-between gap-2">
                          <p className="text-sm font-medium text-fg">{item.title}</p>
                          <Badge tone={status.tone}>{status.label}</Badge>
                        </div>
                        <p className="mt-1 text-xs leading-normal text-fg-muted">
                          {item.description}
                        </p>
                        <p className="mt-2 flex items-center gap-1.5 text-2xs text-fg-disabled">
                          <MessageSquare aria-hidden="true" className="size-3" />
                          {item.comments} comments
                        </p>
                      </div>
                    </CardBody>
                  </Card>
                </li>
              );
            })}
        </ul>
      ) : (
        <div className="grid gap-4 md:grid-cols-3">
          {columns.map((column) => {
            const items = feedback.filter((item) => item.status === column);
            const status = STATUS[column]!;

            return (
              <section key={column}>
                <header className="mb-3 flex items-center gap-2">
                  <Badge tone={status.tone} dot>
                    {status.label}
                  </Badge>
                  <span className="text-xs tabular text-fg-muted">{items.length}</span>
                </header>

                <div className="space-y-2">
                  {items.map((item) => (
                    <Card key={item.id} className="p-0">
                      <CardBody className="p-3">
                        <p className="text-sm font-medium leading-snug text-fg">{item.title}</p>
                        <p className="mt-1 line-clamp-2 text-xs leading-normal text-fg-muted">
                          {item.description}
                        </p>
                        <p className="mt-2 flex items-center gap-1 text-2xs tabular text-fg-disabled">
                          <ChevronUp aria-hidden="true" className="size-3" />
                          {countFor(item)}
                        </p>
                      </CardBody>
                    </Card>
                  ))}

                  {items.length === 0 && (
                    <p className="rounded-lg border border-dashed border-line px-3 py-6 text-center text-xs text-fg-muted">
                      Nothing here yet
                    </p>
                  )}
                </div>
              </section>
            );
          })}
        </div>
      )}
    </div>
  );
}
