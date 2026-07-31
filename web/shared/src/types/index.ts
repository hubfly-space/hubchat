/**
 * Hubchat domain types — the browser-side mirror of the v1 API contract.
 *
 * These are hand-maintained today. Once the Go handlers stabilise, this file
 * becomes generated output from the OpenAPI document (§14 boundary rules) and
 * hand edits stop. Until then: change a type here and the corresponding Go
 * struct in the same commit.
 *
 * Conventions that mirror the server:
 *   · every id is an opaque prefixed string  — never parse it
 *   · every timestamp is an RFC 3339 UTC string
 *   · every tenant-owned entity carries workspace_id
 *   · field names stay snake_case so payloads round-trip untouched
 */

// ---------------------------------------------------------------- primitives

/** Opaque, prefixed, sortable. `wrk_`, `cnv_`, `tkt_`, `cus_`, `msg_`, … */
export type Id = string;
/** RFC 3339 UTC, e.g. "2026-07-26T12:00:00Z". */
export type Timestamp = string;

export type Cursor = string | null;

/** Cursor-paginated collection response. Named to avoid colliding with the
 *  `<Page>` layout component — every list endpoint returns this shape. */
export type Paginated<T> = {
  data: T[];
  next_cursor: Cursor;
  has_more: boolean;
};

/**
 * §16 — the one and only error response body.
 *
 * Named for what it is (the response) rather than the error, because the
 * client exposes an `ApiError` *class* callers narrow on with `instanceof`.
 * Two things called ApiError, one a wire shape and one a thrown value, is a
 * confusion worth spending a longer name to avoid.
 */
export type ApiErrorResponse = {
  error: {
    code: string;
    message: string;
    request_id: string;
    details?: Record<string, unknown>;
  };
};

/** §16 — the envelope every realtime frame and webhook body shares. */
export type EventEnvelope<T = unknown> = {
  id: Id;
  type: string;
  workspace_id: Id;
  /**
   * What the event is about. Realtime clients subscribe by
   * `"<entity_type>:<entity_id>"`, so these are part of the wire contract, not
   * incidental metadata. Absent on workspace-wide events that belong to no
   * single record.
   */
  entity_type?: string;
  entity_id?: Id;
  occurred_at: Timestamp;
  sequence: number;
  data: T;
};

// ---------------------------------------------------------------- tenancy

export type Workspace = {
  id: Id;
  name: string;
  slug: string;
  logo_url: string | null;
  icon_url: string | null;
  default_language: string;
  timezone: string;
  ticket_prefix: string;
  created_at: Timestamp;
};

export type MemberRole =
  | "owner"
  | "admin"
  | "manager"
  | "agent"
  | "developer"
  | "analyst";

/** §5.9 — capabilities are the unit of authorization, roles are just bundles. */
export type Capability =
  | "conversation.read"
  | "conversation.reply"
  | "conversation.assign"
  | "conversation.delete"
  | "customer.read"
  | "customer.read_sensitive"
  | "customer.merge"
  | "company.manage"
  | "ticket.manage"
  | "widget.manage"
  | "portal.manage"
  | "feedback.moderate"
  | "knowledgebase.manage"
  | "automation.manage"
  | "sla.manage"
  | "integration.manage"
  | "report.read"
  | "audit.read"
  | "member.manage"
  | "workspace.manage"
  | "workspace.manage_security";

export type PresenceState = "online" | "away" | "busy" | "offline";

export type Member = {
  id: Id;
  workspace_id: Id;
  user_id: Id;
  name: string;
  email: string;
  avatar_url: string | null;
  role: MemberRole;
  capabilities: Capability[];
  teams: Id[];
  presence: PresenceState;
  accepting_conversations: boolean;
  last_seen_at: Timestamp | null;
  created_at: Timestamp;
};

export type Team = {
  id: Id;
  workspace_id: Id;
  name: string;
  description: string | null;
  lead_id: Id | null;
  member_ids: Id[];
  inbox_ids: Id[];
  routing_strategy: RoutingStrategy;
  created_at: Timestamp;
};

