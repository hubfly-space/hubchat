import { CommandPalette, api, useQuery, useTheme, type CommandItem } from "@hubchat/shared";
import { BookOpen, Inbox, Moon, Settings, TicketCheck, UserRound, Workflow, MessageSquare } from "lucide-react";
import { useMemo } from "react";
import { useNavigate } from "react-router-dom";

type SearchResult = { kind: string; title: string; snippet: string; entity_id: string; conversation_id: string | null };

/** ⌘K global command and permission-filtered server search. */
export function GlobalSearch({ open, onOpenChange, query, onQueryChange }: { open: boolean; onOpenChange: (open: boolean) => void; query: string; onQueryChange: (query: string) => void }) {
  const navigate = useNavigate();
  const { toggleTheme } = useTheme();
  const results = useQuery<{ data: SearchResult[] }>(query.trim() ? ["global-search", query.trim()] : null, (signal) => api.get(`/search?q=${encodeURIComponent(query.trim())}&limit=20`, { signal }), { enabled: Boolean(query.trim()) });
  const items = useMemo<CommandItem[]>(() => {
    const normalized = query.trim().toLowerCase();
    const commands: CommandItem[] = [
      { id: "cmd_inbox", label: "Go to inbox", icon: <Inbox />, group: "Commands", shortcut: "g i", onSelect: () => navigate("/inbox") },
      { id: "cmd_tickets", label: "Go to tickets", icon: <TicketCheck />, group: "Commands", shortcut: "g t", onSelect: () => navigate("/tickets") },
      { id: "cmd_customers", label: "Go to customers", icon: <UserRound />, group: "Commands", shortcut: "g c", onSelect: () => navigate("/customers") },
      { id: "cmd_rules", label: "Go to automation rules", icon: <Workflow />, group: "Commands", onSelect: () => navigate("/automation/rules") },
      { id: "cmd_settings", label: "Open workspace settings", icon: <Settings />, group: "Commands", onSelect: () => navigate("/settings/general") },
      { id: "cmd_theme", label: "Toggle light / dark theme", icon: <Moon />, group: "Commands", keywords: ["appearance", "dark mode"], onSelect: toggleTheme },
    ];
    const records = (results.data?.data ?? []).map((item): CommandItem => ({ id: `${item.kind}_${item.entity_id}`, label: item.title, hint: `${item.kind} · ${item.snippet}`, icon: item.kind === "customer" ? <UserRound /> : item.kind === "message" ? <MessageSquare /> : <BookOpen />, group: item.kind === "customer" ? "Customers" : "Conversations", onSelect: () => navigate(item.conversation_id ? `/inbox/all/${item.conversation_id}` : `/customers/${item.entity_id}`) }));
    if (!normalized) return commands;
    return [...records, ...commands.filter((command) => command.label.toLowerCase().includes(normalized) || command.keywords?.some((keyword) => keyword.includes(normalized)))];
  }, [query, navigate, results.data, toggleTheme]);
  return <CommandPalette open={open} onOpenChange={onOpenChange} items={items} query={query} onQueryChange={onQueryChange} emptyMessage={results.isError ? "Search is unavailable" : query ? `Nothing matches “${query}”` : "Start typing to search this workspace"} footer={<span>Results respect your permissions</span>} />;
}
