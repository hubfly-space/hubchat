import {
  AvatarGroup,
  Badge,
  Button,
  Card,
  CardBody,
  EmptyState,
  Page,
  PageBody,
  PageHeader,
  Section,
  Select,
  SettingsRow,
  cn,
} from "@hubchat/shared";
import { Plus, Trash2, Users } from "lucide-react";
import { useState } from "react";
import { useWorkspace } from "../../app/workspace-context";
import { inboxes, teams } from "../../data/fixtures";

/** Teams and routing (§6.12). */
export default function Teams() {
  const { memberById } = useWorkspace();
  const [activeId, setActiveId] = useState(teams[0]!.id);
  const active = teams.find((team) => team.id === activeId)!;

  return (
    <Page>
      <PageHeader
        title="Teams"
        description="Groups that own inboxes, share a routing strategy, and can be assigned work as a unit."
        actions={
          <Button variant="primary" size="sm" leading={<Plus />}>
            New team
          </Button>
        }
      />

      <PageBody>
        <div className="grid gap-5 lg:grid-cols-[240px_minmax(0,1fr)]">
          <nav aria-label="Teams" className="space-y-1">
            {teams.map((team) => (
              <button
                key={team.id}
                type="button"
                onClick={() => setActiveId(team.id)}
                className={cn(
                  "flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm transition-colors",
                  team.id === activeId
                    ? "bg-accent-subtle font-medium text-fg"
                    : "text-fg-secondary hover:bg-fill hover:text-fg",
                )}
              >
                <Users aria-hidden="true" className="size-3.5 shrink-0 text-fg-muted" />
                <span className="min-w-0 flex-1 truncate">{team.name}</span>
                <span className="text-2xs tabular text-fg-muted">{team.member_ids.length}</span>
              </button>
            ))}
          </nav>

          <div className="min-w-0">
            <Section title={active.name} description={active.description ?? undefined}>
              <Card>
                <CardBody className="pt-0">
                  <SettingsRow label="Team lead" description="Escalation target and default approver.">
                    <Select
                      size="sm"
                      defaultValue={active.lead_id ?? undefined}
                      aria-label="Team lead"
                      options={active.member_ids.map((id) => ({
                        value: id,
                        label: memberById(id)?.name ?? id,
                      }))}
                    />
                  </SettingsRow>

                  <SettingsRow
                    label="Routing strategy"
                    description="How work is distributed when a rule assigns to this team rather than a person."
                  >
                    <Select
                      size="sm"
                      defaultValue={active.routing_strategy}
                      aria-label="Routing strategy"
                      options={[
                        { value: "manual", label: "Manual", description: "Sits in the team queue until someone claims it." },
                        { value: "round_robin", label: "Round robin", description: "Even distribution, in order." },
                        { value: "least_active", label: "Least active", description: "To whoever holds the fewest open threads." },
                        { value: "customer_owner", label: "Customer owner", description: "To the account owner when there is one." },
                        { value: "weighted", label: "Weighted", description: "Deterministic weights per member." },
                      ]}
                    />
                  </SettingsRow>

                  <SettingsRow
                    label="Inbox access"
                    description="Members can only see conversations in these inboxes. Enforced in the query, not the interface."
                  >
                    <div className="flex flex-wrap gap-1.5">
                      {inboxes.map((inbox) => (
                        <Badge
                          key={inbox.id}
                          tone={active.inbox_ids.includes(inbox.id) ? "accent" : "neutral"}
                          variant="outline"
                        >
                          {inbox.name}
                        </Badge>
                      ))}
                    </div>
                  </SettingsRow>
                </CardBody>
              </Card>
            </Section>

            <Section
              title="Members"
              actions={
                <Button variant="secondary" size="sm" leading={<Plus />}>
                  Add member
                </Button>
              }
            >
              <Card>
                <CardBody className="p-0">
                  {active.member_ids.length === 0 ? (
                    <EmptyState icon={Users} size="sm" title="No members" />
                  ) : (
                    <ul className="divide-y divide-line-subtle">
                      {active.member_ids.map((id) => {
                        const member = memberById(id);
                        if (!member) return null;
                        return (
                          <li key={id} className="flex items-center gap-3 px-4 py-2.5">
                            <AvatarGroup
                              size="sm"
                              people={[{ id: member.id, name: member.name }]}
                            />
                            <span className="min-w-0 flex-1">
                              <span className="block truncate text-sm text-fg">{member.name}</span>
                              <span className="block truncate text-xs text-fg-muted">
                                {member.email}
                              </span>
                            </span>
                            {member.id === active.lead_id && <Badge tone="accent">Lead</Badge>}
                            <Button
                              variant="ghost"
                              size="xs"
                              iconOnly
                              aria-label={`Remove ${member.name}`}
                              leading={<Trash2 />}
                            />
                          </li>
                        );
                      })}
                    </ul>
                  )}
                </CardBody>
              </Card>
            </Section>
          </div>
        </div>
      </PageBody>
    </Page>
  );
}
