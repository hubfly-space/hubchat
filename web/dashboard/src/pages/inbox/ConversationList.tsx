import {
  Button,
  EmptyState,
  Menu,
  MenuContent,
  MenuLabel,
  MenuRadioGroup,
  MenuRadioItem,
  MenuTrigger,
  SegmentedControl,
  Tooltip,
  cn,
  useTheme,
  type Conversation,
  type Customer,
} from "@hubchat/shared";
import {
  ArrowDownUp,
  CheckCheck,
  CheckSquare,
  Inbox,
  Rows2,
  Rows3,
  UserPlus,
} from "lucide-react";
import { useState } from "react";
import { ConversationRow } from "./ConversationRow";

export type SortKey = "recent" | "oldest" | "priority";

/**
 * Middle pane of the inbox.
 *
 * Not virtualised in this build because the fixture set is small, but the row
 * component is already height-stable and keyed, so dropping in a windowing
 * layer (§17: "virtualize long lists") is a change to this file alone.
 */
export function ConversationList({
  conversations,
  customersById,
  activeId,
  onSelect,
  viewName,
  onBulkAssignToMe,
  onBulkResolve,
  bulkPending,
  hasMore,
  onLoadMore,
  loadingMore,
}: {
  conversations: Conversation[];
  customersById: Map<string, Customer>;
  activeId: string | null;
  onSelect: (id: string) => void;
  viewName: string;
  onBulkAssignToMe: (ids: string[]) => void;
  onBulkResolve: (ids: string[]) => void;
  bulkPending: boolean;
  hasMore: boolean;
  onLoadMore: () => void;
  loadingMore: boolean;
}) {
  const { density, setDensity } = useTheme();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [sort, setSort] = useState<SortKey>("recent");

  const selectionMode = selected.size > 0;

  const toggle = (id: string) => {
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const sorted = [...conversations].sort((a, b) => {
    switch (sort) {
      case "oldest":
        return a.last_message_at.localeCompare(b.last_message_at);
      case "priority": {
        const rank = { urgent: 0, high: 1, normal: 2, low: 3 };
        return rank[a.priority] - rank[b.priority];
      }
      default:
        return b.last_message_at.localeCompare(a.last_message_at);
    }
  });

  return (
    <div className="flex h-full min-h-0 w-list shrink-0 flex-col border-r border-line bg-surface">
      <header className="flex h-topbar shrink-0 items-center justify-between gap-2 border-b border-line px-3">
        {selectionMode ? (
          <>
            <span className="text-xs font-medium tabular text-fg">
              {selected.size} selected
            </span>
            <div className="flex items-center gap-0.5">
              <Tooltip content="Assign to me">
                <Button
                  variant="ghost"
                  size="xs"
                  iconOnly
                  aria-label="Assign to me"
                  leading={<UserPlus />}
                  loading={bulkPending}
                  onClick={() => {
                    onBulkAssignToMe([...selected]);
                    setSelected(new Set());
                  }}
                />
              </Tooltip>
              <Tooltip content="Mark resolved">
                <Button
                  variant="ghost"
                  size="xs"
                  iconOnly
                  aria-label="Resolve"
                  leading={<CheckCheck />}
                  loading={bulkPending}
                  onClick={() => {
                    onBulkResolve([...selected]);
                    setSelected(new Set());
                  }}
                />
              </Tooltip>
              <Button variant="ghost" size="xs" onClick={() => setSelected(new Set())}>
                Cancel
              </Button>
            </div>
          </>
        ) : (
          <>
            <div className="min-w-0">
              <h2 className="truncate text-sm font-semibold text-fg">{viewName}</h2>
              <p className="text-2xs tabular text-fg-muted">
                {conversations.length} conversation{conversations.length === 1 ? "" : "s"}
              </p>
            </div>

            <div className="flex shrink-0 items-center gap-1">
              <Tooltip content="Select conversations">
                <Button
                  variant="ghost"
                  size="xs"
                  iconOnly
                  aria-label="Select conversations"
                  leading={<CheckSquare />}
                  onClick={() => sorted[0] && toggle(sorted[0].id)}
                />
              </Tooltip>

              <Menu>
                <Tooltip content="Sort">
                  <MenuTrigger asChild>
                    <Button variant="ghost" size="xs" iconOnly aria-label="Sort" leading={<ArrowDownUp />} />
                  </MenuTrigger>
                </Tooltip>
                <MenuContent align="end">
                  <MenuLabel>Sort by</MenuLabel>
                  <MenuRadioGroup value={sort} onValueChange={(value) => setSort(value as SortKey)}>
                    <MenuRadioItem value="recent">Most recent activity</MenuRadioItem>
                    <MenuRadioItem value="oldest">Oldest waiting</MenuRadioItem>
                    <MenuRadioItem value="priority">Priority</MenuRadioItem>
                  </MenuRadioGroup>
                </MenuContent>
              </Menu>

              <SegmentedControl
                aria-label="List density"
                value={density}
                onValueChange={setDensity}
                options={[
                  { value: "comfortable", icon: <Rows3 />, ariaLabel: "Comfortable" },
                  { value: "compact", icon: <Rows2 />, ariaLabel: "Compact" },
                ]}
              />
            </div>
          </>
        )}
      </header>

      <div
        role="listbox"
        aria-label={`${viewName} conversations`}
        className={cn("min-h-0 flex-1 overflow-y-auto overscroll-contain")}
      >
        {sorted.length === 0 ? (
          <EmptyState
            icon={Inbox}
            size="sm"
            title="Nothing here"
            description="This view is clear. New conversations appear here the moment they arrive."
          />
        ) : (
          <>
            {sorted.map((conversation) => (
              <ConversationRow
                key={conversation.id}
                conversation={conversation}
                customer={conversation.customer_id ? customersById.get(conversation.customer_id) : undefined}
                active={conversation.id === activeId}
                selected={selected.has(conversation.id)}
                showSelection={selectionMode}
                onSelect={() => onSelect(conversation.id)}
                onToggleSelect={() => toggle(conversation.id)}
              />
            ))}
            {hasMore && (
              <div className="flex justify-center p-3">
                <Button variant="secondary" size="sm" loading={loadingMore} onClick={onLoadMore}>
                  Load more
                </Button>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
