import {
  api,
  Badge,
  Button,
  Callout,
  CodeBlock,
  EmptyState,
  Page,
  PageBody,
  PageHeader,
  Pagination,
  SearchInput,
  Section,
  StatusDot,
  Toolbar,
  cn,
  useInfinite,
  useQuery,
  useRealtimeEvents,
  formatRelativeShort,
  type Customer,
  type Paginated,
} from "@hubchat/shared";
import { Activity, Pause, Play } from "lucide-react";
import { useState } from "react";

type CustomerEvent = {
  id: string;
  workspace_id: string;
  customer_id: string | null;
  session_id: string | null;
  type: string;
  source: string;
  url: string | null;
  payload: Record<string, unknown>;
  occurred_at: string;
};

/**
 * Live event stream (§6.10).
 *
 * The developer's equivalent of tailing a log: confirm the SDK is firing
 * what you think it is, with the payload you think it has, attributed to the
 * customer you think it is.
 */
export default function EventStream() {
  const [live, setLive] = useState(true);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<string | null>(null);

  const list = useInfinite<CustomerEvent>(["events"], (cursor, signal) => {
    const params = new URLSearchParams();
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<CustomerEvent>>(`/events?${params.toString()}`, { signal });
  });

  // Live means "refetch the head of the stream when a new event lands" — a
  // real subscription, not a cosmetic pulse; pausing stops calling refetch
  // rather than closing the socket, so resuming doesn't miss the gap.
  useRealtimeEvents((event) => {
    if (live && event.type === "event.received") void list.refetch();
  });

  const customerIds = [...new Set(list.items.map((e) => e.customer_id).filter((id): id is string => !!id))];
  const customers = useQuery<{ data: Customer[] }>(
    customerIds.length > 0 ? ["customers", "by-ids", customerIds.join(",")] : null,
    (signal) => api.get(`/customers?ids=${customerIds.join(",")}`, { signal }),
  );
  const customerById = new Map((customers.data?.data ?? []).map((c) => [c.id, c]));

  const rows = list.items.filter((event) =>
    `${event.type} ${JSON.stringify(event.payload)}`.toLowerCase().includes(query.toLowerCase()),
  );
  const active = rows.find((event) => event.id === selected);

  return (
    <Page>
      <PageHeader
        title="Event stream"
        description="Application events arriving from the SDK, the REST API, and the widget. Sensitive payload keys are redacted unless your role allows sensitive customer access."
        actions={
          <Button
            variant={live ? "secondary" : "primary"}
            size="sm"
            leading={live ? <Pause /> : <Play />}
            onClick={() => setLive((current) => !current)}
          >
            {live ? "Pause stream" : "Resume"}
          </Button>
        }
      />

      <Toolbar
        leading={
          <>
            <span className="flex items-center gap-1.5 text-xs text-fg-muted">
              <StatusDot status={live ? "live" : "offline"} pulse={live} />
              {live ? "Streaming" : "Paused"}
            </span>
            <div className="w-72">
              <SearchInput
                inputSize="sm"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                onClear={() => setQuery("")}
                placeholder="Filter loaded events by type or payload"
              />
            </div>
          </>
        }
      />

      <div className="flex min-h-0 flex-1">
        <div className="min-w-0 flex-1 overflow-y-auto">
          {rows.length === 0 ? (
            <EmptyState
              icon={Activity}
              title="No events"
              description="Call Hubchat('track', 'your.event', { … }) from your application to see it appear here."
            />
          ) : (
            <ul className="divide-y divide-line-subtle">
              {rows.map((event) => {
                const customer = event.customer_id ? customerById.get(event.customer_id) : undefined;
                const failed = event.type.includes("fail");

                return (
                  <li key={event.id}>
                    <button
                      type="button"
                      onClick={() => setSelected(event.id)}
                      className={cn(
                        "flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors",
                        event.id === selected ? "bg-accent-subtle" : "hover:bg-surface-hover",
                      )}
                    >
                      <span className="w-16 shrink-0 text-2xs tabular text-fg-muted">
                        {formatRelativeShort(event.occurred_at, new Date())}
                      </span>
                      <span
                        className={cn(
                          "shrink-0 rounded-sm px-1.5 py-0.5 font-mono text-2xs",
                          failed ? "bg-danger-subtle text-danger-text" : "bg-fill text-fg-secondary",
                        )}
                      >
                        {event.type}
                      </span>
                      <span className="min-w-0 flex-1 truncate font-mono text-2xs text-fg-muted">
                        {JSON.stringify(event.payload)}
                      </span>
                      <span className="shrink-0 text-xs text-fg-secondary">{customer?.name ?? "Anonymous"}</span>
                      <Badge tone="neutral" variant="outline">
                        {event.source}
                      </Badge>
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </div>

        <aside className="hidden w-[380px] shrink-0 overflow-y-auto border-l border-line bg-surface p-4 xl:block">
          {active ? (
            <>
              <p className="mb-1 font-mono text-sm text-fg">{active.type}</p>
              <p className="mb-4 text-xs text-fg-muted">
                {formatRelativeShort(active.occurred_at, new Date())} ago · via {active.source}
              </p>
              <CodeBlock
                language="json"
                showLineNumbers
                code={JSON.stringify(
                  {
                    id: active.id,
                    type: active.type,
                    workspace_id: active.workspace_id,
                    customer_id: active.customer_id,
                    session_id: active.session_id,
                    source: active.source,
                    occurred_at: active.occurred_at,
                    payload: active.payload,
                  },
                  null,
                  2,
                )}
              />
            </>
          ) : (
            <p className="text-xs text-fg-muted">Select an event to inspect its payload.</p>
          )}
        </aside>
      </div>

      <Pagination
        hasPrevious={false}
        hasNext={list.hasMore}
        onPrevious={() => undefined}
        onNext={() => void list.fetchNext()}
        summary={`${list.items.length} loaded${list.hasMore ? "+" : ""}`}
      />

      <PageBody className="border-t border-line">
        <Section title="Sending events">
          <Callout tone="info" className="mb-3">
            Event types are logged as sent; declaring a type in the metadata schema is only required
            for the *attributes* an event tries to set, not the event itself. An undeclared attribute
            key is rejected with a 422 rather than silently stored.
          </Callout>

          <CodeBlock
            language="javascript"
            code={`Hubchat('track', 'checkout.failed', {
  order_id: 'ord_8814',
  amount: 4200,
  currency: 'EUR',
  reason: 'card_declined',
});`}
          />
        </Section>
      </PageBody>
    </Page>
  );
}
