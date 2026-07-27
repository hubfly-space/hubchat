import * as RadixDialog from "@radix-ui/react-dialog";
import { Command } from "cmdk";
import { CornerDownLeft, Search } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../lib/cn";
import { Kbd } from "./Kbd";

export type CommandItem = {
  id: string;
  label: string;
  /** Secondary line — the customer's email, the ticket's subject, the article's collection. */
  hint?: string;
  icon?: ReactNode;
  group: string;
  shortcut?: string;
  /** Extra tokens matched by the fuzzy filter but never displayed. */
  keywords?: string[];
  onSelect: () => void;
};

export type CommandPaletteProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  items: CommandItem[];
  query: string;
  onQueryChange: (query: string) => void;
  placeholder?: string;
  loading?: boolean;
  emptyMessage?: string;
  /** Contextual line above the input — "Searching in Support inbox". */
  scope?: ReactNode;
  footer?: ReactNode;
};

/**
 * ⌘K. Serves double duty as global search (§6.17) and as the command surface,
 * because an agent should not have to know in advance whether "resolve" is a
 * thing they type or a button they hunt for.
 *
 * Filtering is `shouldFilter={false}` — results come from the server so that
 * permissions are applied before anything reaches the client (§6.17: sensitive
 * data must not appear for users without permission).
 */
export function CommandPalette({
  open,
  onOpenChange,
  items,
  query,
  onQueryChange,
  placeholder = "Search or run a command…",
  loading,
  emptyMessage = "No results",
  scope,
  footer,
}: CommandPaletteProps) {
  const groups = groupItems(items);

  return (
    <RadixDialog.Root open={open} onOpenChange={onOpenChange}>
      <RadixDialog.Portal>
        <RadixDialog.Overlay className="fixed inset-0 z-[var(--z-overlay)] bg-scrim backdrop-blur-[2px] data-[state=open]:animate-fade" />
        <RadixDialog.Content
          aria-label="Command palette"
          className={cn(
            "fixed left-1/2 top-[12vh] z-[var(--z-dialog)] w-[calc(100vw-2rem)] max-w-xl -translate-x-1/2",
            "overflow-hidden rounded-xl border border-line-strong bg-overlay shadow-4 inset-shadow-highlight",
            "data-[state=open]:animate-zoom-in",
          )}
        >
          <RadixDialog.Title className="sr-only">Search and commands</RadixDialog.Title>

          <Command shouldFilter={false} loop className="flex flex-col">
            {scope && (
              <div className="flex items-center gap-2 border-b border-line px-3 py-1.5 text-2xs text-fg-muted">
                {scope}
              </div>
            )}

            <div className="flex items-center gap-2.5 border-b border-line px-3.5">
              <Search aria-hidden="true" className="size-4 shrink-0 text-fg-muted" />
              <Command.Input
                value={query}
                onValueChange={onQueryChange}
                placeholder={placeholder}
                className="h-12 flex-1 bg-transparent text-md text-fg outline-none placeholder:text-fg-disabled"
              />
              <Kbd keys="esc" />
            </div>

            <Command.List className="max-h-[min(24rem,60vh)] overflow-y-auto overscroll-contain p-1.5">
              {loading && (
                <div className="px-2 py-8 text-center text-xs text-fg-muted">Searching…</div>
              )}

              {!loading && (
                <Command.Empty className="px-2 py-10 text-center text-xs text-fg-muted">
                  {emptyMessage}
                </Command.Empty>
              )}

              {groups.map(({ name, entries }) => (
                <Command.Group
                  key={name}
                  heading={name}
                  className={cn(
                    "[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:pb-1 [&_[cmdk-group-heading]]:pt-2.5",
                    "[&_[cmdk-group-heading]]:text-2xs [&_[cmdk-group-heading]]:font-semibold",
                    "[&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-caps",
                    "[&_[cmdk-group-heading]]:text-fg-muted",
                  )}
                >
                  {entries.map((item) => (
                    <Command.Item
                      key={item.id}
                      value={item.id}
                      keywords={item.keywords}
                      onSelect={() => {
                        item.onSelect();
                        onOpenChange(false);
                      }}
                      className={cn(
                        "flex cursor-default select-none items-center gap-2.5 rounded-md px-2 py-2 text-sm",
                        "text-fg-secondary outline-none",
                        "data-[selected=true]:bg-accent-subtle data-[selected=true]:text-fg",
                      )}
                    >
                      {item.icon && (
                        <span className="shrink-0 text-fg-muted [&_svg]:size-4">{item.icon}</span>
                      )}
                      <span className="min-w-0 flex-1">
                        <span className="block truncate">{item.label}</span>
                        {item.hint && (
                          <span className="block truncate text-xs text-fg-muted">{item.hint}</span>
                        )}
                      </span>
                      {item.shortcut && <Kbd keys={item.shortcut} className="shrink-0" />}
                      <CornerDownLeft
                        aria-hidden="true"
                        className="size-3 shrink-0 text-fg-disabled opacity-0 [[data-selected=true]_&]:opacity-100"
                      />
                    </Command.Item>
                  ))}
                </Command.Group>
              ))}
            </Command.List>

            <div className="flex items-center justify-between gap-3 border-t border-line bg-inset px-3 py-2 text-2xs text-fg-muted">
              <div className="flex items-center gap-3">
                <span className="flex items-center gap-1">
                  <Kbd keys="up" />
                  <Kbd keys="down" />
                  navigate
                </span>
                <span className="flex items-center gap-1">
                  <Kbd keys="enter" />
                  select
                </span>
              </div>
              {footer}
            </div>
          </Command>
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}

function groupItems(items: CommandItem[]) {
  const groups: { name: string; entries: CommandItem[] }[] = [];
  for (const item of items) {
    const existing = groups.find((group) => group.name === item.group);
    if (existing) existing.entries.push(item);
    else groups.push({ name: item.group, entries: [item] });
  }
  return groups;
}
