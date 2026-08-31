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
  api,
  cn,
  formatRelativeShort,
  idempotencyKey,
  useInfinite,
  useMutation,
  useQuery,
  useToast,
  useTheme,
  type ThemeMode,
  type Paginated,
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
import { useEffect, useRef } from "react";
import { useNavigate } from "react-router-dom";

export type ConnectionState = "connected" | "reconnecting" | "offline";

export function TopBar({
  onOpenSearch,
  connection,
}: {
  onOpenSearch: () => void;
  connection: ConnectionState;
}) {
  const navigate = useNavigate();
  const count = useQuery<{ count: number }>(["notifications-count"], (signal) =>
    api.get("/notifications/count", { signal }),
    { refetchInterval: 10_000 },
  );
  const notifications = useInfinite<LiveNotification>(["notifications"], (cursor, signal) => {
    const params = new URLSearchParams({ limit: "20" });
    if (cursor) params.set("cursor", cursor);
    return api.get<Paginated<LiveNotification>>(`/notifications?${params.toString()}`, { signal });
  }, { refetchInterval: 10_000 });
  const preferences = useQuery<{ data: NotificationPreference[] }>(["notification-preferences"], (signal) =>
    api.get("/notifications/preferences", { signal }),
    { staleTime: 60_000 },
  );
  const { mode, setMode, density, setDensity } = useTheme();
  const toast = useToast();

  useLiveNotificationPopups(notifications.items, preferences.data?.data ?? [], preferences.isSuccess, navigate, toast);

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

        <NotificationsMenu
          unreadCount={count.data?.count ?? 0}
          items={notifications.items}
          loading={notifications.isLoading}
          error={Boolean(notifications.error)}
          hasMore={notifications.hasMore}
          loadMore={() => void notifications.fetchNext()}
          loadingMore={notifications.isFetching && !notifications.isLoading}
        />
      </div>
    </header>
  );
}

type NotificationPreference = { type: string; in_app: boolean; browser: boolean; sound: boolean };

const defaultBrowserTypes = new Set(["assignment", "mention", "reply", "sla_warning", "sla_breach"]);

function useLiveNotificationPopups(
  items: LiveNotification[],
  preferences: NotificationPreference[],
  preferencesReady: boolean,
  navigate: ReturnType<typeof useNavigate>,
  toast: ReturnType<typeof useToast>,
) {
  const seen = useRef<Set<string> | null>(null);
  const audioContext = useRef<AudioContext | null>(null);
  useEffect(() => {
    if (!preferencesReady || seen.current === null) {
      if (preferencesReady && seen.current === null) seen.current = new Set(items.map((item) => item.id));
      return;
    }
    const canBrowserNotify = "Notification" in window && window.Notification.permission === "granted";
    const browserEnabled = new Set(preferences.filter((item) => item.browser).map((item) => item.type));
    const inAppEnabled = new Set(preferences.filter((item) => item.in_app).map((item) => item.type));
    const soundEnabled = new Set(preferences.filter((item) => item.sound).map((item) => item.type));
    const savedTypes = new Set(preferences.map((item) => item.type));
    items.forEach((item) => {
      if (seen.current?.has(item.id)) return;
      seen.current?.add(item.id);
      const preferenceType = item.type === "customer_reply" ? "reply" : item.type;
      if (item.read_at !== null) return;
      const open = () => {
        if (item.url) navigate(item.url);
        else if (item.entity_id) navigate(`/inbox?conversation=${encodeURIComponent(item.entity_id)}`);
      };
      // In-app popups work without browser permission. Missing preference rows
      // use the same defaults shown on the notification settings screen.
      if (inAppEnabled.has(preferenceType) || !savedTypes.has(preferenceType)) {
        toast.toast({
          id: `notification_${item.id}`,
          title: item.title,
          description: item.body,
          duration: item.type === "sla_breach" ? 0 : 8000,
          tone: item.type.startsWith("sla_") ? "warning" : "neutral",
          action: { label: "Open", onClick: open },
        });
      }
      if (soundEnabled.has(preferenceType)) playNotificationSound(audioContext);
      const browserAllowed = browserEnabled.has(preferenceType) ||
        (!savedTypes.has(preferenceType) && defaultBrowserTypes.has(preferenceType));
      if (!canBrowserNotify || !browserAllowed) return;
      const popup = new window.Notification(item.title, { body: item.body });
      popup.onclick = () => {
        window.focus();
        open();
        popup.close();
      };
    });
  }, [items, preferences, preferencesReady, navigate, toast]);
}

