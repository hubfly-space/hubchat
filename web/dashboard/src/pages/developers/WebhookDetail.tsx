import {
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  Checkbox,
  CodeBlock,
  CopyField,
  DataTable,
  EmptyState,
  Field,
  Input,
  Metric,
  Page,
  PageBody,
  PageHeader,
  Section,
  Tabs,
  TabsContent,
  TabsList,
  Tooltip,
  formatPercent,
  formatRelativeShort,
  type BadgeTone,
  type Column,
  type WebhookDelivery,
} from "@hubchat/shared";
import { RefreshCw, RotateCcw, Send, Trash2, Webhook } from "lucide-react";
import { useState } from "react";
import { useParams } from "react-router-dom";
import { NOW, webhookDeliveries, webhookEndpoints } from "../../data/fixtures";

const EVENTS = [
  "conversation.created",
  "conversation.assigned",
  "conversation.resolved",
  "message.created",
  "ticket.created",
  "ticket.updated",
  "ticket.sla_breached",
  "customer.created",
  "customer.updated",
  "feedback.created",
  "feedback.status_changed",
  "survey.response_created",
];

const STATUS: Record<WebhookDelivery["status"], { label: string; tone: BadgeTone }> = {
  pending: { label: "Pending", tone: "neutral" },
  succeeded: { label: "Delivered", tone: "success" },
  failed: { label: "Failed", tone: "warning" },
  dead: { label: "Dead", tone: "danger" },
};

