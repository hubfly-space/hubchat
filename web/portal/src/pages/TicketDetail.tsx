import {
  Avatar,
  Badge,
  Breadcrumbs,
  Button,
  Card,
  CardBody,
  Callout,
  EmptyState,
  Textarea,
  cn,
  formatDateTime,
  useToast,
  type BadgeTone,
} from "@hubchat/shared";
import { Download, Paperclip, TicketCheck } from "lucide-react";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { tickets, viewer } from "../data";

const STATUS: Record<string, { label: string; tone: BadgeTone }> = {
  open: { label: "Open", tone: "accent" },
  on_hold: { label: "On hold", tone: "neutral" },
  resolved: { label: "Resolved", tone: "success" },
};

/**
 * Ticket thread as the customer sees it.
 *
 * Note what is absent: no internal notes, no assignee, no SLA countdown, no
 * priority. Those exist on the same conversation for the agent, and the portal
 * API never sends them — the omission is enforced server-side, not by leaving
 * them out of this template.
 */
export default function TicketDetail() {
  const { number } = useParams();
  const toast = useToast();
  const [reply, setReply] = useState("");

  const ticket = tickets.find((item) => item.number === number);

  if (!ticket) {
    return (
      <EmptyState
        icon={TicketCheck}
        size="lg"
        title="Request not found"
        description="It may belong to a different account, or the link may have expired."
        action={
          <Button variant="secondary" size="sm" asChild>
            <Link to="/tickets">Back to your requests</Link>
          </Button>
        }
      />
    );
  }

  const status = STATUS[ticket.status]!;

  return (
    <div className="mx-auto max-w-3xl">
      <Breadcrumbs
        className="mb-4"
        items={[{ label: "Your requests", href: "/tickets" }, { label: ticket.number }]}
      />

      <header className="mb-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <h1 className="text-2xl font-semibold tracking-tighter text-fg">{ticket.title}</h1>
          <Badge tone={status.tone}>{status.label}</Badge>
        </div>
        <p className="mt-2 font-mono text-xs text-fg-muted">
          {ticket.number} · opened {formatDateTime(ticket.created)}
        </p>
      </header>

      {ticket.status === "resolved" && (
        <Callout tone="success" className="mb-5">
          This request was resolved. Replying below reopens it — no need to start a new one.
        </Callout>
      )}

      {/* Thread ---------------------------------------------------------- */}
      <ol className="space-y-4">
        {ticket.messages.map((message) => {
          const mine = message.from === "you";
          return (
            <li
              key={message.id}
              className={cn("flex gap-3", mine && "flex-row-reverse")}
            >
              <Avatar
                name={message.name}
                seed={mine ? viewer.email : message.name}
                size="sm"
                className="mt-0.5 shrink-0"
              />

              <div className={cn("min-w-0 max-w-[85%]", mine && "text-right")}>
                <p className="mb-1 text-2xs text-fg-muted">
                  <span className="text-fg-secondary">{mine ? "You" : message.name}</span>
                  {" · "}
                  {formatDateTime(message.at)}
                </p>

                <div
                  className={cn(
                    "rounded-lg border px-3.5 py-2.5 text-left",
                    mine
                      ? "border-accent-border bg-accent-subtle"
                      : "border-line bg-surface",
                  )}
                >
                  <p className="whitespace-pre-wrap text-sm leading-normal text-fg">
                    {message.body}
                  </p>

                  {"attachment" in message && message.attachment && (
                    <a
                      href="#"
                      className="mt-2.5 flex items-center gap-2 rounded-md border border-line bg-inset px-2.5 py-2 transition-colors hover:border-line-strong"
                    >
                      <Paperclip aria-hidden="true" className="size-3.5 shrink-0 text-fg-muted" />
                      <span className="min-w-0 flex-1 truncate text-xs text-fg">
                        {message.attachment}
                      </span>
                      <Download aria-hidden="true" className="size-3.5 shrink-0 text-fg-muted" />
                    </a>
                  )}
                </div>
              </div>
            </li>
          );
        })}
      </ol>

      {/* Reply ----------------------------------------------------------- */}
      <Card className="mt-8">
        <CardBody>
          <Textarea
            autoResize
            rows={4}
            value={reply}
            onChange={(event) => setReply(event.target.value)}
            placeholder={
              ticket.status === "resolved"
                ? "Reply to reopen this request…"
                : "Add to this request…"
            }
            aria-label="Your reply"
          />

          <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
            <Button variant="ghost" size="sm" leading={<Paperclip />}>
              Attach a file
            </Button>

            <Button
              variant="primary"
              size="sm"
              disabled={!reply.trim()}
              onClick={() => {
                toast.success({
                  title: "Reply sent",
                  description: "The team has been notified and will get back to you.",
                });
                setReply("");
              }}
            >
              {ticket.status === "resolved" ? "Reply and reopen" : "Send reply"}
            </Button>
          </div>
        </CardBody>
      </Card>

      <p className="mt-4 text-center text-2xs text-fg-muted">
        You are signed in as {viewer.email} ·{" "}
        <Link to="/account" className="text-accent-text hover:underline">
          Manage notifications
        </Link>
      </p>
    </div>
  );
}
