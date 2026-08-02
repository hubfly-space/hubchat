import type { Capability } from "@hubchat/shared";
import {
  Activity,
  AtSign,
  BarChart3,
  BookOpen,
  Boxes,
  Braces,
  Building2,
  CalendarClock,
  ClipboardList,
  Clock,
  Code2,
  Database,
  FileText,
  Gauge,
  Inbox,
  KeyRound,
  LayoutDashboard,
  Lightbulb,
  Megaphone,
  ListChecks,
  type LucideIcon,
  MessageSquareReply,
  Radio,
  Route,
  ScrollText,
  Settings,
  ShieldCheck,
  Sparkles,
  Star,
  Tags,
  TicketCheck,
  Timer,
  Users,
  UsersRound,
  Webhook,
  Workflow,
} from "lucide-react";

export type NavLeaf = {
  label: string;
  to: string;
  icon?: LucideIcon;
  /** Hidden unless the viewer holds this capability (§5.9). */
  capability?: Capability;
  badge?: "inbox-unread";
  /** Matches child routes too, e.g. /tickets/:id keeps /tickets active. */
  matchPrefix?: boolean;
};

export type NavGroup = {
  label: string | null;
  items: NavLeaf[];
};

export type NavSection = {
  id: string;
  label: string;
  icon: LucideIcon;
  /** Where clicking the rail icon lands. */
  to: string;
  /** Route prefix that marks this section active. */
  match: string;
  capability?: Capability;
  /**
   * Contextual sidebar content. A section with no groups renders no sidebar —
   * Overview is a single page and does not need one.
   */
  groups?: NavGroup[];
  /** Rendered in the rail's lower cluster instead of the primary cluster. */
  footer?: boolean;
};

/**
 * The navigation model.
 *
 * Two levels, never three. The rail answers "which part of the product", the
 * sidebar answers "which page". Anything that would be a third level is either
 * a tab on the page or a route parameter — a support tool loses badly when the
 * operator has to remember a path.
 */
