import {
  Badge,
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  Eyebrow,
  Field,
  Input,
  Menu,
  MenuContent,
  MenuItem,
  MenuLabel,
  MenuTrigger,
  Page,
  PageBody,
  PageHeader,
  SegmentedControl,
  Select,
  Section,
  Switch,
  Textarea,
  Toolbar,
  Tooltip,
  cn,
  useToast,
} from "@hubchat/shared";
import {
  ArrowDown,
  Ban,
  FlaskConical,
  History,
  Plus,
  Trash2,
  Webhook,
  Workflow,
  Zap,
} from "lucide-react";
import { useState } from "react";
import { useParams } from "react-router-dom";
import { automationRules, inboxes, members, tags, teams } from "../../data/fixtures";
import type { AutomationAction, AutomationActionType, FilterCondition } from "@hubchat/shared";

const TRIGGERS = [
  { value: "conversation.created", label: "Conversation created", group: "Conversations" },
  { value: "message.received", label: "Message received", group: "Conversations" },
  { value: "conversation.idle", label: "Conversation goes idle", group: "Conversations" },
  { value: "ticket.created", label: "Ticket created", group: "Tickets" },
  { value: "ticket.updated", label: "Ticket updated", group: "Tickets" },
  { value: "customer.identified", label: "Customer identified", group: "Customers" },
  { value: "customer.attribute_changed", label: "Customer attribute changed", group: "Customers" },
  { value: "event.received", label: "Application event received", group: "Customers" },
  { value: "form.submitted", label: "Form submitted", group: "Intake" },
  { value: "feedback.submitted", label: "Feedback submitted", group: "Intake" },
  { value: "sla.approaching", label: "SLA approaching breach", group: "Service level" },
  { value: "sla.breached", label: "SLA breached", group: "Service level" },
  { value: "business_hours.changed", label: "Business hours start or end", group: "Time" },
  { value: "schedule", label: "On a schedule", group: "Time" },
];

const ACTION_TYPES: { value: AutomationActionType; label: string; group: string }[] = [
  { value: "assign_member", label: "Assign to a person", group: "Assignment" },
  { value: "assign_team", label: "Assign to a team", group: "Assignment" },
  { value: "move_inbox", label: "Move to another inbox", group: "Assignment" },
  { value: "set_priority", label: "Set priority", group: "Fields" },
  { value: "set_state", label: "Change state", group: "Fields" },
  { value: "set_field", label: "Set a custom field", group: "Fields" },
  { value: "add_tag", label: "Add a tag", group: "Fields" },
  { value: "remove_tag", label: "Remove a tag", group: "Fields" },
  { value: "send_message", label: "Send a message to the customer", group: "Communication" },
  { value: "send_email", label: "Send an email notification", group: "Communication" },
  { value: "invoke_webhook", label: "Call a webhook", group: "Integrations" },
  { value: "start_sla", label: "Start the SLA timer", group: "Service level" },
  { value: "pause_sla", label: "Pause the SLA timer", group: "Service level" },
  { value: "close_after_inactivity", label: "Close after inactivity", group: "Lifecycle" },
];

const CONDITION_FIELDS = [
  { value: "company.tier", label: "Company tier" },
  { value: "customer.plan", label: "Customer plan" },
  { value: "customer.account_status", label: "Account status" },
  { value: "channel", label: "Channel" },
  { value: "inbox", label: "Inbox" },
  { value: "priority", label: "Priority" },
  { value: "state", label: "State" },
  { value: "tag", label: "Tag" },
  { value: "page_url", label: "Page URL" },
  { value: "language", label: "Language" },
  { value: "business_hours", label: "Business hours" },
];

/**
 * Rule builder (§6.13).
 *
 * Laid out as a vertical sentence — WHEN, IF, THEN — rather than a node graph.
 * A graph looks impressive and is worse here: these rules are almost always
 * linear, and a graph makes the common case harder to read while doing nothing
 * for the rare one.
 *
 * The safety controls (§6.13 rule safety) are first-class, not buried:
 * dry run, execution depth limit, rate limit, and version history.
 */
