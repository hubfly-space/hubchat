import {
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  EmptyState,
  Page,
  PageBody,
  PageHeader,
  Section,
  Sparkline,
  Switch,
  Tooltip,
  formatCompact,
  formatRelativeShort,
} from "@hubchat/shared";
import { AlertTriangle, Plus, Webhook } from "lucide-react";
import { Link } from "react-router-dom";
import { NOW, analytics, webhookEndpoints } from "../../data/fixtures";

/** Webhook endpoints (§6.16). */
export default function WebhookList() {
  const disabled = webhookEndpoints.filter((endpoint) => endpoint.auto_disabled_at);

  return (
    <Page>
      <PageHeader
        title="Webhooks"
        description="Signed, timestamped HTTP callbacks with retry, replay, and delivery history."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />}>
            New endpoint
          </Button>
        }
      />

      <PageBody>
        {disabled.length > 0 && (
          <Callout
            tone="danger"
            className="mb-5"
            icon={<AlertTriangle />}
            title={`${disabled.length} endpoint disabled automatically`}
          >
            Hubchat stops delivering to an endpoint after six consecutive failures so a dead URL
            does not clog the queue. Fix the endpoint, then re-enable and replay the missed window.
          </Callout>
        )}

        <Section>
          {webhookEndpoints.length === 0 ? (
            <EmptyState
              icon={Webhook}
              title="No webhook endpoints"
              description="Webhooks are how Hubchat tells your systems something happened, without them polling."
            />
          ) : (
            <div className="space-y-3">
              {webhookEndpoints.map((endpoint) => {
                const total = endpoint.success_24h + endpoint.failure_24h;
                const failureRate = total > 0 ? endpoint.failure_24h / total : 0;

                return (
                  <Card key={endpoint.id}>
                    <CardBody>
                      <div className="flex flex-wrap items-start gap-4">
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <Link
                              to={`/developers/webhooks/${endpoint.id}`}
                              className="truncate font-mono text-sm text-fg hover:underline"
                            >
                              {endpoint.url}
                            </Link>
                            {endpoint.auto_disabled_at && (
                              <Tooltip
                                content={`Disabled ${formatRelativeShort(endpoint.auto_disabled_at, NOW)} ago after repeated failures`}
                              >
                                <span>
                                  <Badge tone="danger">Auto-disabled</Badge>
                                </span>
                              </Tooltip>
                            )}
                            {failureRate > 0.02 && !endpoint.auto_disabled_at && (
                              <Badge tone="warning">
                                {Math.round(failureRate * 100)}% failing
                              </Badge>
                            )}
                          </div>

                          {endpoint.description && (
                            <p className="mt-1 text-xs text-fg-muted">{endpoint.description}</p>
                          )}

                          <div className="mt-2 flex flex-wrap gap-1">
                            {endpoint.events.map((event) => (
                              <Badge key={event} tone="neutral" variant="outline">
                                {event}
                              </Badge>
                            ))}
                          </div>

                          <p className="mt-2 text-2xs tabular text-fg-disabled">
                            {formatCompact(endpoint.success_24h)} delivered ·{" "}
                            {endpoint.failure_24h} failed in 24h · secret {endpoint.secret_hint}
                          </p>
                        </div>

                        <div className="flex shrink-0 items-center gap-3">
                          <Sparkline
                            points={analytics.conversations.slice(-14)}
                            tone={endpoint.failure_24h > 0 ? 6 : 4}
                          />
                          <Switch
                            defaultChecked={endpoint.enabled}
                            aria-label={`Enable ${endpoint.url}`}
                          />
                        </div>
                      </div>
                    </CardBody>
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
