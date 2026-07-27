import {
  Badge,
  Button,
  Card,
  CardBody,
  EmptyState,
  Page,
  PageBody,
  PageHeader,
  Progress,
  Section,
  Switch,
  formatDuration,
  formatPercent,
} from "@hubchat/shared";
import { Plus, Timer } from "lucide-react";
import { Link } from "react-router-dom";
import { slaPolicies } from "../../data/fixtures";

/** SLA policies (§6.14). */
export default function PolicyList() {
  return (
    <Page>
      <PageHeader
        title="SLA policies"
        description="Response and resolution targets, measured against a business-hours calendar."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />}>
            New policy
          </Button>
        }
      />

      <PageBody>
        <Section>
          {slaPolicies.length === 0 ? (
            <EmptyState
              icon={Timer}
              title="No SLA policies"
              description="A policy turns 'we should reply quickly' into a number the inbox can count down."
            />
          ) : (
            <div className="space-y-3">
              {slaPolicies.map((policy) => (
                <Card key={policy.id}>
                  <CardBody>
                    <div className="flex flex-wrap items-start gap-4">
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <Link
                            to={`/sla/policies/${policy.id}`}
                            className="truncate text-sm font-medium text-fg hover:underline"
                          >
                            {policy.name}
                          </Link>
                          {policy.applies_to.conditions.length === 0 && (
                            <Badge tone="neutral">Default</Badge>
                          )}
                        </div>
                        {policy.description && (
                          <p className="mt-1 text-xs text-fg-muted">{policy.description}</p>
                        )}

                        <dl className="mt-3 grid gap-x-6 gap-y-1 text-xs sm:grid-cols-2 lg:grid-cols-4">
                          {policy.targets.map((target) => (
                            <div key={target.priority} className="flex items-center justify-between gap-2">
                              <dt className="capitalize text-fg-muted">{target.priority}</dt>
                              <dd className="tabular text-fg-secondary">
                                {formatDuration(target.first_response_minutes * 60)} /{" "}
                                {formatDuration(target.resolution_minutes * 60)}
                              </dd>
                            </div>
                          ))}
                        </dl>
                        <p className="mt-1 text-2xs text-fg-disabled">
                          First response / resolution, in business hours
                        </p>
                      </div>

                      <div className="w-44 shrink-0">
                        <p className="mb-1.5 flex items-baseline justify-between text-xs">
                          <span className="text-fg-muted">30-day compliance</span>
                          <span className="tabular text-fg">
                            {policy.compliance_30d != null
                              ? formatPercent(policy.compliance_30d, 1)
                              : "—"}
                          </span>
                        </p>
                        <Progress
                          value={policy.compliance_30d ?? 0}
                          tone={(policy.compliance_30d ?? 0) >= 0.95 ? "success" : "warning"}
                          label={`${policy.name} compliance`}
                        />
                      </div>

                      <Switch defaultChecked={policy.enabled} aria-label={`Enable ${policy.name}`} />
                    </div>
                  </CardBody>
                </Card>
              ))}
            </div>
          )}
        </Section>
      </PageBody>
    </Page>
  );
}