/**
 * Play a short, generated tone so the dashboard does not need another static
 * asset. AudioContext is created lazily and every browser-policy failure is
 * ignored: sound is an enhancement, never a reason to break notifications.
 */
function playNotificationSound(contextRef: { current: AudioContext | null }) {
  const AudioContextConstructor = window.AudioContext ||
    (window as Window & { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
  if (!AudioContextConstructor) return;

  try {
    const context = contextRef.current ?? (contextRef.current = new AudioContextConstructor());
    const oscillator = context.createOscillator();
    const gain = context.createGain();
    const start = context.currentTime;
    oscillator.type = "sine";
    oscillator.frequency.setValueAtTime(880, start);
    gain.gain.setValueAtTime(0.0001, start);
    gain.gain.exponentialRampToValueAtTime(0.08, start + 0.01);
    gain.gain.exponentialRampToValueAtTime(0.0001, start + 0.16);
    oscillator.connect(gain);
    gain.connect(context.destination);
    oscillator.start(start);
    oscillator.stop(start + 0.17);
    void context.resume().catch(() => {});
  } catch {
    // Browser audio permissions and older WebViews are allowed to reject this.
  }
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

type LiveNotification = {
  id: string;
  type: string;
  title: string;
  body: string;
  entity_type: string | null;
  entity_id: string | null;
  url: string | null;
  read_at: string | null;
  created_at: string;
};

function NotificationsMenu({
  unreadCount,
  items,
  loading,
  error,
  hasMore,
  loadMore,
  loadingMore,
}: {
  unreadCount: number;
  items: LiveNotification[];
  loading: boolean;
  error: boolean;
  hasMore: boolean;
  loadMore: () => void;
  loadingMore: boolean;
}) {
  const navigate = useNavigate();
  const markRead = useMutation<string, void>(
    (id) => api.post(`/notifications/${id}/read`),
    { invalidates: [["notifications"], ["notifications-count"]] },
  );
  const markAllRead = useMutation<void, void>(
    () => api.post("/notifications/read-all", undefined, { idempotencyKey: idempotencyKey() }),
    { invalidates: [["notifications"], ["notifications-count"]] },
  );

  const openNotification = (item: LiveNotification) => {
    if (item.read_at === null) void markRead.mutate(item.id).catch(() => {});
    if (item.url) navigate(item.url);
    else if (item.entity_id) navigate(`/inbox?conversation=${encodeURIComponent(item.entity_id)}`);
  };

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
            disabled={markAllRead.isPending || unreadCount === 0}
            onClick={() => void markAllRead.mutate().catch(() => {})}
            className="text-xs text-accent-text transition-colors hover:underline"
          >
            Mark all read
          </button>
        </div>

        <div className="max-h-96 overflow-y-auto p-1">
          {loading ? (
            <p className="px-2 py-5 text-center text-xs text-fg-muted">Loading notifications…</p>
          ) : error ? (
            <p className="px-2 py-5 text-center text-xs text-danger">Could not load notifications.</p>
          ) : items.length === 0 ? (
            <p className="px-2 py-5 text-center text-xs text-fg-muted">You’re all caught up.</p>
          ) : items.map((item) => (
            <button
              key={item.id}
              type="button"
              onClick={() => openNotification(item)}
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
                    {formatRelativeShort(item.created_at)}
                  </span>
                </span>
                <span className="mt-0.5 block truncate text-xs text-fg-muted">{item.body}</span>
              </span>
            </button>
          ))}
          {!loading && !error && hasMore && <button type="button" onClick={loadMore} disabled={loadingMore} className="w-full rounded-md px-2 py-2 text-center text-xs text-accent-text hover:bg-fill disabled:opacity-60">{loadingMore ? "Loading older…" : "Load older notifications"}</button>}
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
