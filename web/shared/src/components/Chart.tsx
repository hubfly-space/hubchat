import { ArrowDown, ArrowRight, ArrowUp } from "lucide-react";
import { Fragment, useId, useMemo, useState, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { formatCompact, formatPercent } from "../lib/format";
import { Tooltip } from "./Tooltip";

/**
 * Charts are hand-drawn SVG rather than a charting library.
 *
 * Three reasons, in order of weight:
 *   1. every colour, grid line, and label comes from the token layer, so charts
 *      re-theme with the rest of the product and never introduce a stray hue;
 *   2. the widget and portal bundles stay small (§17 bundle budgets);
 *   3. §6.18 requires every metric to explain its own definition — that is a
 *      component-API concern, not something a generic library exposes.
 *
 * The trade is that these cover the shapes Hubchat actually reports on and
 * nothing more. Adding a new chart type is a deliberate act.
 */

export type ChartPoint = { t: string; v: number };

type Scale = {
  x: (index: number) => number;
  y: (value: number) => number;
  max: number;
  min: number;
};

function buildScale(
  values: number[],
  width: number,
  height: number,
  padding: { top: number; bottom: number },
  zeroBased = true,
): Scale {
  const rawMax = Math.max(...values, 0);
  const rawMin = zeroBased ? 0 : Math.min(...values, 0);
  // Round the ceiling to a friendly step so gridlines land on readable numbers.
  const max = niceCeiling(rawMax) || 1;
  const min = rawMin;
  const usable = height - padding.top - padding.bottom;
  const span = Math.max(values.length - 1, 1);

  return {
    x: (index) => (index / span) * width,
    y: (value) => padding.top + usable - ((value - min) / (max - min || 1)) * usable,
    max,
    min,
  };
}

function niceCeiling(value: number): number {
  if (value <= 0) return 0;
  const magnitude = Math.pow(10, Math.floor(Math.log10(value)));
  const normalized = value / magnitude;
  const step = normalized <= 1 ? 1 : normalized <= 2 ? 2 : normalized <= 5 ? 5 : 10;
  return step * magnitude;
}

function toPath(points: ChartPoint[], scale: Scale, smooth: boolean): string {
  if (points.length === 0) return "";
  const coords = points.map((point, index) => [scale.x(index), scale.y(point.v)] as const);

  if (!smooth || coords.length < 3) {
    return coords.map(([x, y], i) => `${i === 0 ? "M" : "L"}${x.toFixed(2)},${y.toFixed(2)}`).join(" ");
  }

  // Monotone-ish cubic: control points at the horizontal midpoint keep the
  // curve from overshooting below zero on spiky support-volume data.
  let path = `M${coords[0]![0].toFixed(2)},${coords[0]![1].toFixed(2)}`;
  for (let i = 1; i < coords.length; i++) {
    const [x0, y0] = coords[i - 1]!;
    const [x1, y1] = coords[i]!;
    const midX = (x0 + x1) / 2;
    path += ` C${midX.toFixed(2)},${y0.toFixed(2)} ${midX.toFixed(2)},${y1.toFixed(2)} ${x1.toFixed(2)},${y1.toFixed(2)}`;
  }
  return path;
}

/* -------------------------------------------------------------------------- */
/*  Sparkline                                                                  */
/* -------------------------------------------------------------------------- */

export function Sparkline({
  points,
  tone = 1,
  height = 28,
  width = 96,
  className,
}: {
  points: ChartPoint[];
  tone?: 1 | 2 | 3 | 4 | 5 | 6;
  height?: number;
  width?: number;
  className?: string;
}) {
  const gradientId = useId();
  const scale = buildScale(points.map((p) => p.v), width, height, { top: 2, bottom: 2 });
  const path = toPath(points, scale, true);
  const last = points[points.length - 1];

  return (
    <svg
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
      aria-hidden="true"
      className={cn("overflow-visible", className)}
      style={{ width, height }}
    >
      <defs>
        <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={`var(--hc-chart-${tone})`} stopOpacity="0.22" />
          <stop offset="100%" stopColor={`var(--hc-chart-${tone})`} stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={`${path} L${width},${height} L0,${height} Z`} fill={`url(#${gradientId})`} />
      <path
        d={path}
        fill="none"
        stroke={`var(--hc-chart-${tone})`}
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
      />
      {last && (
        <circle cx={width} cy={scale.y(last.v)} r="1.75" fill={`var(--hc-chart-${tone})`} />
      )}
    </svg>
  );
}

/* -------------------------------------------------------------------------- */
/*  Area / line chart                                                          */
/* -------------------------------------------------------------------------- */

export type Series = {
  key: string;
  label: string;
  points: ChartPoint[];
  tone?: 1 | 2 | 3 | 4 | 5 | 6;
  /** Renders as a dashed line without a fill — for targets and prior periods. */
  reference?: boolean;
};

export function AreaChart({
  series,
  height = 220,
  formatValue = (value: number) => formatCompact(value),
  formatLabel = (label: string) => label,
  showGrid = true,
  className,
}: {
  series: Series[];
  height?: number;
  formatValue?: (value: number) => string;
  formatLabel?: (label: string) => string;
  showGrid?: boolean;
  className?: string;
}) {
  const gradientId = useId();
  const [hover, setHover] = useState<number | null>(null);

  const width = 800; // viewBox space; the SVG scales to its container
  const padding = { top: 12, bottom: 24 };
  const allValues = series.flatMap((s) => s.points.map((p) => p.v));
  const scale = useMemo(
    () => buildScale(allValues, width, height, padding),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [allValues.join(","), height],
  );

  const primary = series[0];
  const pointCount = primary?.points.length ?? 0;
  const gridLines = [0, 0.25, 0.5, 0.75, 1];

  return (
    <div className={cn("relative", className)}>
      <svg
        viewBox={`0 0 ${width} ${height}`}
        preserveAspectRatio="none"
        role="img"
        aria-label={series.map((s) => s.label).join(", ")}
        className="w-full"
        style={{ height }}
        onMouseLeave={() => setHover(null)}
        onMouseMove={(event) => {
          const rect = event.currentTarget.getBoundingClientRect();
          const ratio = (event.clientX - rect.left) / rect.width;
          setHover(Math.round(ratio * Math.max(pointCount - 1, 0)));
        }}
      >
        <defs>
          {series.map((s) => (
            <linearGradient key={s.key} id={`${gradientId}-${s.key}`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={`var(--hc-chart-${s.tone ?? 1})`} stopOpacity="0.26" />
              <stop offset="100%" stopColor={`var(--hc-chart-${s.tone ?? 1})`} stopOpacity="0" />
            </linearGradient>
          ))}
        </defs>

        {showGrid &&
          gridLines.map((ratio) => {
            const y = padding.top + (height - padding.top - padding.bottom) * ratio;
            return (
              <line
                key={ratio}
                x1="0"
                x2={width}
                y1={y}
                y2={y}
                stroke="var(--hc-chart-grid)"
                strokeWidth="1"
                vectorEffect="non-scaling-stroke"
                strokeDasharray={ratio === 1 ? undefined : "2 4"}
              />
            );
          })}

        {series.map((s) => {
          const path = toPath(s.points, scale, true);
          return (
            <g key={s.key}>
              {!s.reference && (
                <path
                  d={`${path} L${width},${height - padding.bottom} L0,${height - padding.bottom} Z`}
                  fill={`url(#${gradientId}-${s.key})`}
                />
              )}
              <path
                d={path}
                fill="none"
                stroke={`var(--hc-chart-${s.tone ?? 1})`}
                strokeWidth="1.75"
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeDasharray={s.reference ? "4 4" : undefined}
                strokeOpacity={s.reference ? 0.6 : 1}
                vectorEffect="non-scaling-stroke"
              />
            </g>
          );
        })}

        {hover != null && primary?.points[hover] && (
          <g>
            <line
              x1={scale.x(hover)}
              x2={scale.x(hover)}
              y1={padding.top}
              y2={height - padding.bottom}
              stroke="var(--hc-border-loud)"
              strokeWidth="1"
              vectorEffect="non-scaling-stroke"
            />
            {series.map((s) => {
              const point = s.points[hover];
              if (!point) return null;
              return (
                <circle
                  key={s.key}
                  cx={scale.x(hover)}
                  cy={scale.y(point.v)}
                  r="3"
                  fill="var(--hc-surface)"
                  stroke={`var(--hc-chart-${s.tone ?? 1})`}
                  strokeWidth="2"
                  vectorEffect="non-scaling-stroke"
                />
              );
            })}
          </g>
        )}
      </svg>

      {/* Axis labels live in HTML, not SVG — the chart stretches horizontally and
          `preserveAspectRatio: none` would distort SVG text. */}
      <div className="pointer-events-none absolute inset-y-0 -left-1 flex flex-col justify-between py-0 text-2xs tabular text-fg-muted">
        <span>{formatValue(scale.max)}</span>
        <span>{formatValue(scale.max / 2)}</span>
        <span className="pb-6">{formatValue(scale.min)}</span>
      </div>

      <div className="mt-1 flex justify-between text-2xs text-fg-muted">
        {primary && pointCount > 0 && (
          <>
            <span>{formatLabel(primary.points[0]!.t)}</span>
            {pointCount > 2 && (
              <span>{formatLabel(primary.points[Math.floor(pointCount / 2)]!.t)}</span>
            )}
            <span>{formatLabel(primary.points[pointCount - 1]!.t)}</span>
          </>
        )}
      </div>

      {hover != null && primary?.points[hover] && (
        <div
          className={cn(
            "pointer-events-none absolute top-2 z-10 -translate-x-1/2 whitespace-nowrap",
            "rounded-md border border-line-strong bg-overlay px-2 py-1.5 shadow-3",
          )}
          style={{ left: `${(hover / Math.max(pointCount - 1, 1)) * 100}%` }}
        >
          <p className="text-2xs text-fg-muted">{formatLabel(primary.points[hover]!.t)}</p>
          {series.map((s) => (
            <p key={s.key} className="mt-0.5 flex items-center gap-1.5 text-xs text-fg">
              <span
                aria-hidden="true"
                className="size-1.5 rounded-full"
                style={{ backgroundColor: `var(--hc-chart-${s.tone ?? 1})` }}
              />
              {s.label}
              <span className="ml-auto font-medium tabular">
                {formatValue(s.points[hover]?.v ?? 0)}
              </span>
            </p>
          ))}
        </div>
      )}
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/*  Bar chart                                                                  */
/* -------------------------------------------------------------------------- */

export function BarChart({
  points,
  tone = 1,
  height = 180,
  horizontal = false,
  formatValue = (value: number) => formatCompact(value),
  className,
}: {
  points: (ChartPoint & { tone?: 1 | 2 | 3 | 4 | 5 | 6 })[];
  tone?: 1 | 2 | 3 | 4 | 5 | 6;
  height?: number;
  /** Horizontal reads better once labels are longer than ~6 characters. */
  horizontal?: boolean;
  formatValue?: (value: number) => string;
  className?: string;
}) {
  const max = niceCeiling(Math.max(...points.map((p) => p.v), 0)) || 1;

  if (horizontal) {
    return (
      <div className={cn("flex flex-col gap-2", className)}>
        {points.map((point) => (
          <div key={point.t} className="grid grid-cols-[minmax(0,120px)_1fr_auto] items-center gap-3">
            <span className="truncate text-xs text-fg-secondary" title={point.t}>
              {point.t}
            </span>
            <div className="h-2 overflow-hidden rounded-full bg-chart-track">
              <div
                className="h-full rounded-full transition-[width] duration-slow ease-out"
                style={{
                  width: `${(point.v / max) * 100}%`,
                  backgroundColor: `var(--hc-chart-${point.tone ?? tone})`,
                }}
              />
            </div>
            <span className="w-10 text-right text-xs tabular text-fg-secondary">
              {formatValue(point.v)}
            </span>
          </div>
        ))}
      </div>
    );
  }

  return (
    <div className={cn("flex items-end gap-1", className)} style={{ height }}>
      {points.map((point) => (
        <Tooltip key={point.t} content={`${point.t}: ${formatValue(point.v)}`}>
          <div className="flex h-full flex-1 flex-col justify-end gap-1.5">
            <div
              className="w-full rounded-t-xs transition-[height] duration-slow ease-out hover:brightness-125"
              style={{
                height: `${Math.max((point.v / max) * 100, 1.5)}%`,
                backgroundColor: `var(--hc-chart-${point.tone ?? tone})`,
              }}
            />
            <span className="truncate text-center text-2xs text-fg-muted">{point.t}</span>
          </div>
        </Tooltip>
      ))}
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/*  Donut                                                                      */
/* -------------------------------------------------------------------------- */

export function DonutChart({
  segments,
  size = 132,
  thickness = 14,
  centerLabel,
  centerValue,
  className,
}: {
  segments: { key: string; label: string; value: number; tone: 1 | 2 | 3 | 4 | 5 | 6 }[];
  size?: number;
  thickness?: number;
  centerLabel?: string;
  centerValue?: ReactNode;
  className?: string;
}) {
  const total = segments.reduce((sum, segment) => sum + segment.value, 0) || 1;
  const radius = (size - thickness) / 2;
  const circumference = 2 * Math.PI * radius;
  let offset = 0;

  return (
    <div className={cn("flex items-center gap-5", className)}>
      <div className="relative shrink-0" style={{ width: size, height: size }}>
        <svg viewBox={`0 0 ${size} ${size}`} className="-rotate-90" style={{ width: size, height: size }}>
          <circle
            cx={size / 2}
            cy={size / 2}
            r={radius}
            fill="none"
            stroke="var(--hc-chart-track)"
            strokeWidth={thickness}
          />
          {segments.map((segment) => {
            const length = (segment.value / total) * circumference;
            const dash = `${length} ${circumference - length}`;
            const element = (
              <circle
                key={segment.key}
                cx={size / 2}
                cy={size / 2}
                r={radius}
                fill="none"
                stroke={`var(--hc-chart-${segment.tone})`}
                strokeWidth={thickness}
                strokeDasharray={dash}
                strokeDashoffset={-offset}
                // 2px visual gap between adjacent segments.
                strokeLinecap="butt"
              />
            );
            offset += length;
            return element;
          })}
        </svg>

        {(centerValue || centerLabel) && (
          <div className="absolute inset-0 grid place-content-center text-center">
            {centerValue && (
              <span className="block text-xl font-semibold tabular tracking-tight text-fg">
                {centerValue}
              </span>
            )}
            {centerLabel && <span className="mt-0.5 block text-2xs text-fg-muted">{centerLabel}</span>}
          </div>
        )}
      </div>

      <ul className="min-w-0 flex-1 space-y-1.5">
        {segments.map((segment) => (
          <li key={segment.key} className="flex items-center gap-2 text-xs">
            <span
              aria-hidden="true"
              className="size-2 shrink-0 rounded-[2px]"
              style={{ backgroundColor: `var(--hc-chart-${segment.tone})` }}
            />
            <span className="min-w-0 flex-1 truncate text-fg-secondary">{segment.label}</span>
            <span className="shrink-0 tabular text-fg-muted">
              {formatPercent(segment.value / total)}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/*  Heat map                                                                   */
/* -------------------------------------------------------------------------- */

/** Volume by weekday × hour — where staffing decisions actually get made. */
export function HeatMap({
  data,
  rowLabels,
  columnLabels,
  formatValue = (value: number) => formatCompact(value),
  className,
}: {
  /** data[row][column] */
  data: number[][];
  rowLabels: string[];
  columnLabels: string[];
  formatValue?: (value: number) => string;
  className?: string;
}) {
  const max = Math.max(...data.flat(), 1);

  return (
    <div className={cn("overflow-x-auto", className)}>
      <div className="inline-grid gap-px" style={{ gridTemplateColumns: `auto repeat(${columnLabels.length}, minmax(14px, 1fr))` }}>
        <span />
        {columnLabels.map((label, index) => (
          <span
            key={label}
            className={cn(
              "pb-1 text-center text-2xs text-fg-muted",
              index % 2 === 1 && "opacity-0",
            )}
          >
            {label}
          </span>
        ))}

        {data.map((row, rowIndex) => (
          <Fragment key={rowLabels[rowIndex] ?? rowIndex}>
            <span className="pr-2 text-right text-2xs leading-4 text-fg-muted">
              {rowLabels[rowIndex]}
            </span>
            {row.map((value, columnIndex) => (
              <Tooltip
                key={columnIndex}
                content={`${rowLabels[rowIndex]} ${columnLabels[columnIndex]} · ${formatValue(value)}`}
              >
                <div
                  className="h-4 rounded-[2px] transition-transform hover:scale-110"
                  style={{
                    backgroundColor:
                      value === 0
                        ? "var(--hc-chart-track)"
                        : `color-mix(in oklab, var(--hc-chart-1) ${12 + (value / max) * 88}%, transparent)`,
                  }}
                />
              </Tooltip>
            ))}
          </Fragment>
        ))}
      </div>
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/*  Metric                                                                     */
/* -------------------------------------------------------------------------- */

export function Metric({
  label,
  value,
  delta,
  higherIsBetter = true,
  definition,
  sparkline,
  suffix,
  className,
}: {
  label: string;
  value: ReactNode;
  /** Ratio change against the preceding window. null hides the indicator. */
  delta?: number | null;
  higherIsBetter?: boolean;
  /** §6.18 — every metric must be able to explain itself. */
  definition?: string;
  sparkline?: ChartPoint[];
  suffix?: ReactNode;
  className?: string;
}) {
  const direction = delta == null || Math.abs(delta) < 0.005 ? "flat" : delta > 0 ? "up" : "down";
  const good = direction === "flat" ? null : (direction === "up") === higherIsBetter;
  const Icon = direction === "up" ? ArrowUp : direction === "down" ? ArrowDown : ArrowRight;

  return (
    <div className={cn("min-w-0", className)}>
      <div className="flex items-center gap-1.5">
        <p className="truncate text-xs text-fg-muted">{label}</p>
        {definition && (
          <Tooltip content={definition}>
            <span
              tabIndex={0}
              role="note"
              aria-label={`${label} definition`}
              className="grid size-3.5 shrink-0 place-items-center rounded-full border border-line text-[8px] font-semibold text-fg-muted transition-colors hover:border-line-strong hover:text-fg-secondary"
            >
              ?
            </span>
          </Tooltip>
        )}
      </div>

      <div className="mt-1 flex items-end justify-between gap-3">
        <div className="flex items-baseline gap-1.5">
          <span className="text-2xl font-semibold tabular tracking-tighter text-fg">{value}</span>
          {suffix && <span className="text-xs text-fg-muted">{suffix}</span>}
        </div>
        {sparkline && sparkline.length > 1 && <Sparkline points={sparkline} className="mb-1" />}
      </div>

      {delta != null && (
        <p
          className={cn(
            "mt-1.5 flex items-center gap-1 text-xs tabular",
            good === null && "text-fg-muted",
            good === true && "text-success-text",
            good === false && "text-danger-text",
          )}
        >
          <Icon aria-hidden="true" className="size-3" />
          {formatPercent(Math.abs(delta), 1)}
          <span className="text-fg-muted">vs previous period</span>
        </p>
      )}
    </div>
  );
}
