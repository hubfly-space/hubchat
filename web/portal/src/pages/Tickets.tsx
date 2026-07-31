import {
  Badge,
  Button,
  Card,
  EmptyState,
  SegmentedControl,
  formatRelativeShort,
  type BadgeTone,
} from "@hubchat/shared";
import { Plus, TicketCheck } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { NOW, tickets } from "../data";

const STATUS: Record<string, { label: string; tone: BadgeTone }> = {
  open: { label: "Open", tone: "accent" },
  on_hold: { label: "On hold", tone: "neutral" },
  resolved: { label: "Resolved", tone: "success" },
};

export default function Tickets() {
  const [filter, setFilter] = useState<"open" | "all">("open");

  const visible = tickets.filter((ticket) =>
    filter === "open" ? ticket.status !== "resolved" : true,
  );

  return (
    <div>
      <header className="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tighter text-fg">Your requests</h1>
          <p className="mt-1.5 text-sm text-fg-muted">
            Everything you have sent us, and where it stands.
          </p>
        </div>
        <Button variant="primary" size="sm" leading={<Plus />} asChild>
          <Link to="/tickets/new">New request</Link>
        </Button>
      </header>

      <div className="mb-4">
        <SegmentedControl
          aria-label="Filter requests"
          value={filter}
          onValueChange={setFilter}
          options={[
            { value: "open", label: "Open" },
            { value: "all", label: "All" },
          ]}
        />
      </div>

      {visible.length === 0 ? (
        <EmptyState
          icon={TicketCheck}
          title="No requests here"
          description="When you send us something, it appears here so you can follow along."
          action={
            <Button variant="primary" size="sm" asChild>
              <Link to="/tickets/new">Send a request</Link>
            </Button>
          }
        />
      ) : (
        <ul className="space-y-2">
          {visible.map((ticket) => {
            const status = STATUS[ticket.status]!;
            return (
              <li key={ticket.number}>
                <Card interactive className="p-0">
                  <Link to={`/tickets/${ticket.number}`} className="block p-4">
                    <div className="flex flex-wrap items-start justify-between gap-3">
                      <div className="min-w-0">
                        <p className="text-sm font-medium text-fg">{ticket.title}</p>
                        <p className="mt-1 font-mono text-xs text-fg-muted">{ticket.number}</p>
                      </div>
                      <Badge tone={status.tone}>{status.label}</Badge>
                    </div>

                    <p className="mt-2 line-clamp-1 text-xs text-fg-muted">
                      {ticket.messages[ticket.messages.length - 1]?.body}
                    </p>

                    <p className="mt-2 text-2xs text-fg-disabled">
                      {ticket.messages.length} message{ticket.messages.length === 1 ? "" : "s"} ·
                      updated {formatRelativeShort(ticket.updated, NOW)} ago
                    </p>
                  </Link>
                </Card>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