export default function RuleBuilder() {
  const { ruleId } = useParams();
  const toast = useToast();

  const source = automationRules.find((rule) => rule.id === ruleId);
  const [name, setName] = useState(source?.name ?? "Untitled rule");
  const [trigger, setTrigger] = useState(source?.trigger ?? "conversation.created");
  const [match, setMatch] = useState<"all" | "any">(source?.conditions.match ?? "all");
  const [conditions, setConditions] = useState<FilterCondition[]>(
    source?.conditions.conditions ?? [],
  );
  const [actions, setActions] = useState<AutomationAction[]>(source?.actions ?? []);
  const [enabled, setEnabled] = useState(source?.enabled ?? false);

  const addAction = (type: AutomationActionType) =>
    setActions((current) => [
      ...current,
      { id: `act_${Math.random().toString(36).slice(2, 8)}`, type, params: {} },
    ]);

  return (
    <Page>
      <Toolbar
        className="h-topbar py-0"
        leading={
          <>
            <Workflow aria-hidden="true" className="size-4 text-fg-muted" />
            <input
              value={name}
              onChange={(event) => setName(event.target.value)}
              aria-label="Rule name"
              className="min-w-0 flex-1 bg-transparent text-sm font-medium text-fg outline-none"
            />
            {source && <Badge tone="neutral">v{source.version}</Badge>}
          </>
        }
        trailing={
          <>
            <Switch checked={enabled} onCheckedChange={setEnabled} aria-label="Rule enabled" />
            <Button variant="ghost" size="sm" leading={<History />}>
              History
            </Button>
            <Button
              variant="secondary"
              size="sm"
              leading={<FlaskConical />}
              onClick={() =>
                toast.toast({
                  title: "Dry run complete",
                  description: "Would have matched 38 of the last 500 conversations. No changes were applied.",
                  action: { label: "View matches", onClick: () => undefined },
                })
              }
            >
              Dry run
            </Button>
            <Button variant="primary" size="sm">
              Save rule
            </Button>
          </>
        }
      />

      <PageBody width="narrow">
        {!enabled && (
          <Callout tone="warning" className="mb-5">
            This rule is disabled. It will not run until you enable it, and a dry run is the safest
            way to check what it would do first.
          </Callout>
        )}

        {/* WHEN ---------------------------------------------------------- */}
        <StepCard step="When" tone="accent" description="The event that starts this rule.">
          <Select
            value={trigger}
            onValueChange={(value) => setTrigger(value as typeof trigger)}
            aria-label="Trigger"
            options={TRIGGERS.map((item) => ({
              value: item.value,
              label: item.label,
              group: item.group,
            }))}
          />

          {trigger === "conversation.idle" && (
            <Field label="Idle for" className="mt-3 max-w-48">
              <Input inputSize="sm" type="number" suffix="days" defaultValue={5} />
            </Field>
          )}

          {trigger === "schedule" && (
            <Field label="Cron expression" className="mt-3" description="Evaluated in the workspace timezone.">
              <Input inputSize="sm" mono defaultValue="0 9 * * MON" />
            </Field>
          )}
        </StepCard>

        <Connector />

        {/* IF ------------------------------------------------------------ */}
        <StepCard
          step="If"
          tone="neutral"
          description="Optional. With no conditions the rule runs on every matching event."
          headerAction={
            conditions.length > 1 ? (
              <SegmentedControl
                aria-label="Condition matching"
                value={match}
                onValueChange={setMatch}
                options={[
                  { value: "all", label: "Match all" },
                  { value: "any", label: "Match any" },
                ]}
              />
            ) : null
          }
        >
          {conditions.length === 0 ? (
            <p className="mb-3 text-xs text-fg-muted">
              No conditions — this rule applies to every {TRIGGERS.find((t) => t.value === trigger)?.label.toLowerCase()}.
            </p>
          ) : (
            <ul className="mb-3 space-y-2">
              {conditions.map((condition, index) => (
                <li key={index} className="flex items-center gap-2">
                  <span className="w-10 shrink-0 text-right text-2xs uppercase tracking-caps text-fg-muted">
                    {index === 0 ? "if" : match}
                  </span>
                  <Select
                    size="sm"
                    value={condition.field}
                    aria-label="Field"
                    className="flex-1"
                    options={CONDITION_FIELDS}
                    onValueChange={(value) =>
                      setConditions((current) =>
                        current.map((item, i) => (i === index ? { ...item, field: value } : item)),
                      )
                    }
                  />
                  <Select
                    size="sm"
                    value={condition.operator}
                    aria-label="Operator"
                    className="w-32 shrink-0"
                    options={[
                      { value: "is", label: "is" },
                      { value: "is_not", label: "is not" },
                      { value: "contains", label: "contains" },
                      { value: "in", label: "is any of" },
                      { value: "is_set", label: "is set" },
                    ]}
                    onValueChange={() => undefined}
                  />
                  <Input
                    inputSize="sm"
                    className="flex-1"
                    aria-label="Value"
                    defaultValue={String(condition.value ?? "")}
                  />
                  <Button
                    variant="ghost"
                    size="sm"
                    iconOnly
                    aria-label="Remove condition"
                    leading={<Trash2 />}
                    onClick={() =>
                      setConditions((current) => current.filter((_, i) => i !== index))
                    }
                  />
                </li>
              ))}
            </ul>
          )}

          <Button
            variant="secondary"
            size="sm"
            leading={<Plus />}
            onClick={() =>
              setConditions((current) => [
                ...current,
                { field: "company.tier", operator: "is", value: "" },
              ])
            }
          >
            Add condition
          </Button>
        </StepCard>

        <Connector />

        {/* THEN ---------------------------------------------------------- */}
        <StepCard step="Then" tone="success" description="Applied in order. A failing action does not stop the rest.">
          {actions.length === 0 ? (
            <p className="mb-3 text-xs text-fg-muted">No actions yet — a rule without actions does nothing.</p>
          ) : (
            <ol className="mb-3 space-y-2">
              {actions.map((action, index) => {
                const meta = ACTION_TYPES.find((item) => item.value === action.type);
                return (
                  <li key={action.id} className="flex items-start gap-2">
                    <span className="mt-1.5 grid size-5 shrink-0 place-items-center rounded-full bg-fill text-2xs font-semibold tabular text-fg-muted">
                      {index + 1}
                    </span>
                    <div className="min-w-0 flex-1 rounded-md border border-line bg-inset p-2.5">
                      <p className="mb-2 flex items-center gap-1.5 text-xs font-medium text-fg">
                        <Zap aria-hidden="true" className="size-3 text-accent-text" />
                        {meta?.label ?? action.type}
                      </p>
                      <ActionParams action={action} />
                    </div>
                    <Button
                      variant="ghost"
                      size="sm"
                      iconOnly
                      aria-label="Remove action"
                      leading={<Trash2 />}
                      className="mt-1"
                      onClick={() => setActions((current) => current.filter((item) => item.id !== action.id))}
                    />
                  </li>
                );
              })}
            </ol>
          )}

          <Menu>
            <MenuTrigger asChild>
              <Button variant="secondary" size="sm" leading={<Plus />}>
                Add action
              </Button>
            </MenuTrigger>
            <MenuContent className="w-64">
              {[...new Set(ACTION_TYPES.map((item) => item.group))].map((group) => (
                <div key={group}>
                  <MenuLabel>{group}</MenuLabel>
                  {ACTION_TYPES.filter((item) => item.group === group).map((item) => (
                    <MenuItem
                      key={item.value}
                      icon={item.value === "invoke_webhook" ? <Webhook /> : <Zap />}
                      onSelect={() => addAction(item.value)}
                    >
                      {item.label}
                    </MenuItem>
                  ))}
                </div>
              ))}
            </MenuContent>
          </Menu>
        </StepCard>

        {/* Safety --------------------------------------------------------- */}
        <Section title="Safety" className="mt-8">
          <Callout tone="system" className="mb-3" icon={<Ban />}>
            Hubchat tracks a causation chain on every execution. If this rule's actions re-trigger
            it — directly or through another rule — the chain is cut at the depth limit and the
            execution is logged as a loop rather than running unbounded (§26.7).
          </Callout>

          <Card>
            <CardBody className="space-y-4">
              <Field
                label="Maximum execution depth"
                description="How many rule generations a single originating event may cause."
              >
                <Input inputSize="sm" type="number" defaultValue={3} className="max-w-32" />
              </Field>
              <Field
                label="Rate limit"
                description="Executions per minute for this rule. Excess events are dropped and logged."
              >
                <Input inputSize="sm" type="number" defaultValue={60} className="max-w-32" />
              </Field>
              <Field label="Notes" description="Why this rule exists. Future you will want this.">
                <Textarea rows={2} defaultValue={source?.description ?? ""} />
              </Field>
            </CardBody>
          </Card>
        </Section>
      </PageBody>
    </Page>
  );
}