export const NAV_SECTIONS: NavSection[] = [
  {
    id: "overview",
    label: "Overview",
    icon: LayoutDashboard,
    to: "/overview",
    match: "/overview",
  },

  {
    id: "inbox",
    label: "Inbox",
    icon: Inbox,
    to: "/inbox",
    match: "/inbox",
    capability: "conversation.read",
    // The inbox sidebar is the saved-view list; it is data-driven and rendered
    // by InboxSidebar rather than declared here.
  },

  {
    id: "tasks",
    label: "Tasks",
    icon: ListChecks,
    to: "/tasks",
    match: "/tasks",
    capability: "task.manage",
  },

  {
    id: "tickets",
    label: "Tickets",
    icon: TicketCheck,
    to: "/tickets",
    match: "/tickets",
    capability: "conversation.read",
  },

  {
    id: "customers",
    label: "Customers",
    icon: Users,
    to: "/customers",
    match: "/customers",
    capability: "customer.read",
    groups: [
      {
        label: null,
        items: [
          { label: "People", to: "/customers", icon: Users, matchPrefix: true },
          { label: "Companies", to: "/companies", icon: Building2, matchPrefix: true },
        ],
      },
    ],
  },

  {
    id: "feedback",
    label: "Feedback",
    icon: Lightbulb,
    to: "/feedback",
    match: "/feedback",
    groups: [
      {
        label: null,
        items: [
          { label: "Boards", to: "/feedback", icon: Boxes },
          { label: "Roadmap", to: "/feedback/roadmap", icon: Route },
        ],
      },
    ],
  },

  {
    id: "content",
    label: "Content",
    icon: BookOpen,
    to: "/kb",
    match: "/kb",
    groups: [
      {
        label: "Knowledge base",
        items: [
          { label: "Articles", to: "/kb", icon: FileText },
          { label: "Collections", to: "/kb/collections", icon: Boxes },
          { label: "Search analytics", to: "/kb/search-analytics", icon: Activity },
          { label: "Changelog", to: "/kb/changelog", icon: Megaphone },
        ],
      },
      {
        label: "Collection",
        items: [
          { label: "Surveys", to: "/surveys", icon: Star, matchPrefix: true },
          { label: "Forms", to: "/forms", icon: ClipboardList, matchPrefix: true },
        ],
      },
    ],
  },

  {
    id: "reports",
    label: "Reports",
    icon: BarChart3,
    to: "/reports",
    match: "/reports",
    capability: "report.read",
    groups: [
      {
        label: null,
        items: [
          { label: "Overview", to: "/reports", icon: Gauge },
          { label: "Support operations", to: "/reports/support", icon: Timer },
          { label: "Customer experience", to: "/reports/experience", icon: Star },
          { label: "Widget & portal", to: "/reports/surfaces", icon: Radio },
        ],
      },
    ],
  },

  {
    id: "channels",
    label: "Channels",
    icon: Radio,
    to: "/channels/widgets",
    match: "/channels",
    capability: "widget.manage",
    groups: [
      {
        label: "Customer surfaces",
        items: [
          { label: "Widgets", to: "/channels/widgets", icon: Sparkles, matchPrefix: true },
          { label: "Portals", to: "/channels/portals", icon: Boxes, matchPrefix: true },
        ],
      },
      {
        label: "Routing",
        items: [
          { label: "Inboxes", to: "/channels/inboxes", icon: Inbox },
          { label: "Email channel", to: "/channels/email", icon: AtSign },
        ],
      },
    ],
  },

  {
    id: "automation",
    label: "Automation",
    icon: Workflow,
    to: "/automation/rules",
    match: "/automation",
    capability: "automation.manage",
    groups: [
      {
        label: "Rules",
        items: [
          { label: "Rules", to: "/automation/rules", icon: Workflow, matchPrefix: true },
          { label: "Execution log", to: "/automation/executions", icon: ScrollText },
        ],
      },
      {
        label: "Agent shortcuts",
        items: [
          { label: "Macros", to: "/automation/macros", icon: ListChecks },
          { label: "Saved replies", to: "/automation/replies", icon: MessageSquareReply },
        ],
      },
      {
        label: "Service levels",
        items: [
          { label: "SLA policies", to: "/sla/policies", icon: Timer, matchPrefix: true },
          { label: "Business hours", to: "/sla/calendars", icon: CalendarClock },
        ],
      },
    ],
  },

  {
    id: "developers",
    label: "Developers",
    icon: Code2,
    to: "/developers/keys",
    match: "/developers",
    capability: "integration.manage",
    groups: [
      {
        label: "Access",
        items: [
          { label: "API keys", to: "/developers/keys", icon: KeyRound },
          { label: "Webhooks", to: "/developers/webhooks", icon: Webhook, matchPrefix: true },
        ],
      },
      {
        label: "Data",
        items: [
          { label: "Event stream", to: "/developers/events", icon: Activity },
          { label: "Metadata schema", to: "/developers/metadata", icon: Braces },
        ],
      },
      {
        label: "Reference",
        items: [{ label: "SDK & install", to: "/developers/sdk", icon: Code2 }],
      },
    ],
  },

  {
    id: "settings",
    label: "Settings",
    icon: Settings,
    to: "/settings/general",
    match: "/settings",
    footer: true,
    groups: [
      {
        label: "Workspace",
        items: [
          { label: "General", to: "/settings/general", icon: Settings },
          { label: "Branding", to: "/settings/branding", icon: Sparkles },
          { label: "Tags", to: "/settings/tags", icon: Tags },
          { label: "Ticket fields", to: "/settings/fields", icon: ListChecks },
          { label: "Business hours", to: "/settings/hours", icon: Clock },
        ],
      },
      {
        label: "People",
        items: [
          { label: "Members", to: "/settings/members", icon: UsersRound, capability: "member.manage" },
          { label: "Teams", to: "/settings/teams", icon: Users },
          { label: "Roles & permissions", to: "/settings/roles", icon: ShieldCheck },
        ],
      },
      {
        label: "Governance",
        items: [
          { label: "Security", to: "/settings/security", icon: ShieldCheck, capability: "workspace.manage_security" },
          { label: "Privacy & retention", to: "/settings/privacy", icon: ShieldCheck },
          { label: "Import & export", to: "/settings/data", icon: Database },
          { label: "Audit log", to: "/settings/audit", icon: ScrollText, capability: "audit.read" },
        ],
      },
      {
        label: "Operations",
        items: [
          { label: "Notifications", to: "/settings/notifications", icon: Activity },
          { label: "Background jobs", to: "/settings/jobs", icon: Activity },
          { label: "Usage & limits", to: "/settings/limits", icon: Gauge },
        ],
      },
    ],
  },
];

export function sectionForPath(pathname: string): NavSection | undefined {
  // Longest match wins so /settings/general does not resolve to a shorter prefix.
  return [...NAV_SECTIONS]
    .sort((a, b) => b.match.length - a.match.length)
    .find((section) => pathname.startsWith(section.match)) ??
    NAV_SECTIONS.find((section) =>
      section.groups?.some((group) =>
        group.items.some((item) => pathname.startsWith(item.to)),
      ),
    );
}
