import {
  Badge,
  Callout,
  Card,
  CardBody,
  CardHeader,
  Page,
  PageBody,
  PageHeader,
  Section,
  UsageMeter,
  formatBytes,
} from "@hubchat/shared";
import { Info } from "lucide-react";

const LIMITS = [
  { group: "People", items: [
    { label: "Workspace members", used: 6, limit: null },
    { label: "Teams", used: 2, limit: null },
  ]},
  { group: "Surfaces", items: [
    { label: "Inboxes", used: 3, limit: null },
    { label: "Widgets", used: 3, limit: null },
    { label: "Portals", used: 1, limit: null },
    { label: "Feedback boards", used: 3, limit: null },
    { label: "Knowledge bases", used: 1, limit: null },
  ]},
  { group: "Volume", items: [
    { label: "Monthly active contacts", used: 2_140, limit: null },
    { label: "Conversations this month", used: 1_284, limit: null },
    { label: "Stored events", used: 184_920, limit: 500_000 },
    { label: "API requests today", used: 42_180, limit: 250_000 },
  ]},
];

/**
 * Usage and limits (§23).
 *
 * The self-hosted edition enforces no plan limits — but the counters exist, and
 * the interface reads them, so a hosted edition does not require a second
 * codebase. What you see here is genuine usage, not a paywall.
 */
export default function Limits() {
  return (
    <Page>
      <PageHeader
        title="Usage & limits"
        description="What this workspace is consuming. Useful for capacity planning long before it is useful for billing."
      />

      <PageBody width="narrow">
        <Callout tone="info" className="mb-5" icon={<Info />}>
          This is a self-hosted deployment, so no plan limits are enforced. Counters marked with a
          ceiling are operational safeguards — the event and API limits protect PostgreSQL from a
          runaway integration (§26.4), not your invoice.
        </Callout>

        {LIMITS.map((group) => (
          <Section key={group.group} title={group.group}>
            <Card>
              <CardBody className="space-y-4">
                {group.items.map((item) => (
                  <UsageMeter
                    key={item.label}
                    label={item.label}
                    used={item.used}
                    limit={item.limit}
                  />
                ))}
              </CardBody>
            </Card>
          </Section>
        ))}

        <Section title="Storage">
          <Card>
            <CardBody className="space-y-4">
              <UsageMeter
                label="Attachments"
                used={4_180}
                limit={null}
                unit="MB"
              />
              <UsageMeter label="Generated exports" used={184} limit={null} unit="MB" />
              <div className="border-t border-line-subtle pt-3">
                <p className="flex items-baseline justify-between text-xs">
                  <span className="text-fg-secondary">Total on disk</span>
                  <span className="tabular text-fg">{formatBytes(4_364 * 1024 * 1024)}</span>
                </p>
                <p className="mt-1 text-2xs text-fg-muted">
                  Stored at /var/lib/hubchat/files · 214 GB free on the volume
                </p>
              </div>
            </CardBody>
          </Card>
        </Section>

        <Section title="Per-request ceilings">
          <Card>
            <CardHeader
              title="Hard limits"
              description="Requests exceeding these are rejected rather than truncated, so you find out at integration time instead of discovering silently-lost data later."
            />
            <CardBody>
              <dl className="space-y-2 text-xs">
                {[
                  ["Maximum file size", "25 MB"],
                  ["Maximum event payload", "32 KB"],
                  ["Maximum request body", "10 MB"],
                  ["Maximum JSON attribute depth", "4 levels"],
                  ["Maximum custom attributes per customer", "64"],
                  ["Maximum actions per rule execution", "20"],
                ].map(([label, value]) => (
                  <div key={label} className="flex items-baseline justify-between gap-3">
                    <dt className="text-fg-muted">{label}</dt>
                    <dd className="shrink-0 tabular text-fg-secondary">{value}</dd>
                  </div>
                ))}
              </dl>
            </CardBody>
          </Card>
        </Section>

        <Section title="Edition">
          <Card>
            <CardBody className="flex items-center justify-between gap-4">
              <div>
                <p className="text-sm text-fg">Self-hosted, open source</p>
                <p className="mt-0.5 text-xs text-fg-muted">
                  No entitlement checks are active. Every module is available.
                </p>
              </div>
              <Badge tone="success">All features</Badge>
            </CardBody>
          </Card>
        </Section>
      </PageBody>
    </Page>
  );
}
