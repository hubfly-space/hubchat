import {
  Badge,
  Button,
  Card,
  EmptyState,
  SegmentedControl,
  formatRelativeShort,
  api,
  useQuery,
  type BadgeTone,
} from "@hubchat/shared";
import { Plus, TicketCheck } from "lucide-react";
import { useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { portalErrorMessage, usePortal } from "../portal-context";

const STATUS: Record<string, { label: string; tone: BadgeTone }> = {
  open: { label: "Open", tone: "accent" },
  on_hold: { label: "On hold", tone: "neutral" },
  resolved: { label: "Resolved", tone: "success" },
};

type PortalTicket = {
  id: string;
  number: number;
  prefix: string;
  title: string;
  description: string;
  status: string;
  priority: string;
  conversation_id: string | null;
  created_at: string;
  updated_at: string;
};

export default function Tickets() {
  const [filter, setFilter] = useState<"open" | "all">("open");
  const location = useLocation();
  const { data: portalData } = usePortal();
  const query = useQuery<{ data: PortalTicket[]; has_more: boolean; next_cursor: string | null }>(
    portalData?.viewer ? ["portal", "tickets"] : null,
    (signal) => api.get("/portal/tickets?limit=100", { signal }),
  );

  if (!portalData?.viewer) {
    return <EmptyState icon={TicketCheck} title="Sign in to view your requests" description="Use your email to access requests and replies from this portal." action={<Button variant="primary" size="sm" asChild><Link to={`/sign-in?portal=${encodeURIComponent(portalData?.portal.id ?? "")}&next=${encodeURIComponent(location.pathname + location.search)}`}>Sign in</Link></Button>} />;
  }
  if (query.isLoading) return <div className="py-12 text-center text-sm text-fg-muted">Loading your requests…</div>;
  if (query.isError) return <div className="py-12 text-center text-sm text-danger">{portalErrorMessage(query.error)} <Button className="ml-2" variant="secondary" size="sm" onClick={query.refetch}>Try again</Button></div>;

  const tickets = query.data?.data ?? [];
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
              <li key={ticket.id}>
                <Card interactive className="p-0">
                  <Link to={`/tickets/${ticket.id}`} className="block p-4">
                    <div className="flex flex-wrap items-start justify-between gap-3">
                      <div className="min-w-0">
                        <p className="text-sm font-medium text-fg">{ticket.title}</p>
                        <p className="mt-1 font-mono text-xs text-fg-muted">{ticket.number}</p>
                      </div>
                      <Badge tone={status.tone}>{status.label}</Badge>
                    </div>

                    <p className="mt-2 line-clamp-1 text-xs text-fg-muted">
                      {ticket.description || "No description provided."}
                    </p>

                    <p className="mt-2 text-2xs text-fg-disabled">
                      updated {formatRelativeShort(ticket.updated_at)} ago
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