export type RoutingStrategy =
  | "manual"
  | "round_robin"
  | "least_active"
  | "team_queue"
  | "customer_owner"
  | "weighted";

// ---------------------------------------------------------------- channels

export type ChannelType =
  | "widget"
  | "portal"
  | "email"
  | "form"
  | "api"
  | "manual";

export type Inbox = {
  id: Id;
  workspace_id: Id;
  name: string;
  slug: string;
  description: string | null;
  channels: ChannelType[];
  team_ids: Id[];
  is_default: boolean;
  sla_policy_id: Id | null;
  open_count: number;
  created_at: Timestamp;
};

// ---------------------------------------------------------------- conversations

/** §6.2 base states. Custom states must normalise onto one of these. */
export type ConversationState =
  | "new"
  | "open"
  | "pending"
  | "waiting_for_customer"
  | "waiting_for_support"
  | "snoozed"
  | "resolved"
  | "closed"
  | "spam";

export type Priority = "low" | "normal" | "high" | "urgent";

export type SlaState = "none" | "active" | "paused" | "approaching" | "breached" | "met";

export type Conversation = {
  id: Id;
  workspace_id: Id;
  inbox_id: Id;
  channel: ChannelType;
  subject: string | null;
  state: ConversationState;
  priority: Priority;
  customer_id: Id | null;
  assignee_id: Id | null;
  team_id: Id | null;
  tag_ids: Id[];
  ticket_id: Id | null;
  unread: boolean;
  message_count: number;
  last_message_preview: string;
  last_message_at: Timestamp;
  last_customer_at: Timestamp | null;
  snoozed_until: Timestamp | null;
  sla: ConversationSla | null;
  /** Populated only while another agent has the thread open (§6.12 collision). */
  viewers: Id[];
  created_at: Timestamp;
};

export type ConversationSla = {
  policy_id: Id;
  state: SlaState;
  /** Seconds remaining; negative once breached. */
  first_response_remaining: number | null;
  next_response_remaining: number | null;
  resolution_remaining: number | null;
  paused_reason: string | null;
};

export type MessageAuthorType = "customer" | "agent" | "system" | "automation";

export type MessageKind = "reply" | "note" | "event";

export type DeliveryState = "pending" | "sent" | "delivered" | "read" | "failed";

export type Message = {
  id: Id;
  /** Client-generated, echoed back, used for idempotent send (§9). */
  client_id: string | null;
  conversation_id: Id;
  workspace_id: Id;
  kind: MessageKind;
  author_type: MessageAuthorType;
  author_id: Id | null;
  author_name: string;
  author_avatar_url: string | null;
  body: string;
  /** Populated for kind: "event" — renders as a timeline entry, not a bubble. */
  event_type: string | null;
  event_data: Record<string, unknown> | null;
  attachments: Attachment[];
  quoted_message_id: Id | null;
  delivery: DeliveryState;
  edited_at: Timestamp | null;
  redacted_at: Timestamp | null;
  sequence: number;
  created_at: Timestamp;
};

export type Attachment = {
  id: Id;
  name: string;
  mime_type: string;
  size_bytes: number;
  url: string;
  thumbnail_url: string | null;
};

// ---------------------------------------------------------------- tickets

export type TicketStatus =
  | "new"
  | "open"
  | "pending"
  | "on_hold"
  | "resolved"
  | "closed";

export type Ticket = {
  id: Id;
  workspace_id: Id;
  /** Display number only — never the primary key (§6.3). */
  number: number;
  prefix: string;
  title: string;
  description: string;
  status: TicketStatus;
  priority: Priority;
  type: string | null;
  customer_id: Id | null;
  company_id: Id | null;
  inbox_id: Id;
  channel: ChannelType;
  assignee_id: Id | null;
  team_id: Id | null;
  conversation_id: Id | null;
  parent_id: Id | null;
  child_ids: Id[];
  linked_ticket_ids: Id[];
  tag_ids: Id[];
  field_values: Record<string, FieldValue>;
  sla: ConversationSla | null;
  due_at: Timestamp | null;
  /** §13 conventions — pass back on update; a stale value means someone else changed this ticket first. */
  version: number;
  created_at: Timestamp;
  updated_at: Timestamp;
  resolved_at: Timestamp | null;
  closed_at: Timestamp | null;
};

