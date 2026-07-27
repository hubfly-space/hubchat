import { ArrowDown, ArrowUp, ChevronsUpDown } from "lucide-react";
import { Fragment, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { Checkbox } from "./Toggles";
import { SkeletonText } from "./Skeleton";

export type SortDirection = "asc" | "desc";

export type Column<T> = {
  /** Stable key; also the sort field sent to the API. */
  key: string;
  header: ReactNode;
  /** Cell renderer. Receives the row and its index. */
  cell: (row: T, index: number) => ReactNode;
  width?: string;
  align?: "left" | "right" | "center";
  sortable?: boolean;
  /** Numeric columns get tabular figures and right alignment by default. */
  numeric?: boolean;
  /** Hidden below this breakpoint. Keeps wide tables usable on laptops. */
  hideBelow?: "sm" | "md" | "lg" | "xl";
  /** Excluded from the column-visibility menu — identity columns, mostly. */
  locked?: boolean;
};

export type DataTableProps<T> = {
  rows: T[];
  columns: Column<T>[];
  rowKey: (row: T) => string;
  /** Enables the checkbox column and the bulk-action bar. */
  selection?: {
    selected: Set<string>;
    onChange: (selected: Set<string>) => void;
  };
  sort?: {
    key: string;
    direction: SortDirection;
    onChange: (key: string, direction: SortDirection) => void;
  };
  onRowClick?: (row: T) => void;
  /** Trailing per-row action slot; reveals on hover and on keyboard focus. */
  rowActions?: (row: T) => ReactNode;
  /** Groups consecutive rows under a sticky sub-header. */
  groupBy?: (row: T) => string | null;
  loading?: boolean;
  empty?: ReactNode;
  /** Sticks the header to the top of the scroll container. */
  stickyHeader?: boolean;
  className?: string;
  "aria-label": string;
};

const HIDE_BELOW = {
  sm: "hidden sm:table-cell",
  md: "hidden md:table-cell",
  lg: "hidden lg:table-cell",
  xl: "hidden xl:table-cell",
} as const;

/**
 * The single table implementation. Every list surface in the dashboard —
 * tickets, customers, articles, webhooks, audit log, jobs — renders through it,
 * so keyboard behaviour, density, selection semantics, and empty states are
 * identical everywhere.
 *
 * Deliberately *not* virtualised: virtualisation is applied at the call site
 * (see the inbox conversation list) because it forces fixed row heights, which
 * most of these tables do not want.
 */
export function DataTable<T>({
  rows,
  columns,
  rowKey,
  selection,
  sort,
  onRowClick,
  rowActions,
  groupBy,
  loading,
  empty,
  stickyHeader = true,
  className,
  ...aria
}: DataTableProps<T>) {
  const allSelected = selection != null && rows.length > 0 && rows.every((row) => selection.selected.has(rowKey(row)));
  const someSelected = selection != null && rows.some((row) => selection.selected.has(rowKey(row)));

  const toggleAll = () => {
    if (!selection) return;
    selection.onChange(allSelected ? new Set() : new Set(rows.map(rowKey)));
  };

  const toggleRow = (id: string) => {
    if (!selection) return;
    const next = new Set(selection.selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    selection.onChange(next);
  };

  const onSort = (column: Column<T>) => {
    if (!sort || !column.sortable) return;
    const direction: SortDirection =
      sort.key === column.key && sort.direction === "asc" ? "desc" : "asc";
    sort.onChange(column.key, direction);
  };

  if (loading) {
    return (
      <div className={cn("p-4", className)}>
        <SkeletonText lines={8} />
      </div>
    );
  }

  if (rows.length === 0 && empty) {
    return <div className={className}>{empty}</div>;
  }

  let lastGroup: string | null = null;

  return (
    <table
      aria-label={aria["aria-label"]}
      className={cn("w-full border-separate border-spacing-0 text-sm", className)}
    >
      <thead className={cn(stickyHeader && "sticky top-0 z-[var(--z-sticky)]")}>
        <tr>
          {selection && (
            <th scope="col" className={headerCell("w-9 pl-3 pr-0")}>
              <Checkbox
                checked={allSelected ? true : someSelected ? "indeterminate" : false}
                onCheckedChange={toggleAll}
                aria-label={allSelected ? "Deselect all rows" : "Select all rows"}
              />
            </th>
          )}

          {columns.map((column) => {
            const isSorted = sort?.key === column.key;
            const alignment = column.align ?? (column.numeric ? "right" : "left");

            return (
              <th
                key={column.key}
                scope="col"
                style={column.width ? { width: column.width } : undefined}
                aria-sort={isSorted ? (sort.direction === "asc" ? "ascending" : "descending") : undefined}
                className={cn(
                  headerCell(),
                  alignment === "right" && "text-right",
                  alignment === "center" && "text-center",
                  column.hideBelow && HIDE_BELOW[column.hideBelow],
                )}
              >
                {column.sortable && sort ? (
                  <button
                    type="button"
                    onClick={() => onSort(column)}
                    className={cn(
                      "group -mx-1 inline-flex items-center gap-1 rounded-xs px-1 py-0.5",
                      "transition-colors hover:text-fg",
                      alignment === "right" && "flex-row-reverse",
                      isSorted && "text-fg",
                    )}
                  >
                    {column.header}
                    {isSorted ? (
                      sort.direction === "asc" ? (
                        <ArrowUp className="size-3 text-accent-text" />
                      ) : (
                        <ArrowDown className="size-3 text-accent-text" />
                      )
                    ) : (
                      <ChevronsUpDown className="size-3 opacity-0 transition-opacity group-hover:opacity-60" />
                    )}
                  </button>
                ) : (
                  column.header
                )}
              </th>
            );
          })}

          {rowActions && <th scope="col" className={headerCell("w-10")} />}
        </tr>
      </thead>

      <tbody>
        {rows.map((row, index) => {
          const id = rowKey(row);
          const isSelected = selection?.selected.has(id) ?? false;
          const group = groupBy?.(row) ?? null;
          const showGroupHeader = group !== null && group !== lastGroup;
          if (showGroupHeader) lastGroup = group;

          return (
            <Fragment key={id}>
              {showGroupHeader && (
                <tr>
                  <td
                    colSpan={columns.length + (selection ? 1 : 0) + (rowActions ? 1 : 0)}
                    className="border-b border-line bg-inset px-3 py-1 text-2xs font-semibold uppercase tracking-caps text-fg-muted"
                  >
                    {group}
                  </td>
                </tr>
              )}

              <tr
                data-selected={isSelected}
                onClick={onRowClick ? () => onRowClick(row) : undefined}
                tabIndex={onRowClick ? 0 : undefined}
                onKeyDown={
                  onRowClick
                    ? (event) => {
                        if (event.key === "Enter") onRowClick(row);
                      }
                    : undefined
                }
                className={cn(
                  "group/row transition-colors duration-fast",
                  onRowClick && "cursor-pointer",
                  "hover:bg-surface-hover",
                  isSelected && "bg-accent-subtle hover:bg-accent-subtle-hover",
                )}
              >
                {selection && (
                  <td className={bodyCell("pl-3 pr-0")} onClick={(event) => event.stopPropagation()}>
                    <Checkbox
                      checked={isSelected}
                      onCheckedChange={() => toggleRow(id)}
                      aria-label={`Select row ${index + 1}`}
                    />
                  </td>
                )}

                {columns.map((column) => {
                  const alignment = column.align ?? (column.numeric ? "right" : "left");
                  return (
                    <td
                      key={column.key}
                      className={cn(
                        bodyCell(),
                        alignment === "right" && "text-right",
                        alignment === "center" && "text-center",
                        column.numeric && "tabular",
                        column.hideBelow && HIDE_BELOW[column.hideBelow],
                      )}
                    >
                      {column.cell(row, index)}
                    </td>
                  );
                })}

                {rowActions && (
                  <td className={bodyCell("pr-2")} onClick={(event) => event.stopPropagation()}>
                    <div
                      className={cn(
                        "flex justify-end opacity-0 transition-opacity",
                        "group-hover/row:opacity-100 group-focus-within/row:opacity-100",
                      )}
                    >
                      {rowActions(row)}
                    </div>
                  </td>
                )}
              </tr>
            </Fragment>
          );
        })}
      </tbody>
    </table>
  );
}

function headerCell(extra?: string) {
  return cn(
    "border-b border-line bg-surface px-3 py-2 text-left align-middle",
    "text-2xs font-semibold uppercase tracking-caps text-fg-muted",
    "whitespace-nowrap",
    extra,
  );
}

function bodyCell(extra?: string) {
  return cn(
    "border-b border-line-subtle px-3 align-middle",
    "py-[var(--hc-table-cell-py)]",
    extra,
  );
}

/**
 * Floating bar that replaces the toolbar while rows are selected. Anchored to
 * the bottom of the viewport so it does not shift the table.
 */
export function BulkActionBar({
  count,
  onClear,
  children,
}: {
  count: number;
  onClear: () => void;
  children: ReactNode;
}) {
  if (count === 0) return null;

  return (
    <div
      role="toolbar"
      aria-label={`${count} selected`}
      className={cn(
        "fixed bottom-5 left-1/2 z-[var(--z-sticky)] -translate-x-1/2",
        "flex items-center gap-1 rounded-lg border border-line-strong bg-overlay p-1.5 pl-3",
        "shadow-4 inset-shadow-highlight animate-fade-up",
      )}
    >
      <span className="mr-1 text-xs text-fg-secondary">
        <span className="font-semibold text-fg tabular">{count}</span> selected
      </span>
      <span className="mx-1 h-4 w-px bg-line" aria-hidden="true" />
      {children}
      <span className="mx-1 h-4 w-px bg-line" aria-hidden="true" />
      <button
        type="button"
        onClick={onClear}
        className="rounded-sm px-2 py-1 text-xs text-fg-muted transition-colors hover:bg-fill hover:text-fg"
      >
        Clear
      </button>
    </div>
  );
}