function StepCard({
  step,
  tone,
  description,
  headerAction,
  children,
}: {
  step: string;
  tone: "accent" | "neutral" | "success";
  description: string;
  headerAction?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <Card>
      <CardHeader
        title={
          <span className="flex items-center gap-2">
            <span
              className={cn(
                "rounded-sm px-1.5 py-0.5 text-2xs font-semibold uppercase tracking-caps",
                tone === "accent" && "bg-accent-subtle text-accent-text",
                tone === "neutral" && "bg-fill text-fg-secondary",
                tone === "success" && "bg-success-subtle text-success-text",
              )}
            >
              {step}
            </span>
          </span>
        }
        description={description}
        actions={headerAction}
      />
      <CardBody>{children}</CardBody>
    </Card>
  );
}

function Connector() {
  return (
    <div className="flex justify-center py-1.5" aria-hidden="true">
      <ArrowDown className="size-4 text-fg-disabled" />
    </div>
  );
}

function ActionParams({ action }: { action: AutomationAction }) {
  switch (action.type) {
    case "assign_member":
      return (
        <Select
          size="sm"
          aria-label="Member"
          options={members.map((member) => ({ value: member.id, label: member.name }))}
        />
      );
    case "assign_team":
      return (
        <Select
          size="sm"
          aria-label="Team"
          options={teams.map((team) => ({ value: team.id, label: team.name }))}
        />
      );
    case "move_inbox":
      return (
        <Select
          size="sm"
          aria-label="Inbox"
          options={inboxes.map((inbox) => ({ value: inbox.id, label: inbox.name }))}
        />
      );
    case "add_tag":
    case "remove_tag":
      return (
        <Select
          size="sm"
          aria-label="Tag"
          options={tags.map((tag) => ({ value: tag.id, label: tag.name }))}
        />
      );
    case "set_priority":
      return (
        <Select
          size="sm"
          aria-label="Priority"
          options={[
            { value: "urgent", label: "Urgent" },
            { value: "high", label: "High" },
            { value: "normal", label: "Normal" },
            { value: "low", label: "Low" },
          ]}
        />
      );
    case "set_state":
      return (
        <Select
          size="sm"
          aria-label="State"
          options={[
            { value: "open", label: "Open" },
            { value: "pending", label: "Pending" },
            { value: "waiting_for_customer", label: "Waiting on customer" },
            { value: "resolved", label: "Resolved" },
            { value: "closed", label: "Closed" },
          ]}
        />
      );
    case "send_message":
      return <Textarea rows={2} placeholder="Message sent to the customer…" aria-label="Message" />;
    case "invoke_webhook":
      return (
        <Tooltip content="Endpoints are managed in Developers → Webhooks">
          <span>
            <Select
              size="sm"
              aria-label="Webhook endpoint"
              options={[
                { value: "whk_ops", label: "ops.northwind.cloud/hooks/hubchat" },
                { value: "whk_crm", label: "crm.internal.northwind.cloud/hubchat" },
              ]}
            />
          </span>
        </Tooltip>
      );
    case "close_after_inactivity":
      return <Input inputSize="sm" type="number" suffix="days" defaultValue={7} className="max-w-32" />;
    default:
      return <p className="text-2xs text-fg-muted">No parameters.</p>;
  }
}