// ---------------------------------------------------------------- fields

/** §6.10 — the shared type system for custom fields, metadata, and form inputs. */
export type FieldType =
  | "string"
  | "text"
  | "integer"
  | "decimal"
  | "boolean"
  | "timestamp"
  | "date"
  | "enum"
  | "multi_enum"
  | "string_list"
  | "url"
  | "email"
  | "phone"
  | "json";

export type FieldValue = string | number | boolean | string[] | null;

export type FieldDefinition = {
  id: Id;
  workspace_id: Id;
  key: string;
  label: string;
  type: FieldType;
  description: string | null;
  options: string[] | null;
  required: boolean;
  /** §6.3 — private fields never leave the agent surfaces. */
  visibility: "public" | "internal";
  /** §12 — masked in UI, excluded from search and export, audited on reveal. */
  sensitive: boolean;
  searchable: boolean;
  allowed_sources: MetadataSource[];
  required_capability: Capability | null;
  validation: FieldValidation | null;
  created_at: Timestamp;
};

export type FieldValidation = {
  min?: number;
  max?: number;
  min_length?: number;
  max_length?: number;
  pattern?: string;
};

export type MetadataSource =
  | "widget_init"
  | "js_sdk"
  | "rest_api"
  | "identity_token"
  | "portal_profile"
  | "form"
  | "url_params"
  | "local_storage"
  | "cookie"
  | "event";

/** §6.10 — the metadata allowlist's own type system, narrower than {@link FieldType}: it spans only what untrusted input can plausibly carry. */
export type AttributeType =
  | "string"
  | "integer"
  | "decimal"
  | "boolean"
  | "timestamp"
  | "date"
  | "enum"
  | "string_list"
  | "url"
  | "json";

export type AttributeEntityType = "customer" | "company" | "session";

/**
 * A declared metadata key (§6.10, §12). Deliberately not {@link FieldDefinition}
 * even though the shapes rhyme: this is the allowlist untrusted pipelines
 * (the SDK, the widget, URL parameters) write through, backed by its own
 * `attribute_definitions` table — agent-filled custom fields on a ticket are
 * a different concept with a different owner.
 */
export type AttributeDefinition = {
  id: Id;
  workspace_id: Id;
  entity_type: AttributeEntityType;
  key: string;
  label: string;
  type: AttributeType;
  description: string | null;
  options: string[];
  allowed_sources: MetadataSource[];
  required_capability: Capability | null;
  sensitive: boolean;
  searchable: boolean;
  validation: FieldValidation | null;
  retention_days: number | null;
  created_at: Timestamp;
};

// ---------------------------------------------------------------- customers

export type IdentityVerification = "anonymous" | "unverified" | "verified";

export type Customer = {
  id: Id;
  workspace_id: Id;
  name: string | null;
  email: string | null;
  phone: string | null;
  avatar_url: string | null;
  external_id: string | null;
  verification: IdentityVerification;
  company_ids: Id[];
  language: string | null;
  timezone: string | null;
  attributes: Record<string, FieldValue>;
  /** Keys in `attributes` whose real value is masked because the viewer lacks customer.read_sensitive (§12 audit-on-reveal). */
  masked_attribute_keys: string[];
  tag_ids: Id[];
  owner_id: Id | null;
  presence: PresenceState;
  current_url: string | null;
  first_seen_at: Timestamp;
  last_seen_at: Timestamp | null;
  last_contacted_at: Timestamp | null;
  /** §13 conventions — pass back on update; a stale value means someone else changed this profile first. */
  version: number;
};

export type Company = {
  id: Id;
  workspace_id: Id;
  name: string;
  domain: string | null;
  external_id: string | null;
  tier: string | null;
  owner_id: Id | null;
  attributes: Record<string, FieldValue>;
  tag_ids: Id[];
  sla_policy_id: Id | null;
  customer_count: number;
  open_ticket_count: number;
  created_at: Timestamp;
};

