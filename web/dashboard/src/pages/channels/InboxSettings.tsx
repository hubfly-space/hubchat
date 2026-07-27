import {
  Badge,
  Button,
  Card,
  CardBody,
  CardHeader,
  Field,
  Input,
  Page,
  PageBody,
  PageHeader,
  Section,
  Select,
  SettingsRow,
  Switch,
} from "@hubchat/shared";
import { Inbox, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { inboxes, slaPolicies, teams } from "../../data/fixtures";

/** Inbox configuration (§6.1, §6.12). */
export default function InboxSettings() {
  const [activeId, setActiveId] = useState(inboxes[0]!.id);
  const active = inboxes.find((inbox) => inbox.id === activeId)!;

  return (
    <Page>
      <PageHeader
        title="Inboxes"
        description="Destinations for conversations and tickets. Split by product, department, or brand."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />}>
            New inbox
          </Button>
        }
      />

      <PageBody>
        <div className="grid gap-5 lg:grid-cols-[240px_minmax(0,1fr)]">
          <nav aria-label="Inboxes" className="space-y-1">
            {inboxes.map((inbox) => (
              <button
                key={inbox.id}
                type="button"
                onClick={() => setActiveId(inbox.id)}
                className={
                  inbox.id === activeId
                    ? "flex w-full items-center gap-2 rounded-md bg-accent-subtle px-2.5 py-2 text-left text-sm font-medium text-fg"
                    : "flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm text-fg-secondary transition-colors hover:bg-fill hover:text-fg"
                }
              >
                <Inbox aria-hidden="true" className="size-3.5 shrink-0 text-fg-muted" />
                <span className="min-w-0 flex-1 truncate">{inbox.name}</span>
                {inbox.is_default && <Badge tone="neutral">Default</Badge>}
              </button>
            ))}
          </nav>

          <div className="min-w-0">
            <Section title={active.name}>
              <Card>
                <CardBody className="pt-0">
                  <SettingsRow label="Name" htmlFor="inbox-name">
                    <Input id="inbox-name" inputSize="sm" value={active.name} readOnly />
                  </SettingsRow>

                  <SettingsRow
                    label="Slug"
                    description="Used in inbox-scoped URLs and API filters."
                    htmlFor="inbox-slug"
                  >
                    <Input id="inbox-slug" inputSize="sm" mono value={active.slug} readOnly />
                  </SettingsRow>

                  <SettingsRow label="Description" htmlFor="inbox-desc">
                    <Input id="inbox-desc" inputSize="sm" value={active.description ?? ""} readOnly />
                  </SettingsRow>

                  <SettingsRow
                    label="Default inbox"
                    description="Where conversations land when no rule or widget specifies otherwise."
                  >
                    <Switch defaultChecked={active.is_default} aria-label="Default inbox" />
                  </SettingsRow>
                </CardBody>
              </Card>
            </Section>

            <Section title="Access and routing">
              <Card>
                <CardBody className="pt-0">
                  <SettingsRow
                    label="Teams with access"
                    description="Members outside these teams cannot see this inbox at all — the filter is applied in the query, not the interface."
                  >
                    <div className="flex flex-wrap gap-2">
                      {teams.map((team) => (
                        <Badge
                          key={team.id}
                          tone={active.team_ids.includes(team.id) ? "accent" : "neutral"}
                          variant="outline"
                        >
                          {team.name}
                        </Badge>
                      ))}
                    </div>
                  </SettingsRow>

                  <SettingsRow label="Assignment strategy">
                    <Select
                      size="sm"
                      defaultValue="least_active"
                      aria-label="Assignment strategy"
                      options={[
                        { value: "manual", label: "Manual", description: "Agents pick their own work." },
                        { value: "round_robin", label: "Round robin", description: "Even distribution in order." },
                        { value: "least_active", label: "Least active", description: "To whoever has the fewest open threads." },
                        { value: "team_queue", label: "Team queue", description: "Assign to the team, not a person." },
                      ]}
                    />
                  </SettingsRow>

                  <SettingsRow label="SLA policy">
                    <Select
                      size="sm"
                      defaultValue={active.sla_policy_id ?? undefined}
                      aria-label="SLA policy"
                      options={slaPolicies.map((policy) => ({ value: policy.id, label: policy.name }))}
                    />
                  </SettingsRow>

                  <SettingsRow label="Channels" description="Which sources can deliver into this inbox.">
                    <div className="flex flex-wrap gap-2">
                      {active.channels.map((channel) => (
                        <Badge key={channel} tone="neutral" variant="outline">
                          {channel}
                        </Badge>
                      ))}
                    </div>
                  </SettingsRow>
                </CardBody>
              </Card>
            </Section>

            <Section title="Danger zone">
              <Card className="border-danger-border">
                <CardHeader
                  title="Delete this inbox"
                  description={`${active.open_count} open conversations would need to be moved first. Deleting an inbox does not delete its history.`}
                  actions={
                    <Button variant="danger" size="sm" leading={<Trash2 />} disabled={active.is_default}>
                      Delete
                    </Button>
                  }
                />
              </Card>
            </Section>
          </div>
        </div>
      </PageBody>
    </Page>
  );
}
