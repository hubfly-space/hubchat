import { ChevronLeft, ChevronRight } from "lucide-react";
import { cn } from "../lib/cn";
import { Button } from "./Button";
import { Select } from "./Select";

export type PaginationProps = {
  /** Cursor pagination only — §16 mandates it, so there is no page-number UI. */
  hasPrevious: boolean;
  hasNext: boolean;
  onPrevious: () => void;
  onNext: () => void;
  /** Range label, e.g. "1–50 of 1,284" or "50 shown". */
  summary?: string;
  pageSize?: number;
  onPageSizeChange?: (size: number) => void;
  className?: string;
};

export function Pagination({
  hasPrevious,
  hasNext,
  onPrevious,
  onNext,
  summary,
  pageSize,
  onPageSizeChange,
  className,
}: PaginationProps) {
  return (
    <div
      className={cn(
        "flex shrink-0 items-center justify-between gap-4 border-t border-line bg-surface px-3 py-2",
        className,
      )}
    >
      <div className="flex items-center gap-3">
        {summary && <span className="text-xs tabular text-fg-muted">{summary}</span>}
        {pageSize != null && onPageSizeChange && (
          <Select
            size="sm"
            value={String(pageSize)}
            onValueChange={(value) => onPageSizeChange(Number(value))}
            aria-label="Rows per page"
            className="w-24"
            options={[25, 50, 100, 200].map((size) => ({
              value: String(size),
              label: `${size} rows`,
            }))}
          />
        )}
      </div>

      <div className="flex items-center gap-1">
        <Button
          variant="ghost"
          size="sm"
          disabled={!hasPrevious}
          onClick={onPrevious}
          leading={<ChevronLeft />}
        >
          Previous
        </Button>
        <Button
          variant="ghost"
          size="sm"
          disabled={!hasNext}
          onClick={onNext}
          trailing={<ChevronRight />}
        >
          Next
        </Button>
      </div>
    </div>
  );
}