export type ContactSession = {
  id: Id;
  customer_id: Id | null;
  visitor_id: Id | null;
  started_at: Timestamp;
  last_seen_at: Timestamp;
  ended_at: Timestamp | null;
  ip_country: string | null;
  browser: string | null;
  os: string | null;
  device: "desktop" | "mobile" | "tablet" | "unknown";
  referrer: string | null;
  current_url: string | null;
  page_views: number;
};

export type CustomerEvent = {
  id: Id;
  workspace_id: Id;
  customer_id: Id | null;
  session_id: Id | null;
  type: string;
  source: MetadataSource;
  url: string | null;
  payload: Record<string, unknown>;
  occurred_at: Timestamp;
};

// ---------------------------------------------------------------- tags & views

export type Tag = {
  id: Id;
  workspace_id: Id;
  name: string;
  /** One of the six chart slots — tags never introduce new hues. */
  color: 1 | 2 | 3 | 4 | 5 | 6;
  usage_count: number;
};

export type SavedView = {
  id: Id;
  workspace_id: Id;
  name: string;
  icon: string | null;
  scope: "personal" | "team" | "workspace";
  filters: FilterGroup;
  sort: { field: string; direction: "asc" | "desc" };
  count: number | null;
};

export type FilterOperator =
  | "is"
  | "is_not"
  | "contains"
  | "not_contains"
  | "starts_with"
  | "gt"
  | "gte"
  | "lt"
  | "lte"
  | "is_set"
  | "is_not_set"
  | "in"
  | "not_in";

export type FilterCondition = {
  field: string;
  operator: FilterOperator;
  value: FieldValue;
};

export type FilterGroup = {
  match: "all" | "any";
  conditions: FilterCondition[];
  groups?: FilterGroup[];
};

// ---------------------------------------------------------------- widget & portal

export type WidgetMode =
  | "chat"
  | "support_menu"
  | "ticket_form"
  | "knowledge_base"
  | "feedback"
  | "announcements"
  | "hub";

export type Widget = {
  id: Id;
  workspace_id: Id;
  name: string;
  public_key: string;
  inbox_id: Id;
  modes: WidgetMode[];
  appearance: WidgetAppearance;
  content: WidgetContent;
  behavior: WidgetBehavior;
  domains: string[];
  environment: "production" | "test";
  rollout_percent: number;
  version: number;
  enabled: boolean;
  updated_at: Timestamp;
};

export type WidgetAppearance = {
  accent: string;
  theme: "light" | "dark" | "auto";
  logo_url: string | null;
  avatar_url: string | null;
  launcher_shape: "circle" | "rounded" | "pill";
  launcher_size: "sm" | "md" | "lg";
  launcher_label: string | null;
  position: "bottom-right" | "bottom-left";
  offset_x: number;
  offset_y: number;
  mobile_offset_y: number;
  panel_width: number;
  panel_height: number;
  radius: number;
  header_style: "solid" | "gradient" | "minimal";
  bubble_style: "rounded" | "square";
  font: "system" | "inter" | "geist" | "custom";
  z_index: number;
  hide_branding: boolean;
  custom_css_vars: Record<string, string>;
};

export type WidgetContent = {
  title: string;
  subtitle: string;
  welcome_message: string;
  input_placeholder: string;
  online_message: string;
  offline_message: string;
  response_time_text: string;
  consent_text: string | null;
};

export type WidgetBehavior = {
  trigger: "immediate" | "delay" | "scroll" | "event" | "manual";
  delay_seconds: number;
  scroll_percent: number;
  trigger_event: string | null;
  include_urls: string[];
  exclude_urls: string[];
  devices: ("desktop" | "mobile" | "tablet")[];
  outside_hours: "hide" | "show_offline" | "show_form";
  pre_chat_form_id: Id | null;
  post_chat_survey_id: Id | null;
  allow_anonymous: boolean;
  require_identity: boolean;
  sound: boolean;
  unread_badge: boolean;
  persist_conversation: boolean;
};

