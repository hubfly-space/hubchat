import { Check } from "lucide-react";
import { cn } from "../lib/cn";
import { Input } from "./Input";
import { Popover, PopoverContent, PopoverTrigger } from "./Popover";

/**
 * Brand colours for widgets and portals (§6.4, §6.5).
 *
 * The presets are deliberately a *spread of hues* rather than the product
 * palette — a tenant's brand is theirs, and the widget re-tones through
 * `[data-branded]` without touching the dashboard's own accent. The free-form
 * hex input exists because the presets will never be enough.
 */
const PRESETS = [
  "#3B6EF6", // Hubchat cobalt — the default
  "#2A57D8",
  "#0F6E68",
  "#3E9C6D",
  "#C08A3E",
  "#D05A55",
  "#7C66F0",
  "#B8478F",
  "#E06C2B",
  "#1F2937",
];

export type ColorPickerProps = {
  value: string;
  onChange: (value: string) => void;
  label?: string;
  className?: string;
};

export function ColorPicker({ value, onChange, label, className }: ColorPickerProps) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label={label ?? "Choose colour"}
          className={cn(
            "flex h-control-md items-center gap-2 rounded-md border border-line bg-inset pl-1.5 pr-2.5",
            "transition-colors hover:border-line-strong",
            className,
          )}
        >
          <span
            aria-hidden="true"
            className="size-5 shrink-0 rounded-sm border border-line-strong"
            style={{ backgroundColor: value }}
          />
          <span className="font-mono text-xs uppercase text-fg-secondary">{value}</span>
        </button>
      </PopoverTrigger>

      <PopoverContent className="w-56 p-3">
        <p className="mb-2 text-2xs font-semibold uppercase tracking-caps text-fg-muted">Presets</p>
        <div className="grid grid-cols-5 gap-1.5">
          {PRESETS.map((preset) => (
            <button
              key={preset}
              type="button"
              onClick={() => onChange(preset)}
              aria-label={preset}
              className={cn(
                "grid aspect-square place-items-center rounded-md border transition-transform hover:scale-105",
                value.toLowerCase() === preset.toLowerCase()
                  ? "border-line-loud"
                  : "border-line-subtle",
              )}
              style={{ backgroundColor: preset }}
            >
              {value.toLowerCase() === preset.toLowerCase() && (
                <Check className="size-3 text-white drop-shadow" strokeWidth={3} />
              )}
            </button>
          ))}
        </div>

        <p className="mb-1.5 mt-3 text-2xs font-semibold uppercase tracking-caps text-fg-muted">
          Custom
        </p>
        <div className="flex items-center gap-1.5">
          <input
            type="color"
            value={value}
            onChange={(event) => onChange(event.target.value)}
            aria-label="Colour picker"
            className="size-8 shrink-0 cursor-pointer rounded-md border border-line bg-transparent"
          />
          <Input
            inputSize="sm"
            mono
            value={value}
            onChange={(event) => onChange(event.target.value)}
            aria-label="Hex value"
          />
        </div>
      </PopoverContent>
    </Popover>
  );
}
