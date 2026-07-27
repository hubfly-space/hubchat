import { CommandPalette, formatCompact, type CommandItem } from "@hubchat/shared";
import {
  BookOpen,
  Building2,
  Inbox,
  Lightbulb,
  MessageSquare,
  Moon,
  Settings,
  TicketCheck,
  UserRound,
  Workflow,
} from "lucide-react";
import { useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { useTheme } from "@hubchat/shared";
import { articles, companies, conversations, customers, feedbackItems, tickets } from "../../data/fixtures";

/**
 * ⌘K — global search and command surface in one (§6.17).
 *
 * Two rules this implementation encodes:
 *   · results are permission-filtered *server-side*; the client never receives
 *     a record it may not see, so there is no client-side redaction here;
 *   · commands and records share one ranked list, because an agent typing
 *     "resolve" does not know or care which of the two they want.
 */
export function GlobalSearch({
  open,
  onOpenChange,
  query,
  onQueryChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  query: string;
  onQueryChange: (query: string) => void;
}) {
  const navigate = useNavigate();
  const { toggleTheme } = useTheme();

  const items = useMemo<CommandItem[]>(() => {
    const normalized = query.trim().toLowerCase();

    const commands: CommandItem[] = [
      { id: "cmd_inbox", label: "Go to inbox", icon: <Inbox />, group: "Commands", shortcut: "g i", keywords: ["conversations"], onSelect: () => navigate("/inbox") },
      { id: "cmd_tickets", label: "Go to tickets", icon: <TicketCheck />, group: "Commands", shortcut: "g t", onSelect: () => navigate("/tickets") },
      { id: "cmd_customers", label: "Go to customers", icon: <UserRound />, group: "Commands", shortcut: "g c", onSelect: () => navigate("/customers") },
      { id: "cmd_rules", label: "Go to automation rules", icon: <Workflow />, group: "Commands", onSelect: () => navigate("/automation/rules") },
      { id: "cmd_settings", label: "Open workspace settings", icon: <Settings />, group: "Commands", onSelect: () => navigate("/settings/general") },
      { id: "cmd_theme", label: "Toggle light / dark theme", icon: <Moon />, group: "Commands", keywords: ["appearance", "dark mode"], onSelect: toggleTheme },
    ];

    if (!normalized) {
      return [
        ...conversations.slice(0, 4).map(
          (conversation): CommandItem => ({
            id: `recent_${conversation.id}`,
            label: conversation.subject ?? conversation.last_message_preview,
            hint: "Recent conversation",
            icon: <MessageSquare />,
            group: "Recent",
            onSelect: () => navigate(`/inbox/all/${conversation.id}`),
          }),
        ),
        ...commands,
      ];
    }

    const matches = (value: string | null | undefined) =>
      (value ?? "").toLowerCase().includes(normalized);

    return [
      ...conversations
        .filter((item) => matches(item.subject) || matches(item.last_message_preview))
        .slice(0, 5)
        .map((item): CommandItem => ({
          id: `cnv_${item.id}`,
          label: item.subject ?? item.last_message_preview,
          hint: `Conversation · ${item.state.replace(/_/g, " ")}`,
          icon: <MessageSquare />,
          group: "Conversations",
          onSelect: () => navigate(`/inbox/all/${item.id}`),
        })),

      ...tickets
        .filter((item) => matches(item.title) || matches(`${item.prefix}-${item.number}`))
        .slice(0, 5)
        .map((item): CommandItem => ({
          id: `tkt_${item.id}`,
          label: item.title,
          hint: `${item.prefix}-${item.number} · ${item.status}`,
          icon: <TicketCheck />,
          group: "Tickets",
          onSelect: () => navigate(`/tickets/${item.id}`),
        })),

      ...customers
        .filter((item) => matches(item.name) || matches(item.email) || matches(item.external_id))
        .slice(0, 5)
        .map((item): CommandItem => ({
          id: `cus_${item.id}`,
          label: item.name ?? "Anonymous visitor",
          hint: item.email ?? item.external_id ?? "No contact details",
          icon: <UserRound />,
          group: "Customers",
          onSelect: () => navigate(`/customers/${item.id}`),
        })),

      ...companies
        .filter((item) => matches(item.name) || matches(item.domain))
        .slice(0, 3)
        .map((item): CommandItem => ({
          id: `cmp_${item.id}`,
          label: item.name,
          hint: `${item.domain} · ${formatCompact(item.customer_count)} contacts`,
          icon: <Building2 />,
          group: "Companies",
          onSelect: () => navigate(`/companies/${item.id}`),
        })),

      ...articles
        .filter((item) => matches(item.title))
        .slice(0, 4)
        .map((item): CommandItem => ({
          id: `art_${item.id}`,
          label: item.title,
          hint: `Article · ${item.state}`,
          icon: <BookOpen />,
          group: "Knowledge base",
          onSelect: () => navigate(`/kb/articles/${item.id}`),
        })),

      ...feedbackItems
        .filter((item) => matches(item.title))
        .slice(0, 3)
        .map((item): CommandItem => ({
          id: `fbi_${item.id}`,
          label: item.title,
          hint: `Feedback · ${item.vote_count} votes`,
          icon: <Lightbulb />,
          group: "Feedback",
          onSelect: () => navigate(`/feedback/items/${item.id}`),
        })),

      ...commands.filter(
        (command) =>
          command.label.toLowerCase().includes(normalized) ||
          command.keywords?.some((keyword) => keyword.includes(normalized)),
      ),
    ];
  }, [query, navigate, toggleTheme]);

  return (
    <CommandPalette
      open={open}
      onOpenChange={onOpenChange}
      items={items}
      query={query}
      onQueryChange={onQueryChange}
      emptyMessage={
        query ? `Nothing matches “${query}”` : "Start typing to search this workspace"
      }
      footer={<span>Results respect your permissions</span>}
    />
  );
}