export type Portal = {
  id: Id;
  workspace_id: Id;
  name: string;
  subdomain: string;
  custom_domain: string | null;
  domain_status: "unverified" | "pending" | "active" | "error";
  theme: PortalTheme;
  navigation: PortalNavItem[];
  features: {
    tickets: boolean;
    knowledge_base: boolean;
    feedback: boolean;
    changelog: boolean;
    announcements: boolean;
  };
  auth_methods: PortalAuthMethod[];
  permissions: PortalPermissions;
  enabled: boolean;
  updated_at: Timestamp;
};

export type PortalTheme = {
  accent: string;
  mode: "light" | "dark" | "auto";
  logo_url: string | null;
  favicon_url: string | null;
  headline: string;
  subheadline: string;
  footer_links: { label: string; url: string }[];
  custom_css_vars: Record<string, string>;
};

export type PortalNavItem = {
  id: Id;
  label: string;
  href: string;
  external: boolean;
};

export type PortalAuthMethod = "password" | "magic_link" | "ticket_link" | "sso_token" | "anonymous";

export type PortalPermissions = {
  view_tickets_by_email: boolean;
  view_company_tickets: boolean;
  reopen_resolved: boolean;
  edit_fields: boolean;
  add_participants: boolean;
  download_transcript: boolean;
};

// ---------------------------------------------------------------- forms

export type Form = {
  id: Id;
  workspace_id: Id;
  name: string;
  slug: string;
  purpose: "ticket" | "conversation" | "feedback" | "survey" | "lead";
  fields: FormField[];
  routing: { inbox_id: Id | null; team_id: Id | null; tag_ids: Id[]; priority: Priority | null };
  confirmation: { title: string; body: string; redirect_url: string | null };
  access: "public" | "authenticated";
  spam_protection: boolean;
  submission_count: number;
  enabled: boolean;
  updated_at: Timestamp;
};

export type FormField = {
  id: Id;
  key: string;
  label: string;
  type: FieldType | "file" | "rating" | "hidden";
  placeholder: string | null;
  description: string | null;
  options: string[] | null;
  required: boolean;
  default_value: FieldValue;
  /** Renders only when this condition passes — §6.11 conditional fields. */
  condition: FilterCondition | null;
  validation: FieldValidation | null;
};

// ---------------------------------------------------------------- feedback

export type FeedbackStatus =
  | "open"
  | "reviewing"
  | "planned"
  | "in_progress"
  | "completed"
  | "declined";

export type FeedbackBoard = {
  id: Id;
  workspace_id: Id;
  name: string;
  slug: string;
  description: string | null;
  visibility: "public" | "private" | "invite_only";
  allow_comments: boolean;
  allow_voting: boolean;
  votes_per_customer: number | null;
  moderation: "none" | "pre" | "post";
  item_count: number;
  created_at: Timestamp;
};

export type FeedbackItem = {
  id: Id;
  workspace_id: Id;
  board_id: Id;
  title: string;
  description: string;
  type: string;
  status: FeedbackStatus;
  visibility: "public" | "private";
  submitter_id: Id | null;
  company_id: Id | null;
  product_area: string | null;
  vote_count: number;
  subscriber_count: number;
  comment_count: number;
  viewer_has_voted: boolean;
  viewer_subscribed: boolean;
  tag_ids: Id[];
  priority: Priority | null;
  linked_conversation_ids: Id[];
  linked_ticket_ids: Id[];
  merged_into_id: Id | null;
  created_at: Timestamp;
  updated_at: Timestamp;
};

export type FeedbackComment = {
  id: Id;
  item_id: Id;
  author_type: "customer" | "agent";
  author_id: Id | null;
  author_name: string;
  author_avatar_url: string | null;
  body: string;
  /** Renders with the accent treatment on public boards. */
  is_official_update: boolean;
  created_at: Timestamp;
};

// ---------------------------------------------------------------- knowledge base

export type ArticleState = "draft" | "in_review" | "scheduled" | "published" | "archived";

export type KnowledgeBase = {
  id: Id;
  workspace_id: Id;
  name: string;
  slug: string;
  default_language: string;
  languages: string[];
  visibility: "public" | "private";
  article_count: number;
};

