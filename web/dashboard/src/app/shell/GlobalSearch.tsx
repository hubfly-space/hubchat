import { CommandPalette, Select, Input, api, useQuery, useTheme, type CommandItem, type Paginated } from "@hubchat/shared";
import { BookOpen, Inbox, Moon, Settings, TicketCheck, UserRound, Workflow, MessageSquare } from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

type SearchResult = { kind: string; title: string; snippet: string; entity_id: string; conversation_id: string | null };

/** ⌘K global command and permission-filtered server search. */
export function GlobalSearch({ open, onOpenChange, query, onQueryChange }: { open: boolean; onOpenChange: (open: boolean) => void; query: string; onQueryChange: (query: string) => void }) {
  const navigate = useNavigate();
  const { toggleTheme } = useTheme();
  const [kind, setKind] = useState("all");
  const [channel, setChannel] = useState("all");
  const [after, setAfter] = useState("");
  const params = new URLSearchParams({ q: query.trim(), limit: "30" });
  if (kind !== "all") params.set("kind", kind);
  if (channel !== "all") params.set("channel", channel);
  if (after) params.set("after", new Date(`${after}T00:00:00Z`).toISOString());
  const results = useQuery<Paginated<SearchResult>>(query.trim() ? ["global-search", params.toString()] : null, (signal) => api.get(`/search?${params.toString()}`, { signal }), { enabled: Boolean(query.trim()) });
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
    const routes: Record<string, string> = { customer: "/customers/", company: "/companies/", ticket: "/tickets/", article: "/kb/articles/", feedback: "/feedback/items/" };
    const groups: Record<string, string> = { customer: "Customers", company: "Companies", ticket: "Tickets", article: "Articles", feedback: "Feedback", message: "Conversations" };
    const records = (results.data?.data ?? []).map((item): CommandItem => ({ id: `${item.kind}_${item.entity_id}`, label: item.title, hint: `${item.kind} · ${item.snippet}`, icon: item.kind === "customer" ? <UserRound /> : item.kind === "message" ? <MessageSquare /> : <BookOpen />, group: groups[item.kind] ?? item.kind, onSelect: () => navigate(item.conversation_id ? `/inbox/all/${item.conversation_id}` : `${routes[item.kind] ?? "/customers/"}${item.entity_id}`) }));
    if (!normalized) return commands;
    return [...records, ...commands.filter((command) => command.label.toLowerCase().includes(normalized) || command.keywords?.some((keyword) => keyword.includes(normalized)))];
  }, [query, navigate, results.data, toggleTheme]);
  return <CommandPalette open={open} onOpenChange={onOpenChange} items={items} query={query} onQueryChange={onQueryChange} emptyMessage={results.isError ? "Search is unavailable" : query ? `Nothing matches “${query}”` : "Start typing to search this workspace"} footer={<div className="flex flex-wrap items-center gap-2"><Select aria-label="Search type" size="sm" value={kind} onValueChange={setKind} options={[{ value: "all", label: "All types" }, ...["message", "customer", "company", "ticket", "article", "feedback"].map((value) => ({ value, label: value }))]} /><Select aria-label="Search channel" size="sm" value={channel} onValueChange={setChannel} options={[{ value: "all", label: "All channels" }, ...["widget", "email", "portal", "manual", "api", "form"].map((value) => ({ value, label: value }))]} /><Input aria-label="Search after date" type="date" value={after} onChange={(event) => setAfter(event.target.value)} className="w-36" /><span>Results respect your permissions</span></div>} />;
}
