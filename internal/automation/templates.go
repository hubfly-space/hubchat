package automation

type Template struct {
	Key         string         `json:"key"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Trigger     string         `json:"trigger"`
	Conditions  map[string]any `json:"conditions"`
	Actions     []Action       `json:"actions"`
}

func Templates() []Template {
	return []Template{
		{Key: "urgent_reply", Name: "Prioritize urgent customer replies", Description: "Raise priority when a customer's message contains urgent. Review the condition before enabling.", Trigger: "message.received", Conditions: map[string]any{"match": "all", "conditions": []any{map[string]any{"field": "author_type", "operator": "is", "value": "customer"}, map[string]any{"field": "body", "operator": "contains", "value": "urgent"}}}, Actions: []Action{{ID: "urgent_priority", Type: "set_priority", Params: map[string]any{"priority": "urgent"}}}},
		{Key: "new_followup", Name: "Create a follow-up task for new conversations", Description: "Create an unassigned task due in one day. Choose an assignee in the rule editor if required.", Trigger: "conversation.created", Conditions: map[string]any{}, Actions: []Action{{ID: "followup_task", Type: "create_task", Params: map[string]any{"title": "Follow up on new conversation", "due_after_minutes": 1440}}}},
		{Key: "sla_escalation", Name: "Escalate breached conversations", Description: "Raise priority when a conversation breaches its SLA.", Trigger: "sla.breached", Conditions: map[string]any{"match": "all", "conditions": []any{map[string]any{"field": "event.entity_type", "operator": "is", "value": "conversation"}}}, Actions: []Action{{ID: "sla_priority", Type: "set_priority", Params: map[string]any{"priority": "urgent"}}}},
	}
}
