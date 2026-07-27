import { ChevronDown, ListFilter, Plus, X } from "lucide-react";
import { useState, type ReactNode } from "react";
import { cn } from "../lib/cn";
import type { FilterCondition, FilterOperator, FieldValue } from "../types";
import { Button } from "./Button";
import { Menu, MenuContent, MenuItem, MenuLabel, MenuSeparator, MenuTrigger } from "./Menu";
import { Popover, PopoverContent, PopoverTrigger } from "./Popover";
import { SearchInput } from "./Input";

export type FilterFieldDef = {
  key: string;
  label: string;
  icon?: ReactNode;
  /** Constrains the operator menu to what makes sense for this field. */
  operators: FilterOperator[];
  /** Static options, or omitted for free-text / numeric fields. */
  options?: { value: string; label: string; icon?: ReactNode }[];
  group?: string;
};

const OPERATOR_LABEL: Record<FilterOperator, string> = {
  is: "is",
  is_not: "is not",
  contains: "contains",
  not_contains: "does not contain",
  starts_with: "starts with",
  gt: "is after",
  gte: "is on or after",
  lt: "is before",
  lte: "is on or before",
  is_set: "is set",
  is_not_set: "is not set",
  in: "is any of",
  not_in: "is none of",
};

export type FilterBarProps = {
  fields: FilterFieldDef[];
  conditions: FilterCondition[];
  onChange: (conditions: FilterCondition[]) => void;
  /** Rendered after the chips — usually the "Save as view" affordance. */
  trailing?: ReactNode;
  className?: string;
};

/**
 * The filter row shared by the inbox, tickets, customers, feedback, and the
 * audit log. Conditions serialise straight into `FilterGroup`, which is the
 * same shape saved views, automation rules, and SLA scoping all consume — so
 * "filter the list" and "write a rule that matches this" are the same mental
 * model and, importantly, the same code path.
 */
export function FilterBar({ fields, conditions, onChange, trailing, className }: FilterBarProps) {
  const addCondition = (field: FilterFieldDef) => {
    onChange([
      ...conditions,
      { field: field.key, operator: field.operators[0] ?? "is", value: null },
    ]);
  };

  const updateCondition = (index: number, patch: Partial<FilterCondition>) => {
    onChange(conditions.map((condition, i) => (i === index ? { ...condition, ...patch } : condition)));
  };

  const removeCondition = (index: number) => {
    onChange(conditions.filter((_, i) => i !== index));
  };

  return (
    <div className={cn("flex flex-wrap items-center gap-1.5", className)}>
      {conditions.map((condition, index) => {
        const field = fields.find((f) => f.key === condition.field);
        if (!field) return null;
        return (
          <FilterChip
            key={`${condition.field}-${index}`}
            field={field}
            condition={condition}
            onChange={(patch) => updateCondition(index, patch)}
            onRemove={() => removeCondition(index)}
          />
        );
      })}

      <AddFilterMenu fields={fields} onSelect={addCondition} hasFilters={conditions.length > 0} />

      {conditions.length > 0 && (
        <Button variant="ghost" size="sm" onClick={() => onChange([])} className="text-fg-muted">
          Clear all
        </Button>
      )}

      {trailing && <div className="ml-auto flex items-center gap-1.5">{trailing}</div>}
    </div>
  );
}

