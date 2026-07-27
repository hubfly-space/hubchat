import {
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  Checkbox,
  EmptyState,
  Field,
  Input,
  Page,
  PageBody,
  PageHeader,
  Section,
  Select,
  SettingsRow,
} from "@hubchat/shared";
import { PauseCircle, Timer, Trash2 } from "lucide-react";
import { useParams } from "react-router-dom";
import { calendars, slaPolicies } from "../../data/fixtures";
import type { ConversationState } from "@hubchat/shared";

const PAUSE_STATES: { value: ConversationState; label: string }[] = [
  { value: "waiting_for_customer", label: "Waiting on customer" },
  { value: "pending", label: "Pending" },
  { value: "snoozed", label: "Snoozed" },
  { value: "closed", label: "Closed" },
];

/** SLA policy editor (§6.14). */
export default function PolicyEditor() {
  const { policyId } = useParams();
  const policy = slaPolicies.find((item) => item.id === policyId) ?? slaPolicies[0];

  if (!policy) {
    return (
      <Page>
        <EmptyState icon={Timer} size="lg" title="Policy not found" />
      </Page>
    );
  }

  return (
    <Page>
      <PageHeader
        breadcrumbs={[{ label: "SLA policies", href: "/sla/policies" }, { label: policy.name }]}
        title={policy.name}
        description={policy.description ?? undefined}
        meta={<Badge tone={policy.enabled ? "success" : "neutral"}>{policy.enabled ? "Active" : "Disabled"}</Badge>}
        actions={
          <>
            <Button variant="secondary" size="sm">
              Discard
            </Button>
            <Button variant="primary" size="sm">
              Save policy
            </Button>
          </>
        }
      />

      <PageBody width="narrow">
        <Section title="Targets" description="Measured in business hours against the calendar below.">
          <Card>
            <CardBody className="p-0">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-line">
                    <th className="px-4 py-2 text-left text-2xs font-semibold uppercase tracking-caps text-fg-muted">
                      Priority
                    </th>
                    <th className="px-4 py-2 text-right text-2xs font-semibold uppercase tracking-caps text-fg-muted">
                      First response
                    </th>
                    <th className="px-4 py-2 text-right text-2xs font-semibold uppercase tracking-caps text-fg-muted">
                      Next response
                    </th>
                    <th className="px-4 py-2 text-right text-2xs font-semibold uppercase tracking-caps text-fg-muted">
                      Resolution
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {policy.targets.map((target) => (
                    <tr key={target.priority} className="border-b border-line-subtle last:border-b-0">
                      <td className="px-4 py-2 capitalize text-fg">{target.priority}</td>
                      {(
                        [
                          target.first_response_minutes,
                          target.next_response_minutes,
                          target.resolution_minutes,
                        ] as const
                      ).map((minutes, index) => (
                        <td key={index} className="px-4 py-2">
                          <Input
                            inputSize="sm"
                            type="number"
                            suffix="min"
                            defaultValue={minutes}
                            aria-label={`${target.priority} target ${index + 1}`}
                            className="ml-auto max-w-32"
                          />
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </CardBody>
          </Card>
        </Section>

        <Section title="Clock">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow
                label="Business-hours calendar"
                description="Timers only advance during open hours, and never on a holiday."
              >
                <Select
                  size="sm"
                  defaultValue={policy.calendar_id}
                  aria-label="Calendar"
                  options={calendars.map((calendar) => ({
                    value: calendar.id,
                    label: calendar.name,
                    description: calendar.timezone,
                  }))}
                />
              </SettingsRow>

              <SettingsRow
                label="Pause the timer when"
                description="A paused timer shows the reason in the inbox, so nobody thinks the clock broke."
              >
                <div className="flex flex-col gap-2">
                  {PAUSE_STATES.map((state) => (
                    <Checkbox
                      key={state.value}
                      label={state.label}
                      defaultChecked={policy.pause_states.includes(state.value)}
                    />
                  ))}
                </div>
              </SettingsRow>

              <SettingsRow
                label="Warning threshold"
                description="When the remaining time falls below this share of the target, the conversation moves into the Approaching SLA view."
              >
                <Input
                  inputSize="sm"
                  type="number"
                  suffix="%"
                  defaultValue={policy.warning_threshold_percent}
                  className="max-w-32"
                />
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>

        <Section title="Scope" description="Which conversations this policy applies to.">
          <Card>
            <CardBody>
              {policy.applies_to.conditions.length === 0 ? (
                <Callout tone="info">
                  This is the default policy — it applies to everything no other policy claims. The
                  most specific matching policy always wins.
                </Callout>
              ) : (
                <ul className="space-y-2">
                  {policy.applies_to.conditions.map((condition, index) => (
                    <li key={index} className="flex items-center gap-2 text-sm">
                      <span className="text-2xs uppercase tracking-caps text-fg-muted">
                        {index === 0 ? "if" : policy.applies_to.match}
                      </span>
                      <code className="rounded-xs bg-fill px-1.5 py-0.5 font-mono text-xs text-fg-secondary">
                        {condition.field}
                      </code>
                      <span className="text-fg-muted">{condition.operator.replace(/_/g, " ")}</span>
                      <code className="rounded-xs bg-fill px-1.5 py-0.5 font-mono text-xs text-fg-secondary">
                        {String(condition.value)}
                      </code>
                    </li>
                  ))}
                </ul>
              )}
            </CardBody>
          </Card>
        </Section>

        <Section title="Escalation" description="What happens on breach, beyond the badge turning red.">
          <Card>
            <CardBody>
              {policy.escalation_actions.length === 0 ? (
                <p className="text-xs text-fg-muted">
                  No escalation actions. Breaches are still surfaced in the Breached SLA view and in
                  reports.
                </p>
              ) : (
                <ul className="space-y-1.5">
                  {policy.escalation_actions.map((action) => (
                    <li key={action.id} className="flex items-center gap-2 text-sm text-fg-secondary">
                      <PauseCircle aria-hidden="true" className="size-3.5 text-warning-text" />
                      {action.type.replace(/_/g, " ")}
                    </li>
                  ))}
                </ul>
              )}
              <Field label="" className="mt-3">
                <Button variant="secondary" size="sm">
                  Add escalation action
                </Button>
              </Field>
            </CardBody>
          </Card>
        </Section>

        <Section title="Danger zone">
          <Card className="border-danger-border">
            <CardHeader
              title="Delete this policy"
              description="Conversations currently governed by it fall back to the default policy. Historical compliance figures are preserved."
              actions={
                <Button variant="danger" size="sm" leading={<Trash2 />}>
                  Delete
                </Button>
              }
            />
          </Card>
        </Section>
      </PageBody>
    </Page>
  );
}