export type ArticleCollection = {
  id: Id;
  knowledge_base_id: Id;
  name: string;
  slug: string;
  description: string | null;
  icon: string | null;
  article_count: number;
  position: number;
};

export type Article = {
  id: Id;
  workspace_id: Id;
  knowledge_base_id: Id;
  collection_id: Id | null;
  title: string;
  slug: string;
  excerpt: string;
  body: string;
  state: ArticleState;
  language: string;
  author_id: Id;
  tag_ids: Id[];
  related_ids: Id[];
  seo: { title: string | null; description: string | null };
  view_count: number;
  helpful_count: number;
  unhelpful_count: number;
  version: number;
  published_at: Timestamp | null;
  scheduled_at: Timestamp | null;
  updated_at: Timestamp;
};

// ---------------------------------------------------------------- surveys

export type SurveyType = "csat" | "ces" | "nps" | "custom";

export type Survey = {
  id: Id;
  workspace_id: Id;
  name: string;
  type: SurveyType;
  questions: SurveyQuestion[];
  delivery: ("widget" | "portal" | "email" | "link")[];
  trigger: "on_resolve" | "on_close" | "manual" | "scheduled";
  response_count: number;
  average_score: number | null;
  response_rate: number | null;
  enabled: boolean;
  expires_at: Timestamp | null;
};

export type SurveyQuestion = {
  id: Id;
  prompt: string;
  type: "stars" | "number" | "emoji" | "choice" | "multi_choice" | "text";
  options: string[] | null;
  required: boolean;
  condition: FilterCondition | null;
};

export type SurveyResponse = {
  id: Id;
  survey_id: Id;
  customer_id: Id | null;
  conversation_id: Id | null;
  agent_id: Id | null;
  score: number | null;
  answers: { question_id: Id; value: FieldValue }[];
  comment: string | null;
  submitted_at: Timestamp;
};

// ---------------------------------------------------------------- automation

export type AutomationTrigger =
  | "conversation.created"
  | "message.received"
  | "ticket.created"
  | "ticket.updated"
  | "customer.identified"
  | "customer.attribute_changed"
  | "event.received"
  | "form.submitted"
  | "feedback.submitted"
  | "sla.approaching"
  | "sla.breached"
  | "conversation.idle"
  | "business_hours.changed"
  | "schedule";

export type AutomationActionType =
  | "assign_member"
  | "assign_team"
  | "add_tag"
  | "remove_tag"
  | "set_priority"
  | "set_field"
  | "set_state"
  | "send_message"
  | "send_email"
  | "invoke_webhook"
  | "move_inbox"
  | "start_sla"
  | "pause_sla"
  | "close_after_inactivity"
  | "create_task";

export type AutomationAction = {
  id: Id;
  type: AutomationActionType;
  params: Record<string, FieldValue>;
};

export type AutomationRule = {
  id: Id;
  workspace_id: Id;
  name: string;
  description: string | null;
  trigger: AutomationTrigger;
  conditions: FilterGroup;
  actions: AutomationAction[];
  enabled: boolean;
  version: number;
  run_count_24h: number;
  last_run_at: Timestamp | null;
  error_count_24h: number;
  updated_at: Timestamp;
};

export type AutomationExecution = {
  id: Id;
  rule_id: Id;
  rule_version: number;
  subject_type: string;
  subject_id: Id;
  outcome: "matched" | "skipped" | "failed" | "dry_run";
  /** §26.7 — how deep the causation chain was when this fired. */
  depth: number;
  causation_id: Id | null;
  actions_applied: number;
  error: string | null;
  duration_ms: number;
  occurred_at: Timestamp;
};

export type Macro = {
  id: Id;
  workspace_id: Id;
  name: string;
  folder: string | null;
  scope: "personal" | "team" | "workspace";
  body: string | null;
  actions: AutomationAction[];
  usage_count: number;
  updated_at: Timestamp;
};

export type SavedReply = {
  id: Id;
  workspace_id: Id;
  name: string;
  shortcut: string | null;
  folder: string | null;
  scope: "personal" | "team" | "workspace";
  body: string;
  usage_count: number;
};