function AddFilterMenu({
  fields,
  onSelect,
  hasFilters,
}: {
  fields: FilterFieldDef[];
  onSelect: (field: FilterFieldDef) => void;
  hasFilters: boolean;
}) {
  const [query, setQuery] = useState("");
  const filtered = fields.filter((field) =>
    field.label.toLowerCase().includes(query.toLowerCase()),
  );
  const groups = [...new Set(filtered.map((field) => field.group ?? "Fields"))];

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          variant={hasFilters ? "ghost" : "secondary"}
          size="sm"
          leading={hasFilters ? <Plus /> : <ListFilter />}
        >
          {hasFilters ? "Add filter" : "Filter"}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-64 p-0">
        <div className="border-b border-line p-1.5">
          <SearchInput
            inputSize="sm"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onClear={() => setQuery("")}
            placeholder="Find a field…"
            autoFocus
          />
        </div>
        <div className="max-h-72 overflow-y-auto p-1">
          {groups.map((group) => (
            <div key={group}>
              <p className="px-2 pb-1 pt-2 text-2xs font-semibold uppercase tracking-caps text-fg-muted">
                {group}
              </p>
              {filtered
                .filter((field) => (field.group ?? "Fields") === group)
                .map((field) => (
                  <button
                    key={field.key}
                    type="button"
                    onClick={() => onSelect(field)}
                    className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm text-fg-secondary transition-colors hover:bg-fill hover:text-fg [&_svg]:size-3.5 [&_svg]:text-fg-muted"
                  >
                    {field.icon}
                    {field.label}
                  </button>
                ))}
            </div>
          ))}
          {filtered.length === 0 && (
            <p className="px-2 py-6 text-center text-xs text-fg-muted">No matching field</p>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}

function FilterChip({
  field,
  condition,
  onChange,
  onRemove,
}: {
  field: FilterFieldDef;
  condition: FilterCondition;
  onChange: (patch: Partial<FilterCondition>) => void;
  onRemove: () => void;
}) {
  const valueLabel = describeValue(field, condition.value);
  const needsValue = condition.operator !== "is_set" && condition.operator !== "is_not_set";

  return (
    <span className="inline-flex h-7 items-stretch overflow-hidden rounded-md border border-line bg-surface text-xs">
      <span className="flex items-center gap-1.5 border-r border-line px-2 text-fg-secondary [&_svg]:size-3 [&_svg]:text-fg-muted">
        {field.icon}
        {field.label}
      </span>

      <Menu>
        <MenuTrigger asChild>
          <button
            type="button"
            className="border-r border-line px-2 text-fg-muted transition-colors hover:bg-fill hover:text-fg"
          >
            {OPERATOR_LABEL[condition.operator]}
          </button>
        </MenuTrigger>
        <MenuContent>
          <MenuLabel>Operator</MenuLabel>
          {field.operators.map((operator) => (
            <MenuItem key={operator} onSelect={() => onChange({ operator })}>
              {OPERATOR_LABEL[operator]}
            </MenuItem>
          ))}
        </MenuContent>
      </Menu>

      {needsValue && (
        <Menu>
          <MenuTrigger asChild>
            <button
              type="button"
              className="flex max-w-44 items-center gap-1 px-2 font-medium text-fg transition-colors hover:bg-fill"
            >
              <span className="truncate">{valueLabel}</span>
              <ChevronDown className="size-3 shrink-0 text-fg-muted" />
            </button>
          </MenuTrigger>
          <MenuContent className="max-h-72">
            {field.options?.length ? (
              <>
                <MenuLabel>{field.label}</MenuLabel>
                {field.options.map((option) => (
                  <MenuItem
                    key={option.value}
                    icon={option.icon}
                    onSelect={() => onChange({ value: option.value })}
                  >
                    {option.label}
                  </MenuItem>
                ))}
                <MenuSeparator />
                <MenuItem onSelect={() => onChange({ value: null })}>Clear value</MenuItem>
              </>
            ) : (
              <div className="p-1.5">
                <SearchInput
                  inputSize="sm"
                  autoFocus
                  defaultValue={typeof condition.value === "string" ? condition.value : ""}
                  placeholder="Value…"
                  onKeyDown={(event) => {
                    if (event.key === "Enter") onChange({ value: event.currentTarget.value });
                  }}
                />
              </div>
            )}
          </MenuContent>
        </Menu>
      )}

      <button
        type="button"
        onClick={onRemove}
        aria-label={`Remove ${field.label} filter`}
        className="grid w-6 place-items-center border-l border-line text-fg-muted transition-colors hover:bg-danger-subtle hover:text-danger-text"
      >
        <X className="size-3" />
      </button>
    </span>
  );
}

function describeValue(field: FilterFieldDef, value: FieldValue): string {
  if (value == null || value === "") return "any";
  if (Array.isArray(value)) {
    if (value.length === 0) return "any";
    if (value.length === 1) return labelFor(field, value[0]!);
    return `${labelFor(field, value[0]!)} +${value.length - 1}`;
  }
  return labelFor(field, String(value));
}

function labelFor(field: FilterFieldDef, value: string): string {
  return field.options?.find((option) => option.value === value)?.label ?? value;
}
