import {
  Badge,
  Button,
  Card,
  CardBody,
  Callout,
  EmptyState,
  Menu,
  MenuContent,
  MenuItem,
  MenuSeparator,
  MenuTrigger,
  Page,
  PageBody,
  PageHeader,
  Section,
  Switch,
  Tooltip,
  formatRelativeShort,
} from "@hubchat/shared";
import { AlertTriangle, Copy, FlaskConical, History, MoreHorizontal, Plus, Trash2, Workflow } from "lucide-react";
import { Link } from "react-router-dom";
import { NOW, automationRules } from "../../data/fixtures";

const TRIGGER_LABEL: Record<string, string> = {
  "conversation.created": "When a conversation is created",
  "message.received": "When a message arrives",
  "ticket.created": "When a ticket is created",
  "ticket.updated": "When a ticket changes",
  "customer.identified": "When a customer identifies",
  "customer.attribute_changed": "When a customer attribute changes",
  "event.received": "When an application event arrives",
  "form.submitted": "When a form is submitted",
  "feedback.submitted": "When feedback is submitted",
  "sla.approaching": "When an SLA is about to breach",
  "sla.breached": "When an SLA breaches",
  "conversation.idle": "When a conversation goes idle",
  "business_hours.changed": "When business hours start or end",
  schedule: "On a schedule",
};

/** Automation rules (§6.13). */
export default function RuleList() {
  const failing = automationRules.filter((rule) => rule.error_count_24h > 0);

  return (
    <Page>
      <PageHeader
        title="Rules"
        description="Explicit triggers, conditions, and actions. Deterministic — the same input always produces the same outcome."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />} asChild>
            <Link to="/automation/rules/new">New rule</Link>
          </Button>
        }
      />

      <PageBody>
        {failing.length > 0 && (
          <Callout
            tone="warning"
            className="mb-5"
            icon={<AlertTriangle />}
            title={`${failing.length} rule${failing.length === 1 ? "" : "s"} failed in the last 24 hours`}
            actions={
              <Button variant="secondary" size="sm" asChild>
                <Link to="/automation/executions">View log</Link>
              </Button>
            }
          >
            A failing action does not stop the rest of the rule — remaining actions still apply.
          </Callout>
        )}

        <Section>
          {automationRules.length === 0 ? (
            <EmptyState
              icon={Workflow}
              title="No rules yet"
              description="Rules handle the repetitive decisions: routing by account tier, escalating breaches, closing idle threads."
            />
          ) : (
            <div className="space-y-3">
              {automationRules.map((rule) => (
                <Card key={rule.id}>
                  <CardBody>
                    <div className="flex flex-wrap items-start gap-4">
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <Link
                            to={`/automation/rules/${rule.id}`}
                            className="truncate text-sm font-medium text-fg hover:underline"
                          >
                            {rule.name}
                          </Link>
                          <Badge tone="neutral">v{rule.version}</Badge>
                          {rule.error_count_24h > 0 && (
                            <Tooltip content={`${rule.error_count_24h} failed executions in 24h`}>
                              <span>
                                <Badge tone="danger" leading={<AlertTriangle />}>
                                  {rule.error_count_24h}
                                </Badge>
                              </span>
                            </Tooltip>
                          )}
                        </div>

                        <p className="mt-1 text-xs text-fg-secondary">
                          {TRIGGER_LABEL[rule.trigger] ?? rule.trigger}
                          {rule.conditions.conditions.length > 0 && (
                            <>
                              {" "}
                              <span className="text-fg-muted">
                                and {rule.conditions.conditions.length} condition
                                {rule.conditions.conditions.length === 1 ? "" : "s"} match
                              </span>
                            </>
                          )}
                          {" → "}
                          <span className="text-fg-muted">
                            {rule.actions.length} action{rule.actions.length === 1 ? "" : "s"}
                          </span>
                        </p>

                        {rule.description && (
                          <p className="mt-1 text-xs text-fg-muted">{rule.description}</p>
                        )}

                        <p className="mt-2 text-2xs tabular text-fg-disabled">
                          Ran {rule.run_count_24h}× in 24h ·{" "}
                          {rule.last_run_at
                            ? `last ${formatRelativeShort(rule.last_run_at, NOW)} ago`
                            : "never run"}
                        </p>
                      </div>

                      <div className="flex shrink-0 items-center gap-2">
                        <Switch defaultChecked={rule.enabled} aria-label={`Enable ${rule.name}`} />
                        <Menu>
                          <MenuTrigger asChild>
                            <Button
                              variant="ghost"
                              size="sm"
                              iconOnly
                              aria-label={`Actions for ${rule.name}`}
                              leading={<MoreHorizontal />}
                            />
                          </MenuTrigger>
                          <MenuContent align="end">
                            <MenuItem icon={<FlaskConical />}>Dry run against recent data</MenuItem>
                            <MenuItem icon={<History />}>Version history</MenuItem>
                            <MenuItem icon={<Copy />}>Duplicate</MenuItem>
                            <MenuSeparator />
                            <MenuItem icon={<Trash2 />} destructive>
                              Delete rule
                            </MenuItem>
                          </MenuContent>
                        </Menu>
                      </div>
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
