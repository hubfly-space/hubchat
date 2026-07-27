import {
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  Input,
  Page,
  PageBody,
  PageHeader,
  Section,
  Select,
  SettingsRow,
  Switch,
  Textarea,
} from "@hubchat/shared";
import { ShieldAlert } from "lucide-react";

const RETENTION_CATEGORIES = [
  { key: "conversations", label: "Conversations and messages", detail: "Including attachments referenced by them.", value: "forever" },
  { key: "events", label: "Customer events", detail: "High-volume. The most useful thing to expire aggressively.", value: "90d" },
  { key: "sessions", label: "Contact sessions and page views", detail: "Presence history, not the conversation itself.", value: "30d" },
  { key: "audit", label: "Audit log", detail: "Append-only. Shortening this weakens your own incident response.", value: "2y" },
  { key: "webhooks", label: "Webhook delivery history", detail: "Replay is only possible inside this window.", value: "30d" },
  { key: "surveys", label: "Survey responses", detail: "Aggregates survive deletion of the individual response.", value: "forever" },
];

/** Privacy and retention (§12). */
export default function Privacy() {
  return (
    <Page>
      <PageHeader
        title="Privacy & retention"
        description="What Hubchat keeps, for how long, and what happens when a customer asks you to delete it."
        actions={
          <Button variant="primary" size="sm">
            Save changes
          </Button>
        }
      />

      <PageBody width="narrow">
        <Section title="Retention" description="Records past their window are deleted or anonymised by a scheduled job.">
          <Card>
            <CardBody className="pt-0">
              {RETENTION_CATEGORIES.map((category) => (
                <SettingsRow key={category.key} label={category.label} description={category.detail}>
                  <Select
                    size="sm"
                    defaultValue={category.value}
                    aria-label={`${category.label} retention`}
                    options={[
                      { value: "30d", label: "30 days" },
                      { value: "90d", label: "90 days" },
                      { value: "1y", label: "1 year" },
                      { value: "2y", label: "2 years" },
                      { value: "forever", label: "Keep indefinitely" },
                    ]}
                  />
                </SettingsRow>
              ))}
            </CardBody>
          </Card>
        </Section>

        <Section title="Collection">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow
                label="Store IP addresses"
                description="Anonymised keeps only the /24 network, which is enough for abuse handling without identifying a household."
              >
                <Select
                  size="sm"
                  defaultValue="anonymised"
                  aria-label="IP storage"
                  options={[
                    { value: "full", label: "Store in full" },
                    { value: "anonymised", label: "Anonymise" },
                    { value: "none", label: "Do not store" },
                  ]}
                />
              </SettingsRow>

              <SettingsRow
                label="Track page views"
                description="Records which pages a visitor saw before contacting you. Off means the agent sees only the current URL."
              >
                <Switch defaultChecked aria-label="Track page views" />
              </SettingsRow>

              <SettingsRow
                label="Allowed local-storage keys"
                description="Only these keys may be read from the host page and attached to a session."
              >
                <Input inputSize="sm" mono placeholder="app_version, experiment_bucket" />
              </SettingsRow>

              <SettingsRow
                label="Allowed cookie keys"
                description="Same rule for cookies. Empty means none are read."
              >
                <Input inputSize="sm" mono placeholder="locale" />
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>

        <Section title="Consent">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow
                label="Require consent before the first message"
                description="The visitor must acknowledge the notice before a conversation can start. Consent state is stored with the session."
              >
                <Switch aria-label="Require consent" />
              </SettingsRow>

              <SettingsRow label="Consent notice">
                <Textarea
                  rows={3}
                  defaultValue="By starting a conversation you agree to our privacy policy. We store your messages and basic session details to provide support."
                />
              </SettingsRow>

              <SettingsRow
                label="Privacy policy URL"
                description="Linked from the widget, the portal footer, and outbound email."
              >
                <Input inputSize="sm" mono prefix="https://" defaultValue="northwind.cloud/privacy" />
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>

        <Section title="Customer data requests">
          <Callout tone="warning" className="mb-3" icon={<ShieldAlert />}>
            Deletion is not uniformly destructive, and the interface will tell you exactly what
            happens before you confirm: messages are deleted, aggregate report figures are retained
            in anonymised form, and audit entries are preserved because deleting them would defeat
            their purpose.
          </Callout>

          <Card>
            <CardHeader
              title="Export a customer's data"
              description="Produces a machine-readable archive of everything associated with one customer."
              actions={
                <Button variant="secondary" size="sm">
                  Start export
                </Button>
              }
            />
          </Card>

          <Card className="mt-3">
            <CardHeader
              title="Delete a customer"
              description="Removes personal data and anonymises what must be retained. Irreversible."
              actions={
                <Button variant="danger" size="sm">
                  Delete a customer
                </Button>
              }
            />
          </Card>
        </Section>
      </PageBody>
    </Page>
  );
}