/** Webhook endpoint detail with delivery history and replay (§6.16). */
export default function WebhookDetail() {
  const { endpointId } = useParams();
  const [tab, setTab] = useState("deliveries");

  const endpoint = webhookEndpoints.find((item) => item.id === endpointId) ?? webhookEndpoints[0];

  if (!endpoint) {
    return (
      <Page>
        <EmptyState icon={Webhook} size="lg" title="Endpoint not found" />
      </Page>
    );
  }

  const deliveries = webhookDeliveries.filter((item) => item.endpoint_id === endpoint.id);
  const total = endpoint.success_24h + endpoint.failure_24h;

  const columns: Column<WebhookDelivery>[] = [
    {
      key: "created_at",
      header: "When",
      width: "88px",
      numeric: true,
      cell: (delivery) => (
        <span className="text-xs text-fg-muted">
          {formatRelativeShort(delivery.created_at, NOW)}
        </span>
      ),
    },
    {
      key: "event_type",
      header: "Event",
      cell: (delivery) => (
        <span className="font-mono text-xs text-fg-secondary">{delivery.event_type}</span>
      ),
    },
    {
      key: "status",
      header: "Status",
      width: "104px",
      cell: (delivery) => (
        <Badge tone={STATUS[delivery.status].tone}>{STATUS[delivery.status].label}</Badge>
      ),
    },
    {
      key: "response_status",
      header: "Response",
      width: "92px",
      numeric: true,
      cell: (delivery) =>
        delivery.response_status ? (
          <span
            className={
              delivery.response_status >= 400 ? "text-xs text-danger-text" : "text-xs text-fg-muted"
            }
          >
            {delivery.response_status}
          </span>
        ) : (
          <span className="text-xs text-fg-disabled">—</span>
        ),
    },
    {
      key: "attempt",
      header: "Attempt",
      width: "84px",
      numeric: true,
      hideBelow: "md",
      cell: (delivery) => delivery.attempt,
    },
    {
      key: "duration_ms",
      header: "Duration",
      width: "92px",
      numeric: true,
      hideBelow: "lg",
      cell: (delivery) =>
        delivery.duration_ms != null ? (
          <span className="text-xs text-fg-muted">{delivery.duration_ms}ms</span>
        ) : (
          "—"
        ),
    },
  ];

  return (
    <Page>
      <PageHeader
        breadcrumbs={[{ label: "Webhooks", href: "/developers/webhooks" }, { label: "Endpoint" }]}
        title={<span className="font-mono">{endpoint.url}</span>}
        description={endpoint.description ?? undefined}
        meta={
          endpoint.auto_disabled_at ? (
            <Badge tone="danger">Auto-disabled</Badge>
          ) : (
            <Badge tone={endpoint.enabled ? "success" : "neutral"}>
              {endpoint.enabled ? "Active" : "Paused"}
            </Badge>
          )
        }
        actions={
          <>
            <Button variant="secondary" size="sm" leading={<Send />}>
              Send test event
            </Button>
            <Button variant="secondary" size="sm" leading={<RotateCcw />}>
              Replay failed
            </Button>
          </>
        }
        tabs={
          <Tabs value={tab} onValueChange={setTab}>
            <TabsList
              items={[
                { value: "deliveries", label: "Deliveries", count: deliveries.length },
                { value: "events", label: "Subscribed events", count: endpoint.events.length },
                { value: "security", label: "Security" },
                { value: "settings", label: "Settings" },
              ]}
            />
          </Tabs>
        }
      />

      <PageBody>
        {endpoint.auto_disabled_at && (
          <Callout tone="danger" className="mb-5" title="This endpoint was disabled automatically">
            Six consecutive deliveries failed. Fix the receiving service, re-enable the endpoint,
            then replay the failed window — events are retained for 30 days.
          </Callout>
        )}

        <Tabs value={tab} onValueChange={setTab}>
          <TabsContent value="deliveries">
            <Section>
              <Card>
                <CardBody className="grid gap-6 sm:grid-cols-4">
                  <Metric
                    label="Delivered (24h)"
                    value={endpoint.success_24h.toLocaleString()}
                    definition="Deliveries that returned a 2xx response."
                  />
                  <Metric
                    label="Failed (24h)"
                    value={endpoint.failure_24h.toLocaleString()}
                    higherIsBetter={false}
                    definition="Deliveries that returned a non-2xx response or timed out."
                  />
                  <Metric
                    label="Success rate"
                    value={total > 0 ? formatPercent(endpoint.success_24h / total, 1) : "—"}
                    definition="Successful deliveries as a share of attempts in the last 24 hours."
                  />
                  <Metric
                    label="Median latency"
                    value="142 ms"
                    higherIsBetter={false}
                    definition="Time from dispatch to a complete response from your endpoint."
                  />
                </CardBody>
              </Card>
            </Section>

            <Section title="Recent deliveries">
              <Card>
                <CardBody className="p-0">
                  <DataTable
                    aria-label="Webhook deliveries"
                    rows={deliveries}
                    columns={columns}
                    rowKey={(delivery) => delivery.id}
                    rowActions={(delivery) =>
                      delivery.status === "failed" || delivery.status === "dead" ? (
                        <Tooltip content="Replay this delivery">
                          <Button
                            variant="ghost"
                            size="xs"
                            iconOnly
                            aria-label="Replay"
                            leading={<RefreshCw />}
                          />
                        </Tooltip>
                      ) : null
                    }
                    empty={
                      <EmptyState
                        icon={Webhook}
                        size="sm"
                        title="No deliveries yet"
                        description="Send a test event to confirm your endpoint is reachable."
                      />
                    }
                  />
                </CardBody>
              </Card>
            </Section>
          </TabsContent>

          <TabsContent value="events">
            <Card>
              <CardHeader
                title="Subscribed events"
                description="Only these types are dispatched to this endpoint. Adding an event does not backfill history."
              />
              <CardBody>
                <div className="grid gap-2 sm:grid-cols-2">
                  {EVENTS.map((event) => (
                    <Checkbox
                      key={event}
                      label={<span className="font-mono text-xs">{event}</span>}
                      defaultChecked={endpoint.events.includes(event)}
                    />
                  ))}
                </div>
              </CardBody>
            </Card>
          </TabsContent>

          <TabsContent value="security">
            <Section title="Signing secret">
              <div className="space-y-2">
                <CopyField label="Secret" value={`whsec_${"•".repeat(24)}4f2a`} masked />
                <Callout tone="warning">
                  Rotating issues a new secret and keeps the old one valid for 24 hours, so you can
                  deploy the change without dropping events.
                </Callout>
                <Button variant="secondary" size="sm" leading={<RotateCcw />}>
                  Rotate secret
                </Button>
              </div>
            </Section>

            <Section
              title="Verifying a request"
              description="Check the signature and the timestamp. The signature alone does not protect you from replay."
            >
              <CodeBlock
                language="go"
                filename="verify.go"
                code={`func Verify(secret, signature, timestamp string, body []byte) bool {
	// Reject anything older than five minutes.
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || time.Since(time.Unix(ts, 0)) > 5*time.Minute {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body) // raw bytes, before JSON parsing

	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}`}
              />
            </Section>

            <Section title="Example payload">
              <CodeBlock
                language="json"
                code={`{
  "id": "evt_01hq7xk2m9",
  "type": "ticket.sla_breached",
  "workspace_id": "wrk_01hq7x",
  "occurred_at": "2026-07-26T12:00:00Z",
  "sequence": 1042,
  "data": {
    "ticket_id": "tkt_4468",
    "number": 4468,
    "target": "next_response",
    "breached_by_seconds": 1920
  }
}`}
              />
            </Section>
          </TabsContent>

          <TabsContent value="settings">
            <Card>
              <CardBody className="space-y-4">
                <Field label="Endpoint URL" description="Must be HTTPS in production.">
                  <Input mono defaultValue={endpoint.url} />
                </Field>
                <Field label="Description">
                  <Input defaultValue={endpoint.description ?? ""} />
                </Field>
              </CardBody>
            </Card>

            <Card className="mt-4 border-danger-border">
              <CardHeader
                title="Delete this endpoint"
                description="Delivery history is retained for 30 days and then purged."
                actions={
                  <Button variant="danger" size="sm" leading={<Trash2 />}>
                    Delete
                  </Button>
                }
              />
            </Card>
          </TabsContent>
        </Tabs>
      </PageBody>
    </Page>
  );
}
