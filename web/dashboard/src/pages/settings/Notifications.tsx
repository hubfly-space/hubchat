import {
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  Checkbox,
  Page,
  PageBody,
  PageHeader,
  Section,
  Select,
  SettingsRow,
  Switch,
} from "@hubchat/shared";
import { Bell } from "lucide-react";

const AGENT_EVENTS = [
  { key: "assignment", label: "A conversation is assigned to me", defaults: ["app", "email", "push"] },
  { key: "mention", label: "Someone mentions me in a note", defaults: ["app", "email", "push"] },
  { key: "reply", label: "A customer replies to a thread I own", defaults: ["app", "push"] },
  { key: "sla_warning", label: "An SLA I own is approaching breach", defaults: ["app", "push"] },
  { key: "sla_breach", label: "An SLA I own has breached", defaults: ["app", "email", "push"] },
  { key: "team_unassigned", label: "Unassigned work arrives in my team's inbox", defaults: ["app"] },
  { key: "feedback", label: "Feedback is submitted on a board I moderate", defaults: ["app"] },
];

const CUSTOMER_EVENTS = [
  { key: "ticket_created", label: "Ticket created", detail: "Confirmation with the ticket number." },
  { key: "agent_replied", label: "Agent replied", detail: "The reply body, with a link to continue in the portal." },
  { key: "status_changed", label: "Ticket status changed", detail: "Only for meaningful transitions, not every field edit." },
  { key: "resolved", label: "Ticket resolved", detail: "Includes the reopen link." },
  { key: "survey", label: "Satisfaction survey", detail: "Sent after resolution, subject to per-customer frequency limits." },
  { key: "feedback_status", label: "Feedback status changed", detail: "To everyone subscribed to the item." },
];

/** Notification defaults (§6.15). */
export default function Notifications() {
  return (
    <Page>
      <PageHeader
        title="Notifications"
        description="Workspace defaults. Each member can override their own in Account → Preferences."
        actions={
          <Button variant="primary" size="sm">
            Save changes
          </Button>
        }
      />

      <PageBody width="narrow">
        <Section title="Agent notifications">
          <Card>
            <CardBody className="p-0">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-line">
                    <th className="px-4 py-2 text-left text-2xs font-semibold uppercase tracking-caps text-fg-muted">
                      Event
                    </th>
                    {["In-app", "Email", "Browser"].map((channel) => (
                      <th
                        key={channel}
                        className="w-20 px-2 py-2 text-center text-2xs font-semibold uppercase tracking-caps text-fg-muted"
                      >
                        {channel}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {AGENT_EVENTS.map((event) => (
                    <tr key={event.key} className="border-b border-line-subtle last:border-b-0">
                      <td className="px-4 py-2.5 text-fg">{event.label}</td>
                      {["app", "email", "push"].map((channel) => (
                        <td key={channel} className="px-2 py-2.5 text-center">
                          <Checkbox
                            defaultChecked={event.defaults.includes(channel)}
                            aria-label={`${event.label} via ${channel}`}
                            className="inline-flex"
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

        <Section title="Quiet hours">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow
                label="Suppress outside business hours"
                description="Email and browser notifications queue until the next open window. In-app notifications still appear."
              >
                <Switch defaultChecked aria-label="Suppress outside business hours" />
              </SettingsRow>

              <SettingsRow
                label="Digest for low-priority events"
                description="Batches non-urgent notifications into a single message."
              >
                <Select
                  size="sm"
                  defaultValue="hourly"
                  aria-label="Digest frequency"
                  options={[
                    { value: "off", label: "Send immediately" },
                    { value: "hourly", label: "Hourly digest" },
                    { value: "daily", label: "Daily digest" },
                  ]}
                />
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>

        <Section title="Customer notifications">
          <Callout tone="warning" className="mb-3" icon={<Bell />}>
            Outbound email is not configured on this deployment, so none of these are currently
            being delivered — they are queued.
          </Callout>

          <Card>
            <CardHeader
              title="What customers receive"
              description="Templates are customisable under Channels → Email."
            />
            <CardBody className="space-y-3">
              {CUSTOMER_EVENTS.map((event) => (
                <Switch
                  key={event.key}
                  label={event.label}
                  description={event.detail}
                  defaultChecked
                />
              ))}
            </CardBody>
          </Card>
        </Section>
      </PageBody>
    </Page>
  );
}
