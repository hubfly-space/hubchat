import {
  Badge,
  Button,
  Card,
  EmptyState,
  Pagination,
  SegmentedControl,
  formatRelativeShort,
  api,
  useInfinite,
  type Paginated,
  type BadgeTone,
} from "@hubchat/shared";
import { Plus, TicketCheck } from "lucide-react";
import { useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { portalErrorMessage, usePortal } from "../portal-context";
import { portalText } from "../i18n";

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
  const t = (key: string, fallback: string, values?: Record<string, string | number>) => portalText(portalData, key, fallback, values);
  const query = useInfinite<PortalTicket>(
    portalData?.viewer ? ["portal", "tickets"] : null,
    (cursor, signal) => {
      const params = new URLSearchParams({ limit: "25" });
      if (cursor) params.set("cursor", cursor);
      return api.get<Paginated<PortalTicket>>(`/portal/tickets?${params.toString()}`, { signal });
    },
  );

  if (!portalData?.viewer) {
    return <EmptyState icon={TicketCheck} title="Sign in to view your requests" description="Use your email to access requests and replies from this portal." action={<Button variant="primary" size="sm" asChild><Link to={`/sign-in?portal=${encodeURIComponent(portalData?.portal.id ?? "")}&next=${encodeURIComponent(location.pathname + location.search)}`}>{t("sign_in", "Sign in")}</Link></Button>} />;
  }
  if (query.isLoading) return <div className="py-12 text-center text-sm text-fg-muted">{t("loading_requests", "Loading your requests…")}</div>;
  if (query.error) return <div className="py-12 text-center text-sm text-danger">{portalErrorMessage(query.error)} <Button className="ml-2" variant="secondary" size="sm" onClick={query.refetch}>{t("try_again", "Try again")}</Button></div>;

  const tickets = query.items;
  const visible = tickets.filter((ticket) =>
    filter === "open" ? ticket.status !== "resolved" : true,
  );

  return (
    <div>
      <header className="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tighter text-fg">{t("your_requests", "Your requests")}</h1>
          <p className="mt-1.5 text-sm text-fg-muted">
            {t("requests_description", "Everything you have sent us, and where it stands.")}
          </p>
        </div>
        <Button variant="primary" size="sm" leading={<Plus />} asChild>
          <Link to="/tickets/new">{t("new_request", "New request")}</Link>
        </Button>
      </header>

      <div className="mb-4">
        <SegmentedControl
          aria-label={t("filter_requests", "Filter requests")}
          value={filter}
          onValueChange={setFilter}
          options={[
            { value: "open", label: t("open", "Open") },
            { value: "all", label: t("all", "All") },
          ]}
        />
      </div>

      {visible.length === 0 ? (
        <EmptyState
          icon={TicketCheck}
          title={t("no_requests", "No requests here")}
          description={t("follow_requests", "When you send us something, it appears here so you can follow along.")}
          action={
            <Button variant="primary" size="sm" asChild>
              <Link to="/tickets/new">{t("send_request", "Send a request")}</Link>
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
                      {ticket.description || t("no_description", "No description provided.")}
                    </p>

                    <p className="mt-2 text-2xs text-fg-disabled">
                      {t("updated", "updated {time} ago", { time: formatRelativeShort(ticket.updated_at) })}
                    </p>
                  </Link>
                </Card>
              </li>
            );
          })}
        </ul>
      )}

      <Pagination
        hasPrevious={false}
        hasNext={query.hasMore}
        onPrevious={() => undefined}
        onNext={() => void query.fetchNext()}
        summary={t("requests_loaded", "{count} request{suffix} loaded", { count: tickets.length, suffix: tickets.length === 1 ? "" : "s" })}
      />
    </div>
  );
}
