import {
  Button,
  Kbd,
  Menu,
  MenuContent,
  MenuItem,
  MenuLabel,
  MenuSeparator,
  MenuTrigger,
  SegmentedControl,
  StatusDot,
  Tooltip,
  cn,
  formatRelativeShort,
  useTheme,
  type ThemeMode,
} from "@hubchat/shared";
import {
  Bell,
  BookOpen,
  Columns2,
  Inbox,
  MessageSquarePlus,
  Monitor,
  Moon,
  Plus,
  Search,
  Sun,
  TicketPlus,
  UserPlus,
} from "lucide-react";
import { useNavigate } from "react-router-dom";
import { NOW, notifications } from "../../data/fixtures";

export type ConnectionState = "connected" | "reconnecting" | "offline";

export function TopBar({
  onOpenSearch,
  connection,
}: {
  onOpenSearch: () => void;
  connection: ConnectionState;
}) {
  const navigate = useNavigate();
  const unread = notifications.filter((item) => item.read_at === null);
  const { mode, setMode, density, setDensity } = useTheme();

  return (
    <header className="flex h-topbar shrink-0 items-center gap-3 border-b border-line bg-surface px-3">
      {/* Search is a button, not an input. It opens the palette, and pretending
          otherwise (a fake input that steals focus) is a small lie that costs a
          keystroke every time. */}
      <button
        type="button"
        onClick={onOpenSearch}
        className={cn(
          "flex h-7 w-full max-w-md items-center gap-2 rounded-md border border-line bg-inset px-2.5",
          "text-xs text-fg-muted transition-colors",
          "hover:border-line-strong hover:text-fg-secondary",
        )}
      >
        <Search aria-hidden="true" className="size-3.5" />
        <span className="flex-1 text-left">Search conversations, customers, articles…</span>
        <Kbd keys="mod+k" />
      </button>

      <div className="ml-auto flex items-center gap-1">
        <ConnectionIndicator state={connection} />

        <div className="hidden items-center gap-1 md:flex">
          <Tooltip content="Dashboard theme">
            <SegmentedControl
              aria-label="Dashboard theme"
              value={mode}
              onValueChange={(value) => setMode(value as ThemeMode)}
              options={[
                { value: "dark", icon: <Moon />, ariaLabel: "Dark" },
                { value: "light", icon: <Sun />, ariaLabel: "Light" },
                { value: "system", icon: <Monitor />, ariaLabel: "System" },
              ]}
            />
          </Tooltip>

          <Tooltip content={density === "compact" ? "Compact mode on" : "Compact mode off"}>
            <Button
              variant={density === "compact" ? "secondary" : "ghost"}
              size="sm"
              iconOnly
              aria-label={density === "compact" ? "Disable compact mode" : "Enable compact mode"}
              aria-pressed={density === "compact"}
              leading={<Columns2 />}
              onClick={() => setDensity(density === "compact" ? "comfortable" : "compact")}
            />
          </Tooltip>
        </div>

        <Menu>
          <MenuTrigger asChild>
            <Button variant="ghost" size="sm" leading={<Plus />}>
              New
            </Button>
          </MenuTrigger>
          <MenuContent align="end">
            <MenuItem icon={<MessageSquarePlus />} onSelect={() => navigate("/inbox")}>
              Conversation
            </MenuItem>
            <MenuItem icon={<TicketPlus />} onSelect={() => navigate("/tickets")}>
              Ticket
            </MenuItem>
            <MenuItem icon={<UserPlus />} onSelect={() => navigate("/customers")}>
              Customer
            </MenuItem>
            <MenuSeparator />
            <MenuItem icon={<BookOpen />} onSelect={() => navigate("/kb/articles/new")}>
              Article
            </MenuItem>
            <MenuItem icon={<Inbox />} onSelect={() => navigate("/channels/inboxes")}>
              Inbox
            </MenuItem>
          </MenuContent>
        </Menu>

        <NotificationsMenu unreadCount={unread.length} />
      </div>
    </header>
  );
}

function ConnectionIndicator({ state }: { state: ConnectionState }) {
  if (state === "connected") {
    // A healthy realtime connection is the expected case and gets a dot, not a
    // label. Only degradation earns words (§18 degraded modes).
    return (
      <Tooltip content="Realtime connected">
        <span className="mr-1 grid size-7 place-items-center" aria-label="Realtime connected">
          <StatusDot status="live" pulse />
        </span>
      </Tooltip>
    );
  }

  return (
    <span
      role="status"
      className={cn(
        "mr-1 flex items-center gap-1.5 rounded-md px-2 py-1 text-xs",
        state === "reconnecting"
          ? "bg-warning-subtle text-warning-text"
          : "bg-danger-subtle text-danger-text",
      )}
    >
      <StatusDot status={state === "reconnecting" ? "away" : "busy"} />
      {state === "reconnecting" ? "Reconnecting…" : "Offline — messages will queue"}
    </span>
  );
}

function NotificationsMenu({ unreadCount }: { unreadCount: number }) {
  const navigate = useNavigate();

  return (
    <Menu>
      <MenuTrigger asChild>
        <button
          type="button"
          aria-label={`Notifications${unreadCount > 0 ? `, ${unreadCount} unread` : ""}`}
          className="relative grid size-7 place-items-center rounded-md text-fg-muted transition-colors hover:bg-fill hover:text-fg"
        >
          <Bell aria-hidden="true" className="size-4" />
          {unreadCount > 0 && (
            <span className="absolute right-1 top-1 size-1.5 rounded-full bg-accent ring-2 ring-surface" />
          )}
        </button>
      </MenuTrigger>

      <MenuContent align="end" className="w-80 p-0">
        <div className="flex items-center justify-between border-b border-line px-3 py-2">
          <MenuLabel className="p-0">Notifications</MenuLabel>
          <button
            type="button"
            className="text-xs text-accent-text transition-colors hover:underline"
          >
            Mark all read
          </button>
        </div>

        <div className="max-h-96 overflow-y-auto p-1">
          {notifications.map((item) => (
            <button
              key={item.id}
              type="button"
              onClick={() => item.entity_id && navigate(`/inbox/all/${item.entity_id}`)}
              className={cn(
                "flex w-full items-start gap-2.5 rounded-md p-2 text-left transition-colors hover:bg-fill",
                item.read_at === null && "bg-accent-subtle/40",
              )}
            >
              <span
                aria-hidden="true"
                className={cn(
                  "mt-1.5 size-1.5 shrink-0 rounded-full",
                  item.read_at === null ? "bg-accent" : "bg-transparent",
                )}
              />
              <span className="min-w-0 flex-1">
                <span className="flex items-baseline justify-between gap-2">
                  <span className="truncate text-xs font-medium text-fg">{item.title}</span>
                  <span className="shrink-0 text-2xs tabular text-fg-muted">
                    {formatRelativeShort(item.created_at, NOW)}
                  </span>
                </span>
                <span className="mt-0.5 block truncate text-xs text-fg-muted">{item.body}</span>
              </span>
            </button>
          ))}
        </div>

        <div className="border-t border-line p-1">
          <MenuItem onSelect={() => navigate("/settings/notifications")}>
            Notification settings
          </MenuItem>
        </div>
      </MenuContent>
    </Menu>
  );
}
