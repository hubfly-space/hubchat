import { Input, Select, Textarea, Tooltip, type AutomationAction, type AutomationActionType, type FieldValue } from "@hubchat/shared";

export const ACTION_TYPES: { value: AutomationActionType; label: string; group: string }[] = [
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
  { value: "create_task", label: "Create a task", group: "Lifecycle" },
];

export type ActionDirectory = {
  members: { id: string; name: string }[];
  teams: { id: string; name: string }[];
  inboxes: { id: string; name: string }[];
  tags: { id: string; name: string }[];
  webhooks: { id: string; url: string; enabled: boolean }[];
};

export function ActionParams({
  action,
  members,
  teams,
  inboxes,
  tags,
  webhooks,
  onChange,
}: ActionDirectory & {
  action: AutomationAction;
  onChange: (params: AutomationAction["params"]) => void;
}) {
  const set = (key: string, value: FieldValue) => onChange({ ...action.params, [key]: value });
  const value = (key: string) => String(action.params[key] ?? "");

  switch (action.type) {
    case "assign_member":
      return <Select size="sm" aria-label="Member" value={value("member_id")} onValueChange={(selected) => set("member_id", selected)} options={members.map((member) => ({ value: member.id, label: member.name }))} />;
    case "assign_team":
      return <Select size="sm" aria-label="Team" value={value("team_id")} onValueChange={(selected) => set("team_id", selected)} options={teams.map((team) => ({ value: team.id, label: team.name }))} />;
    case "move_inbox":
      return <Select size="sm" aria-label="Inbox" value={value("inbox_id")} onValueChange={(selected) => set("inbox_id", selected)} options={inboxes.map((inbox) => ({ value: inbox.id, label: inbox.name }))} />;
    case "add_tag":
    case "remove_tag":
      return <Select size="sm" aria-label="Tag" value={value("tag_id")} onValueChange={(selected) => set("tag_id", selected)} options={tags.map((tag) => ({ value: tag.id, label: tag.name }))} />;
    case "set_priority":
      return <Select size="sm" aria-label="Priority" value={value("priority")} onValueChange={(selected) => set("priority", selected)} options={[{ value: "urgent", label: "Urgent" }, { value: "high", label: "High" }, { value: "normal", label: "Normal" }, { value: "low", label: "Low" }]} />;
    case "set_state":
      return <Select size="sm" aria-label="State" value={value("state")} onValueChange={(selected) => set("state", selected)} options={[{ value: "open", label: "Open" }, { value: "pending", label: "Pending" }, { value: "waiting_for_customer", label: "Waiting on customer" }, { value: "resolved", label: "Resolved" }, { value: "closed", label: "Closed" }]} />;
    case "set_field":
      return <div className="grid gap-2 sm:grid-cols-2"><Input inputSize="sm" value={value("field")} onChange={(event) => set("field", event.target.value)} placeholder="field_key" aria-label="Field key" /><Input inputSize="sm" value={value("value")} onChange={(event) => set("value", event.target.value)} placeholder="Value" aria-label="Field value" /></div>;
    case "send_message":
      return <Textarea rows={2} value={value("body")} onChange={(event) => set("body", event.target.value)} placeholder="Message sent to the customer…" aria-label="Message" />;
    case "send_email":
      return <div className="space-y-2"><Input inputSize="sm" value={value("to")} onChange={(event) => set("to", event.target.value)} placeholder="recipient@example.com" aria-label="Recipient" /><Input inputSize="sm" value={value("subject")} onChange={(event) => set("subject", event.target.value)} placeholder="Email subject" aria-label="Subject" /><Textarea rows={2} value={value("body")} onChange={(event) => set("body", event.target.value)} placeholder="Email body" aria-label="Email body" /></div>;
    case "invoke_webhook":
      return <div className="space-y-2"><Tooltip content="Endpoints are managed in Developers → Webhooks"><span><Select size="sm" aria-label="Webhook endpoint" value={value("endpoint_id")} onValueChange={(selected) => set("endpoint_id", selected)} options={webhooks.filter((endpoint) => endpoint.enabled).map((endpoint) => ({ value: endpoint.id, label: endpoint.url }))} /></span></Tooltip><Textarea rows={2} value={value("payload")} onChange={(event) => set("payload", event.target.value)} placeholder='{"source":"automation"}' aria-label="Webhook payload" /></div>;
    case "pause_sla":
      return <Input inputSize="sm" value={value("reason")} onChange={(event) => set("reason", event.target.value)} placeholder="Waiting for a dependency" aria-label="Pause reason" />;
    case "close_after_inactivity":
      return <Input inputSize="sm" type="number" suffix="minutes" value={value("after_minutes")} onChange={(event) => set("after_minutes", Number(event.target.value) || 0)} className="max-w-32" />;
    case "create_task":
      return <div className="space-y-2"><Input inputSize="sm" value={value("title")} onChange={(event) => set("title", event.target.value)} placeholder="Follow up with the customer" aria-label="Task title" /><Textarea rows={2} value={value("description")} onChange={(event) => set("description", event.target.value)} placeholder="Task details" aria-label="Task description" /><div className="flex gap-2"><Input inputSize="sm" type="number" value={value("due_after_minutes")} onChange={(event) => set("due_after_minutes", Number(event.target.value) || 0)} placeholder="Due after minutes" aria-label="Due after minutes" /><Select size="sm" aria-label="Task assignee" value={value("assignee_id")} onValueChange={(selected) => set("assignee_id", selected)} options={members.map((member) => ({ value: member.id, label: member.name }))} /></div></div>;
    default:
      return <p className="text-2xs text-fg-muted">No parameters.</p>;
  }
}

export function actionLabel(type: AutomationActionType) {
  return ACTION_TYPES.find((item) => item.value === type)?.label ?? type.replace(/_/g, " ");
}
