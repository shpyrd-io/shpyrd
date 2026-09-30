"use client";

import * as React from "react";
import { cn } from "cn";
import { ChartArea, Table2 } from "lucide-react";
import {
  area,
  axis,
  edge,
  HEIGHT,
  inks,
  shade,
  stepOf,
  toneOf,
  WIDTH,
  type Point,
  type Scale,
  type Tone,
} from "../lib/chart";
import { metricValue, timeLabel } from "../lib/format";
import { Button } from "./button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "./table";

export type TimeSeries = {
  name: string;
  points: Point[];
  tone?: Tone;
};

// A line the chart draws across itself: what a series is measured against.
export type Reference = { value: number; label: string; tone?: Tone };

// Something that happened at a moment, drawn across every series: a release.
export type Marker = { time: number; label: string };

const never = () => () => {};

// Times are written in the language and the hours of who reads them, and
// those are known only in the browser: until then the labels are empty.
function useBrowser() {
  return React.useSyncExternalStore(
    never,
    () => true,
    () => false,
  );
}

// How something changed along the time. It is drawn with two colours, the
// ink of a line and its wash; the legend at its side is also what reads
// the values: it shows the last ones, and those under the pointer.
function TimeChart({
  className,
  title,
  description,
  series,
  unit = "",
  kind = "step",
  arrangement = "layered",
  palette = "series",
  references = [],
  markers = [],
  range = "1h",
  format,
  ...props
}: Omit<React.ComponentProps<"figure">, "title"> & {
  title: React.ReactNode;
  description?: React.ReactNode;
  series: TimeSeries[];
  // What the values are counted in: `ms`, `bytes`, `cores`, `rps`, `%`.
  unit?: string;
  // In `step` a value holds until the next one; a `line` goes straight
  // from one to the next.
  kind?: "step" | "line";
  // `layered` draws each series from the bottom, one over the other;
  // `stacked` puts each on top of the one before.
  arrangement?: "layered" | "stacked";
  // `shades` gives every series the ink of the first, each one stronger:
  // for series that are steps of the same thing, as percentiles are. The
  // highest goes first, at the back; the lowest last, in front.
  palette?: "series" | "shades";
  references?: Reference[];
  markers?: Marker[];
  // The stretch of time shown, for the labels of the time: `1h`, `24h`, `7d`.
  range?: string;
  format?: (value: number) => string;
}) {
  const browser = useBrowser();
  const [at, setAt] = React.useState<number | null>(null);
  const [table, setTable] = React.useState(false);
  const write = format ?? ((value: number) => metricValue(value, unit));

  const drawn = React.useMemo(() => {
    const times = [...new Set(series.flatMap((s) => s.points.map((p) => p[0])))].sort(
      (a, b) => a - b,
    );
    const step = stepOf(times);
    const from = times[0] ?? 0;
    const end = (times[times.length - 1] ?? 0) + (kind === "step" ? step : 0);

    // What is drawn of each series: in a stack, its values over those of
    // the series under it.
    const sums = new Map<number, number>();
    const tops = series.map((s) =>
      arrangement === "stacked"
        ? s.points.map(([time, value]): Point => {
            const sum = (sums.get(time) ?? 0) + value;
            sums.set(time, sum);
            return [time, sum];
          })
        : s.points,
    );
    const highest = Math.max(
      0,
      ...tops.flatMap((points) => points.map((p) => p[1])),
      ...references.map((r) => r.value),
    );
    const ticks = axis(highest, unit);
    const scale: Scale = { from, to: end, max: ticks[ticks.length - 1] };
    const values = series.map((s) => new Map(s.points));
    return { times, end, scale, ticks, tops, values };
  }, [series, references, arrangement, kind, unit]);

  const { times, end, scale, ticks, tops, values } = drawn;
  const empty = times.length === 0;
  const index = at ?? times.length - 1;
  const time = times[index];

  // The series are drawn in their order: the first at the back, the last
  // in front. In layers the last should be the one with the lowest values.
  const layers = series.map((s, i) => ({ s, i }));

  function move(event: React.PointerEvent<HTMLDivElement>) {
    const box = event.currentTarget.getBoundingClientRect();
    const where = scale.from + ((event.clientX - box.left) / box.width) * (scale.to - scale.from);
    let nearest = 0;
    for (let i = 0; i < times.length; i++) {
      if (times[i] <= where) nearest = i;
    }
    setAt(nearest);
  }

  function key(event: React.KeyboardEvent<HTMLDivElement>) {
    if (event.key === "ArrowLeft") setAt(Math.max(0, index - 1));
    else if (event.key === "ArrowRight") setAt(Math.min(times.length - 1, index + 1));
    else if (event.key === "Home") setAt(0);
    else if (event.key === "End") setAt(times.length - 1);
    else if (event.key === "Escape") setAt(null);
    else return;
    event.preventDefault();
  }

  const left = (t: number) => `${((t - scale.from) / (scale.to - scale.from || 1)) * 100}%`;
  const top = (v: number) => `${100 - (Math.min(v, scale.max) / scale.max) * 100}%`;
  const labels = empty
    ? []
    : Array.from({ length: 5 }, (_, i) => scale.from + ((scale.to - scale.from) * i) / 4);

  return (
    <figure
      data-slot="time-chart"
      className={cn("@container/chart grid gap-3", className)}
      {...props}
    >
      <figcaption className="flex items-start justify-between gap-3">
        <div className="grid gap-0.5">
          <div className="text-sm leading-snug font-medium">{title}</div>
          {description && (
            <div className="text-xs text-muted-foreground">{description}</div>
          )}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <span
            data-slot="time-chart-time"
            className="font-mono text-xs text-muted-foreground tabular-nums"
          >
            {!empty && browser && (at === null ? "latest" : timeLabel(time, range))}
          </span>
          <Button
            variant="ghost"
            size="icon-xs"
            aria-pressed={table}
            aria-label={table ? "Show the chart" : "Show the values as a table"}
            title={table ? "Show the chart" : "Show the values as a table"}
            icon={table ? <ChartArea /> : <Table2 />}
            onClick={() => setTable(!table)}
          />
        </div>
      </figcaption>

      {empty ? (
        <div className="flex h-44 items-center justify-center rounded-lg border border-dashed text-xs text-muted-foreground">
          Nothing was measured in this time
        </div>
      ) : table ? (
        <div className="max-h-64 overflow-auto rounded-lg border">
          <Table variant="secondary">
            <TableHeader>
              <TableRow>
                <TableHead>Time</TableHead>
                {series.map((s) => (
                  <TableHead key={s.name} className="text-right">
                    {s.name}
                  </TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {times.map((t) => (
                <TableRow key={t}>
                  <TableCell className="font-mono text-xs">
                    {browser && timeLabel(t, range)}
                  </TableCell>
                  {series.map((s, i) => (
                    <TableCell key={s.name} className="text-right font-mono text-xs">
                      {values[i].has(t) ? write(values[i].get(t) as number) : "—"}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : (
        <div className="grid gap-x-6 gap-y-3 @xl/chart:grid-cols-[minmax(0,1fr)_8rem]">
          <div className="grid grid-cols-[3.5rem_minmax(0,1fr)]">
            {/* The values of the axis, at the left of the drawing. */}
            <div className="relative h-44">
              {ticks.map((value) => (
                <span
                  key={value}
                  className="absolute right-2 -translate-y-1/2 font-mono text-[10px] whitespace-nowrap text-muted-foreground tabular-nums"
                  style={{ top: top(value) }}
                >
                  {write(value)}
                </span>
              ))}
            </div>

            <div
              data-slot="time-chart-plot"
              role="img"
              tabIndex={0}
              aria-label={`${typeof title === "string" ? title : "Chart"}: ${series
                .map((s) => s.name)
                .join(", ")}. The arrows move along the time.`}
              className="relative h-44 cursor-crosshair rounded-xs outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
              onPointerMove={move}
              onPointerLeave={() => setAt(null)}
              onKeyDown={key}
              onBlur={() => setAt(null)}
            >
              <svg
                viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
                preserveAspectRatio="none"
                className="absolute inset-0 size-full overflow-visible"
                aria-hidden="true"
              >
                {ticks.map((value) => (
                  <line
                    key={value}
                    x1={0}
                    x2={WIDTH}
                    y1={HEIGHT - (value / scale.max) * HEIGHT}
                    y2={HEIGHT - (value / scale.max) * HEIGHT}
                    vectorEffect="non-scaling-stroke"
                    className={cn("stroke-border", value === 0 && "stroke-foreground/25")}
                  />
                ))}
                {layers.map(({ s, i }) => {
                  const strength =
                    palette === "shades"
                      ? shade(i, series.length)
                      : { wash: arrangement === "stacked" ? 0.22 : 0.14, line: 1 };
                  return (
                    <g
                      key={s.name}
                      data-series={s.name}
                      className={inks[palette === "shades" ? toneOf(series[0].tone, 0) : toneOf(s.tone, i)]}
                    >
                      <path
                        d={area(
                          tops[i],
                          scale,
                          kind,
                          end,
                          arrangement === "stacked" && i > 0 ? tops[i - 1] : undefined,
                        )}
                        className="fill-(--ink)"
                        fillOpacity={strength.wash}
                      />
                      <path
                        d={edge(tops[i], scale, kind, end)}
                        fill="none"
                        vectorEffect="non-scaling-stroke"
                        strokeWidth={1.5}
                        strokeLinejoin="round"
                        strokeOpacity={strength.line}
                        className="stroke-(--ink)"
                      />
                    </g>
                  );
                })}
                {references.map((r) => (
                  <line
                    key={r.label}
                    x1={0}
                    x2={WIDTH}
                    y1={HEIGHT - (r.value / scale.max) * HEIGHT}
                    y2={HEIGHT - (r.value / scale.max) * HEIGHT}
                    vectorEffect="non-scaling-stroke"
                    strokeDasharray="4 3"
                    className={cn("stroke-(--ink)", inks[r.tone ?? "neutral"])}
                  />
                ))}
                {markers.map((m) => (
                  <line
                    key={m.label}
                    x1={((m.time - scale.from) / (scale.to - scale.from)) * WIDTH}
                    x2={((m.time - scale.from) / (scale.to - scale.from)) * WIDTH}
                    y1={0}
                    y2={HEIGHT}
                    vectorEffect="non-scaling-stroke"
                    className="stroke-foreground/40"
                  />
                ))}
              </svg>

              {references.map((r) => (
                <span
                  key={r.label}
                  className="absolute right-1 -translate-y-full rounded-xs bg-background/80 px-1 font-mono text-[10px] text-muted-foreground"
                  style={{ top: top(r.value) }}
                >
                  {r.label} {write(r.value)}
                </span>
              ))}

              {at !== null && (
                <div
                  data-slot="time-chart-crosshair"
                  className="pointer-events-none absolute inset-y-0 w-px bg-foreground/50"
                  style={{ left: left(time) }}
                >
                  {series.map((s, i) => {
                    const point = tops[i].find((p) => p[0] === time);
                    if (!point) return null;
                    return (
                      <span
                        key={s.name}
                        className={cn(
                          "absolute left-0 size-2 -translate-x-1/2 -translate-y-1/2 rounded-full bg-(--ink) ring-2 ring-background",
                          inks[palette === "shades" ? toneOf(series[0].tone, 0) : toneOf(s.tone, i)],
                        )}
                        style={{ top: top(point[1]) }}
                      />
                    );
                  })}
                </div>
              )}
            </div>

            {/* The times of the axis, and what happened at a moment. */}
            <div className="col-start-2 grid gap-1 pt-1.5">
              <div className="relative h-4">
                {labels.map((t, i) => (
                  <span
                    key={t}
                    className={cn(
                      "absolute font-mono text-[10px] whitespace-nowrap text-muted-foreground tabular-nums",
                      i === 0 ? "" : i === labels.length - 1 ? "-translate-x-full" : "-translate-x-1/2",
                    )}
                    style={{ left: left(t) }}
                  >
                    {browser && timeLabel(t, range)}
                  </span>
                ))}
              </div>
              {markers.length > 0 && (
                <div className="relative h-5">
                  {markers.map((m) => (
                    <span
                      key={m.label}
                      title={browser ? `${m.label}, ${timeLabel(m.time, range)}` : m.label}
                      className="absolute -translate-x-1/2 rounded-full border bg-muted px-1.5 font-mono text-[10px] leading-4 text-muted-foreground"
                      style={{ left: left(m.time) }}
                    >
                      {m.label}
                    </span>
                  ))}
                </div>
              )}
            </div>
          </div>

          {/* The legend, which also reads the values. */}
          <ul
            data-slot="time-chart-legend"
            className="flex flex-wrap gap-x-5 gap-y-2 pl-14 @xl/chart:flex-col @xl/chart:flex-nowrap @xl/chart:pl-0"
          >
            {series.map((s, i) => {
              const strength =
                palette === "shades" ? shade(i, series.length) : { wash: 0.22, line: 1 };
              const value = values[i].get(time);
              return (
                <li
                  key={s.name}
                  className={cn(
                    "flex items-stretch gap-2",
                    inks[palette === "shades" ? toneOf(series[0].tone, 0) : toneOf(s.tone, i)],
                  )}
                >
                  <span
                    aria-hidden="true"
                    className="relative w-2 shrink-0 overflow-hidden rounded-[3px] border border-(--ink)"
                    style={{ borderColor: `color-mix(in oklab, var(--ink) ${strength.line * 100}%, transparent)` }}
                  >
                    <span
                      className="absolute inset-0 bg-(--ink)"
                      style={{ opacity: Math.min(1, strength.wash * 2.2) }}
                    />
                  </span>
                  <span className="grid min-w-0">
                    <span className="truncate text-[11px] leading-4 text-muted-foreground">
                      {s.name}
                    </span>
                    <span className="font-mono text-sm leading-5 font-medium tabular-nums">
                      {value === undefined ? "—" : write(value)}
                    </span>
                  </span>
                </li>
              );
            })}
          </ul>
        </div>
      )}
    </figure>
  );
}

export { TimeChart };