// ---------------------------------------------------------------- SLA

export type SlaPolicy = {
  id: Id;
  workspace_id: Id;
  name: string;
  description: string | null;
  calendar_id: Id;
  targets: SlaTarget[];
  pause_states: ConversationState[];
  warning_threshold_percent: number;
  escalation_actions: AutomationAction[];
  applies_to: FilterGroup;
  enabled: boolean;
  compliance_30d: number | null;
};

export type SlaTarget = {
  priority: Priority;
  first_response_minutes: number;
  next_response_minutes: number;
  resolution_minutes: number;
};

export type BusinessHoursCalendar = {
  id: Id;
  workspace_id: Id;
  name: string;
  timezone: string;
  /** Index 0 = Monday. Empty array means closed that day. */
  weekly: { start: string; end: string }[][];
  holidays: { date: string; label: string }[];
  is_default: boolean;
};

// ---------------------------------------------------------------- integrations

export type ApiKey = {
  id: Id;
  workspace_id: Id;
  name: string;
  /** Head fragment only — the full key is shown exactly once, at creation. */
  prefix: string;
  scopes: Capability[];
  last_used_at: Timestamp | null;
  expires_at: Timestamp | null;
  created_by: Id;
  created_at: Timestamp;
  revoked_at: Timestamp | null;
};

export type WebhookEndpoint = {
  id: Id;
  workspace_id: Id;
  url: string;
  description: string | null;
  events: string[];
  secret_hint: string;
  enabled: boolean;
  /** §6.16 — endpoints auto-disable after sustained failure. */
  auto_disabled_at: Timestamp | null;
  success_24h: number;
  failure_24h: number;
  consecutive_failures?: number;
  created_by?: Id | null;
  created_at: Timestamp;
  updated_at?: Timestamp;
};

export type WebhookDelivery = {
  id: Id;
  endpoint_id: Id;
  event_id: Id | null;
  event_type: string;
  status: "pending" | "delivered" | "failed" | "exhausted" | "cancelled" | "succeeded" | "dead";
  attempt: number;
  max_attempts?: number;
  response_status: number | null;
  response_body?: string;
  duration_ms: number | null;
  error: string | null;
  next_attempt_at: Timestamp | null;
  created_at: Timestamp;
};

// ---------------------------------------------------------------- operations

export type AuditLog = {
  id: Id;
  workspace_id: Id;
  actor_type: "member" | "customer" | "api_key" | "system";
  actor_id: Id | null;
  actor_name: string;
  action: string;
  entity_type: string;
  entity_id: Id | null;
  request_id: string | null;
  ip: string | null;
  metadata: Record<string, unknown>;
  occurred_at: Timestamp;
};

export type Job = {
  id: Id;
  workspace_id: Id | null;
  queue: string;
  type: string;
  state: "pending" | "running" | "succeeded" | "failed" | "dead" | "cancelled";
  attempt: number;
  max_attempts: number;
  scheduled_at: Timestamp;
  started_at: Timestamp | null;
  finished_at: Timestamp | null;
  error: string | null;
};

export type Notification = {
  id: Id;
  workspace_id: Id;
  type: "assignment" | "mention" | "sla_warning" | "sla_breach" | "reply" | "system";
  title: string;
  body: string;
  entity_type: string | null;
  entity_id: Id | null;
  read_at: Timestamp | null;
  created_at: Timestamp;
};

// ---------------------------------------------------------------- analytics

export type MetricPoint = {
  /** ISO date or datetime, bucketed server-side to the requested granularity. */
  t: string;
  v: number;
};

export type MetricSeries = {
  key: string;
  label: string;
  points: MetricPoint[];
  total: number;
  /** Ratio change against the preceding equal-length window. */
  delta: number | null;
  /** Whether an increase is good. Drives the up/down colour, not the arrow. */
  higher_is_better: boolean;
  unit: "count" | "seconds" | "percent" | "score";
  /** §6.18 — every metric must be able to explain itself in the UI. */
  definition: string;
};
