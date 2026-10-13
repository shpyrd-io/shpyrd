"use client";
import {
  useEffect,
  useId,
  useRef,
  useState,
  type CSSProperties,
  type SVGProps,
} from "react";
import { axis } from "../lib/chart";
import { ChartArea, Table2 } from "lucide-react";
import { cn } from "../lib/cn";
import { Button } from "./button";
import {
  chartIssue,
  compact,
  decimalSum,
  decimalCompare,
  seriesColor,
  type ChartOptions,
  type PlotData,
  type PlotPoint,
  type PlotSeries,
} from "../lib/metric-chart";
import {observationLabel} from '../lib/metric-chart';
import { Table as DataTable, TableHeader, TableHead, TableRow, TableBody, TableCell } from "./table";
function Table({heads, rows}: {heads: string[]; rows: React.ReactNode[][]}) {
  return <DataTable variant="secondary"><TableHeader><TableRow>{heads.map(h => <TableHead key={h} className="not-first:text-right">{h}</TableHead>)}</TableRow></TableHeader><TableBody>{rows.map((row, i) => <TableRow key={i}>{row.map((cell, j) => <TableCell key={j} className="font-mono text-xs not-first:text-right">{cell}</TableCell>)}</TableRow>)}</TableBody></DataTable>;
}
import { MetricBullet, MetricDumbbell, MetricFunnel, MetricGauge, MetricState } from "./metric-special-charts";
const W = 860,
  H = 300,
  L = 76,
  R = 828,
  T = 22,
  B = 246;
const dateLabel = (key: string, zone: string, short = false) =>
  /^\d{4}-\d{2}$/.test(key)
    ? key
    : Number.isFinite(Date.parse(key))
      ? new Intl.DateTimeFormat("en", {
          timeZone: zone,
          month: "short",
          day: "numeric",
          ...(!short
            ? { hour: "2-digit" as const, minute: "2-digit" as const }
            : {}),
        }).format(new Date(key))
      : key;
const rowName = (r: {label: string; dimensions: Record<string,string>}) => (Object.values(r.dimensions).join(" / ") || r.label).replace(/^\d+ · /, "");
const positive = (value: string | null) => value !== null && Number(value) > 0;
export function MetricChart({
  data,
  options,
  onDrill,
  description,
  className,
}: {
  data: PlotData;
  options: ChartOptions;
  description?: React.ReactNode;
  className?: string;
  onDrill?: (point: PlotPoint, series: PlotSeries) => void;
}) {
  const [hidden, setHidden] = useState<string[]>([]),
    [table, setTable] = useState(false),
    [focus, setFocus] = useState<number | null>(null);
  // Assign tones before hiding series so their colours remain stable.
  const ids = [...new Set([...data.series.map(s => s.id), ...data.rows.map(r => r.key)])];
  const dimension = data.dimensionOrder?.[0] ?? Object.keys(data.rows[0]?.dimensions ?? {})[0];
  const parents = [...new Set(data.rows.map(r => r.dimensions[dimension]).filter(Boolean))];
  const color = (key: string) => {
    if (key === "other") return "var(--chart-5)";
    if (["sunburst", "sankey"].includes(options.kind)) {
      const parent = data.rows.find(r=>r.key===key)?.dimensions[dimension] ?? key;
      if (parents.includes(parent)) return `var(--chart-${Math.min(parents.indexOf(parent)+1,5)})`;
    }
    return ids.includes(key) ? `var(--chart-${Math.min(ids.indexOf(key) + 1, 5)})` : seriesColor(key);
  };
  const id = useId();
  const issue = chartIssue(data, options),
    showTable = table || options.kind === "table";
  const series = data.series.filter((s) => !hidden.includes(s.id));
  const keys = [
    ...new Set(data.series.flatMap((s) => s.points.map((p) => p.key))),
  ].sort();
  const interactive = [
    "line",
    "step",
    "bar",
    "area",
    "stacked_bar",
    "stacked_area",
    "bump",
  ].includes(options.kind);
  const frame = (contents: React.ReactNode | ((unit: number) => React.ReactNode), viewBox = `0 0 ${W} ${H}`) => (
    <MetricPlot
      viewBox={viewBox}
      role="img"
      aria-labelledby={id}
    >
      {unit => <><title id={id}>{`${data.title}. ${data.unit}. Exact values are available in the table.`}</title>
      {typeof contents === "function" ? contents(unit) : contents}</>}
    </MetricPlot>
  );
  const timeTable = interactive || ["small_multiples", "calendar", "state", "scatter", "bubble"].includes(options.kind);
  const hasPrevious = data.rows.some(row => row.previous !== undefined);
  const dataTable = (
    <div className="max-h-64 overflow-auto rounded-lg border">
      <Table
        heads={timeTable ? ["Time", ...data.series.map(s => s.name)] : ["Category", "Value", ...(hasPrevious ? ["Previous"] : [])]}
        rows={timeTable
          ? keys.map(key => [dateLabel(key, data.timezone), ...data.series.map(s => s.points.find(p => p.key === key)?.value ?? "—")])
          : data.rows.map(row => [row.label, row.value ?? "—", ...(hasPrevious ? [row.previous ?? "—"] : [])])}
      />
    </div>
  );
  let plot: React.ReactNode = null;
  if (!issue && !showTable) {
    if (options.kind === "small_multiples")
      plot = (
        <div className="metric-small-multiples">
          {series.map((s) => (
            <div key={s.id}>
              <p>
                {s.name} · {s.unit}
              </p>
              <Cartesian
                data={{ ...data, series: [s] }}
                kind="line"
                color={color}
              />
            </div>
          ))}
        </div>
      );
    else if (
      [
        "line",
        "step",
        "bar",
        "area",
        "stacked_bar",
        "stacked_area",
        "bump",
      ].includes(options.kind)
    )
      plot = (
        <Cartesian
          data={{ ...data, series }}
          kind={options.kind}
          color={color}
          target={options.target}
          focus={focus ?? undefined}
          onFocus={setFocus}
          allKeys={keys}
        />
      );
    else if (options.kind === "dumbbell")
      plot = <MetricDumbbell data={data} color={color} />;
    else if (options.kind === "funnel")
      plot = <MetricFunnel data={data} orientation={options.orientation} color={color} />;
    else if (
      ["ranking", "slope", "waterfall"].includes(
        options.kind,
      )
    ) {
      const rows = (
        options.kind === "ranking"
          ? [...data.rows].sort((a, b) =>
              a.value === null
                ? 1
                : b.value === null
                  ? -1
                  : decimalCompare(b.value, a.value),
            )
          : data.rows
      ).slice(0, 50);
      const max = Math.max(
        1,
        ...rows.map((r) => Math.abs(Number(r.value ?? 0))),
        ...rows.map((r) => Math.abs(Number(r.previous ?? 0))),
      );
      if (options.kind === "slope")
        plot = frame(
          rows.map((r) => {
            const low = Math.min(
                ...rows.map((r) => Number(r.value ?? 0)),
                ...rows.map((r) => Number(r.previous ?? 0)),
              ),
              y = (v: number) => B - ((v - low + (max-low)*.12) / ((max - low)*1.24 || 1)) * (B - T),
              stroke = color(r.key);
            return r.value !== null &&
              r.previous !== null &&
              r.previous !== undefined ? (
              <g key={r.key} stroke={stroke}>
                <title>{["", r.label, ": ", r.previous, "→ ", r.value, "", data.unit, ""].join("").trim()}</title>
                <line
                  x1={220}
                  x2={620}
                  y1={y(Number(r.previous))}
                  y2={y(Number(r.value))}
                />
                <circle cx={220} cy={y(Number(r.previous))} r={4} fill="var(--background)" />
                <circle cx={620} cy={y(Number(r.value))} r={4} fill={stroke} />
                <text x={206} y={y(Number(r.previous)) - 8} textAnchor="end" stroke="none">{rowName(r)}<tspan x={206} dy={25}>{compact(r.previous)}</tspan></text>
                <text x={634} y={y(Number(r.value)) - 8} stroke="none">{rowName(r)}<tspan x={634} dy={25}>{compact(r.value)}</tspan></text>
              </g>
            ) : null;
          }),
        );
      else if (options.kind === "waterfall") {
        let sum = 0;
        const steps = rows.map((r) => {
            const from = sum;
            sum += Number(r.value ?? 0);
            return { r, from, to: sum };
          }),
          low = Math.min(0, ...steps.map((s) => s.to)),
          high = Math.max(1, ...steps.map((s) => s.to)),
          w = (R - L) / (steps.length + 1);
        plot = frame(unit => {
          const base = 340 - 46 * unit;
          const y = (v: number) => base - ((v - low) / (high - low)) * (base - T - 20 * unit);
          const labelLines = (name: string) => {
            const limit = Math.max(4, Math.floor((w - 8 * unit) / (6.5 * unit))), words = name.split(" ");
            let first = words.shift() ?? "";
            while (words.length && first.length + words[0].length + 1 <= limit) first += " " + words.shift();
            return [first, words.join(" ")].filter(Boolean).map(line => line.length > limit ? line.slice(0, limit - 1) + "…" : line);
          };
          return <>
            <line x1={L} x2={R} y1={y(0)} y2={y(0)} stroke="var(--border)" />
            {steps.map((s, i) => (
              <g key={s.r.key}>
                <title>{["", s.r.label, ": ", s.r.value, "", data.unit, ""].join("").trim()}</title>
                <rect
                  x={L + i * w + 4}
                  y={Math.min(y(s.from), y(s.to))}
                  width={w - 8}
                  height={Math.max(1, Math.abs(y(s.to) - y(s.from)))}
                  fill={
                    Number(s.r.value) >= 0 ? "var(--chart-3)" : "var(--chart-1)"
                  }
                  fillOpacity={0.25} stroke={Number(s.r.value) >= 0 ? "var(--chart-3)" : "var(--chart-1)"} vectorEffect="non-scaling-stroke" rx={2}
                />
                <line x1={L + (i + 1) * w - 4} x2={L + (i + 1) * w + 4} y1={y(s.to)} y2={y(s.to)} stroke="var(--muted-foreground)" strokeDasharray="2 2" />
                <text x={L + i * w + w / 2} y={base + 12 * unit} dominantBaseline="hanging" textAnchor="middle">
                  {labelLines(rowName(s.r)).map((line, n) => <tspan key={n} x={L + i * w + w / 2} dy={n ? 16 * unit : 0}>{line}</tspan>)}
                </text>
                <text
                  x={L + i * w + w / 2}
                  y={Math.min(y(s.from), y(s.to)) - 8 * unit}
                  textAnchor="middle"
                >
                  {compact(s.r.value)}
                </text>
              </g>
            ))}
            <rect
              x={L + steps.length * w + 4}
              y={Math.min(y(0), y(sum))}
              width={w - 8}
              height={Math.abs(y(sum) - y(0))}
              fill="var(--chart-2)" fillOpacity={0.25} stroke="var(--chart-2)" vectorEffect="non-scaling-stroke" rx={2}
            />
            <text
              x={L + steps.length * w + w / 2}
              y={base + 12 * unit} dominantBaseline="hanging"
              textAnchor="middle"
            >
              Total
            </text>
          </>;
        }, "0 0 860 340");
      } else
        plot = frame(
          rows.map((r, i) => {
            const yy = 30 + i * 35,
              n = Number(r.value ?? 0),
              prev = r.previous == null ? null : Number(r.previous),
              negative = rows.some(
                (r) => Number(r.value) < 0 || Number(r.previous ?? 0) < 0,
              ),
              start = negative ? 520 : 280,
              x = (v: number) => start + (v / max) * (negative ? 240 : 480);
            return (
              <g key={r.key}>
                <title>{["", r.label, ": ", r.value ?? "Unavailable", "", data.unit, "", prev !== null ? ` · Previous ${r.previous}` : "", ""].join("").trim()}</title>
                <text x={12} y={yy + 4}>
                  {rowName(r).slice(0, 20)}
                </text>
                {r.value !== null && <rect x={Math.min(start, x(n))} y={yy - 10}
                  width={(Math.abs(n) / max) * (negative ? 240 : 480)} height={20} rx={2}
                  fill={color(r.key)} fillOpacity={0.24} stroke={color(r.key)} />}
                <text x={805} y={yy + 4} textAnchor="end">
                  {compact(r.value)}
                </text>
              </g>
            );
          }),
          `0 0 860 ${Math.max(100, rows.length * 35 + 25)}`,
        );
    } else if (
      ["pie", "donut", "treemap", "sunburst", "pareto"].includes(options.kind)
    ) {
      const all = data.rows
          .filter((r) => positive(r.value))
          .sort((a, b) => Number(b.value) - Number(a.value)),
        top = all.slice(0, 5),
        rest = all.slice(5);
      const rows = rest.length
          ? [
              ...top,
              {
                key: "other",
                label: "Other",
                dimensions: {},
                value: decimalSum(rest.map((r) => r.value!)),
              },
            ]
          : top,
        total = rows.reduce((v, r) => v + Number(r.value), 0);
      if (options.kind === "treemap")
        plot = frame(unit =>
          treeRects(
            rows.map((r, i) => ({ i, value: Number(r.value) })),
            20,
            20,
            820,
            240,
          ).map((b) => {
            const r = rows[b.i], inset = 1.5 * unit, padding = 4.5 * unit;
            const room = Math.max(0, b.w - 2 * inset - 2 * padding), chars = Math.floor(room / (7 * unit));
            const name = rowName(r), label = name.length > chars ? name.slice(0, Math.max(0, chars - 1)) + "…" : name;
            return (
              <g key={r.key}>
                <title>{["", r.label, ": ", r.value, "( ", ((Number(r.value) / total) * 100).toFixed(1), "%) "].join("").trim()}</title>
                <rect
                  x={b.x + inset}
                  y={b.y + inset}
                  width={Math.max(0, b.w - 2 * inset)}
                  height={Math.max(0, b.h - 2 * inset)}
                  fill={color(r.key)}
                  fillOpacity={0.18}
                  stroke={color(r.key)} strokeOpacity={0.6} vectorEffect="non-scaling-stroke" rx={3}
                />
                {chars >= 3 && b.h > 2 * inset + 2 * padding + 12 * unit && (
                  <text x={b.x + inset + padding} y={b.y + inset + padding} dominantBaseline="hanging" style={{fontSize:12 * unit}}>
                    {label}
                  </text>
                )}
              </g>
            );
          }),
        );
      else if (options.kind === "pareto") {
        let cumulative = 0;
        plot = frame(
          <>
            {all.slice(0, 50).map((r, i) => {
              const before = cumulative;
              cumulative += Number(r.value) / total;
              const x = L + ((i + 0.5) * (R - L)) / all.length,
                y = B - cumulative * (B - T),
                w = (R - L) / all.length;
              return (
                <g key={r.key}>
                  <title>{["", r.label, ": ", r.value, "; cumulative", " ", "", (cumulative * 100).toFixed(1), "% "].join("").trim()}</title>
                  <rect
                    x={x - w * 0.35}
                    y={B - (Number(r.value) / Number(all[0].value)) * (B - T)}
                    width={w * 0.7}
                    height={(Number(r.value) / Number(all[0].value)) * (B - T)}
                    fill={color(r.key)}
                    fillOpacity={0.25} stroke={color(r.key)} vectorEffect="non-scaling-stroke" rx={2}
                  />
                  {i > 0 && (
                    <line
                      x1={x - w}
                      x2={x}
                      y1={B - before * (B - T)}
                      y2={y}
                      stroke="var(--chart-4)" strokeWidth={1.5} vectorEffect="non-scaling-stroke"
                    />
                  )}
                  <circle cx={x} cy={y} r={3} fill="color-mix(in oklch, var(--chart-4) 25%, var(--background))" stroke="var(--chart-4)" vectorEffect="non-scaling-stroke" />
                </g>
              );
            })}
            <text x={R} y={T}>
              100%
            </text>
            <text x={R} y={B}>
              0%
            </text>
          </>,
        );
      } else {
        const polar = (a: number, r: number) => [
          260 + Math.cos(a) * r,
          140 + Math.sin(a) * r,
        ];
        const wedge = (
          from: number,
          to: number,
          outer: number,
          inner: number,
          inset = 0,
        ) => {
          // Inset both radial edges; with a 1px outline, 1.5px per side leaves a 2px gutter.
          // Clamp for very small slices so their edges cannot cross.
          inset = to - from >= Math.PI * 2 - 0.000001 ? 0 : Math.min(inset, (inner || outer) * Math.sin(Math.min((to - from) * .45, Math.PI / 2)));
          const offset = (radius: number) => Math.asin(Math.min(1, inset / radius));
          const out = inset ? offset(outer) : 0, inn = inset && inner ? offset(inner) : 0;
          const a = polar(from + out, outer), b = polar(to - out - 0.000001, outer),
            c = inner ? polar(to - inn - 0.000001, inner) : polar((from + to) / 2, inset ? inset / Math.sin((to - from) / 2) : 0),
            d = polar(from + inn, inner);
          return `M${a}A${outer},${outer} 0 ${to - from - 2 * out > Math.PI ? 1 : 0} 1 ${b}L${c}${inner ? `A${inner},${inner} 0 ${to - from - 2 * inn > Math.PI ? 1 : 0} 0 ${d}` : ""}Z`;
        };
        plot = (
          <div className="metric-chart-radial">
            {frame(unit => {
              let angle = -Math.PI / 2;
              return <>
                {(options.kind === "sunburst"
                  ? [...all].sort((a, b) =>
                      a.dimensions[
                        data.dimensionOrder?.[0] ?? Object.keys(a.dimensions)[0]
                      ].localeCompare(
                        b.dimensions[
                          data.dimensionOrder?.[0] ?? Object.keys(b.dimensions)[0]
                        ],
                      ),
                    )
                  : rows
                ).map((r) => {
                  const from = angle;
                  angle += (Number(r.value) / total) * Math.PI * 2;
                  return (
                    <path
                      key={r.key}
                      d={wedge(
                        from,
                        angle,
                        118,
                        options.kind === "pie"
                          ? 0
                          : options.kind === "sunburst"
                            ? 75 + 1.5 * unit
                            : 65,
                        1.5 * unit,
                      )}
                      fill={color(r.key)} fillOpacity={0.25}
                      stroke={color(r.key)}
                      strokeWidth={1} vectorEffect="non-scaling-stroke"
                    >
                      <title>{["", r.label, ": ", r.value, "·", " ", "", ((Number(r.value) / total) * 100).toFixed(1), "% "].join("").trim()}</title>
                    </path>
                  );
                })}
                {options.kind === "sunburst" &&
                  (() => {
                    const parents = new Map<string, number>();
                    for (const r of [...all].sort((a, b) =>
                      a.dimensions[
                        data.dimensionOrder?.[0] ?? Object.keys(a.dimensions)[0]
                      ].localeCompare(
                        b.dimensions[
                          data.dimensionOrder?.[0] ?? Object.keys(b.dimensions)[0]
                        ],
                      ),
                    )) {
                      const key =
                        r.dimensions[
                          data.dimensionOrder?.[0] ?? Object.keys(r.dimensions)[0]
                        ];
                      parents.set(key, (parents.get(key) ?? 0) + Number(r.value));
                    }
                    let a = -Math.PI / 2;
                    return [...parents].map(([name, v]) => {
                      const from = a;
                      a += (v / total) * Math.PI * 2;
                      return (
                        <path
                          key={name}
                          d={wedge(from, a, 75 - 1.5 * unit, 32, 1.5 * unit)}
                          fill={color(name)} fillOpacity={0.35}
                          stroke={color(name)} vectorEffect="non-scaling-stroke"
                        >
                          <title>{["", name, ": ", compact(v), ""].join("").trim()}</title>
                        </path>
                      );
                    });
                  })()}
                {options.kind === "donut" && (
                  <text className="metric-donut-value" x={260} y={140} textAnchor="middle" dominantBaseline="central">
                    {compact(decimalSum(rows.map((r) => r.value!)))}
                  </text>
                )}
              </>;
            }, "130 10 260 260",
            )}
            <ul className="metric-chart-radial-legend" aria-label="Chart legend">
              {(options.kind === "sunburst" ? parents.map(parent=>({key:parent,label:parent,dimensions:{[dimension]:parent},value:decimalSum(all.filter(r=>r.dimensions[dimension]===parent).map(r=>r.value!))})) : rows).map((r) => (
                <li key={r.key}>
                  <i className="metric-legend-ink" style={{ "--ink": color(r.key) } as CSSProperties} aria-hidden="true" />
                  <span className="metric-legend-content"><span className="metric-legend-name" title={r.label}>{Object.values(r.dimensions).join(" / ") || r.label}</span>
                  <strong title={`${r.value} ${data.unit}`}>{compact(r.value)} <small>{((Number(r.value) / total) * 100).toFixed(1)}%</small></strong></span>
                </li>
              ))}
            </ul>
          </div>
        );
      }
    } else if (["bullet", "gauge"].includes(options.kind)) {
      plot = options.kind === "bullet" ? <MetricBullet data={data} options={options} /> : <MetricGauge data={data} options={options} />;
    } else if (["scatter", "bubble"].includes(options.kind)) {
      const sources = data.sources;
      const xSeries = sources?.[0]?.series[0] ?? series[0],
        ySeries = sources?.[1]?.series[0] ?? series[1],
        zSeries = sources?.[2]?.series[0] ?? series[2];
      const rows = sources?.every((d) => d.partition)
        ? sources[0].rows
            .filter((r) => !r.other)
            .map((r) => ({
              key: r.key,
              label: r.label,
              x: r.value,
              y: sources[1].rows.find((y) => !y.other && y.key === r.key)
                ?.value,
              z: sources[2]?.rows.find((z) => !z.other && z.key === r.key)
                ?.value,
            }))
        : (xSeries?.points.map((p) => ({
            key: p.key,
            label: p.key,
            x: p.value,
            y: ySeries?.points.find((y) => y.key === p.key)?.value,
            z: zSeries?.points.find((z) => z.key === p.key)?.value,
          })) ?? []);
      const matched = rows.filter(
          (r) =>
            r.x !== null &&
            r.y != null &&
            (options.kind !== "bubble" || (r.z != null && Number(r.z) >= 0)),
        ),
        xs = matched.map((r) => Number(r.x)),
        ys = matched.map((r) => Number(r.y)),
        xmin = Math.min(0, ...xs),
        xmax = Math.max(1, ...xs),
        ymin = Math.min(0, ...ys),
        ymax = Math.max(1, ...ys),
        zmax = Math.max(1, ...matched.map((r) => Number(r.z ?? 1)));
      plot =
        matched.length === 0 ? (
          <p>
            No matching observations. Align the dimension values or reporting
            intervals.
          </p>
        ) : (
          frame(
            <>
              {matched.map((r) => (
                <circle
                  key={r.key}
                  cx={L + ((Number(r.x) - xmin) / (xmax - xmin)) * (R - L)}
                  cy={B - ((Number(r.y) - ymin) / (ymax - ymin)) * (B - T)}
                  r={
                    options.kind === "bubble"
                      ? 4 + Math.sqrt(Number(r.z) / zmax) * 22
                      : 5
                  }
                  fill={color(r.key)}
                  fillOpacity={0.6}
                >
                  <title>{["", r.label, ": X ", r.x, "; Y ", r.y, "", r.z ? `; size ${r.z}` : "", ""].join("").trim()}</title>
                </circle>
              ))}
              <text x={L} y={T}>
                {sources?.[1]?.title ?? ySeries?.name} (
                {sources?.[1]?.unit ?? ySeries?.unit}) · {compact(ymin)}–
                {compact(ymax)}
              </text>
              <text x={L} y={285}>
                {sources?.[0]?.title ?? xSeries?.name} (
                {sources?.[0]?.unit ?? xSeries?.unit}) · {compact(xmin)}–
                {compact(xmax)} · {matched.length} matched observations
              </text>
            </>,
          )
        );
    } else if (options.kind === "calendar") {
      const points = series[0]?.points ?? [],
        max = Math.max(1, ...points.map((p) => Math.abs(Number(p.value ?? 0))));
      const parts = (key: string) =>
        new Intl.DateTimeFormat("en-CA", {
          timeZone: data.timezone,
          year: "numeric",
          month: "2-digit",
          day: "2-digit",
        }).format(new Date(key));
      const dates = points.map((p) => parts(p.key)),
        months = [...new Set(dates.map((d) => d.slice(0, 7)))];
      plot = (
        <div className="metric-calendar">
          {months.map((month) => (
            <div key={month}>
              <strong>{month}</strong>
              <div className="metric-calendar-grid">
                {["S", "M", "T", "W", "T", "F", "S"].map((d, i) => (
                  <span key={i}>{d}</span>
                ))}
                {Array.from(
                  { length: new Date(month + "-01T12:00:00Z").getUTCDay() },
                  (_, i) => (
                    <span key={"blank" + i} />
                  ),
                )}
                {Array.from(
                  {
                    length: new Date(
                      Number(month.slice(0, 4)),
                      Number(month.slice(5)),
                      0,
                    ).getDate(),
                  },
                  (_, i) => {
                    const date = month + "-" + String(i + 1).padStart(2, "0"),
                      p = points[dates.indexOf(date)];
                    return (
                      <button
                        key={date}
                        title={`${date}: ${p?.value ?? "Unavailable"} ${data.unit}`}
                        disabled={!p}
                        onClick={() => p && onDrill?.(p, series[0])}
                        style={{
                          background:
                            p?.value != null
                              ? `color-mix(in srgb, var(--chart-1) ${8 + (Math.abs(Number(p.value)) / max) * 22}%, var(--background))`
                              : "transparent",
                          borderColor: p?.value != null ? "var(--chart-1)" : "var(--border)",
                          borderStyle: p?.value != null ? "solid" : "dashed",
                        }}
                      >
                        {i + 1}
                      </button>
                    );
                  },
                )}
              </div>
            </div>
          ))}
        </div>
      );
    } else if (options.kind === "matrix" || options.kind === "sankey") {
      const rows = data.rows,
        keys = data.dimensionOrder ?? Object.keys(rows[0]?.dimensions ?? {}),
        xs = [...new Set(rows.map((r) => r.dimensions[keys[0]]))],
        ys = [...new Set(rows.map((r) => r.dimensions[keys[1]]))];
      if (xs.length > 50 || ys.length > 50)
        plot = <p>Filter dimensions to at most 50 values per axis.</p>;
      else if (options.kind === "matrix") {
        const values = rows.filter(r => r.value !== null).map(r => Number(r.value));
        const low = values.length ? Math.min(...values) : 0, high = values.length ? Math.max(...values) : 0;
        const tint = (v: number) => 0.1 + (high === low ? .5 : (v - low) / (high - low)) * .55;
        plot = <div className="metric-matrix">{frame(unit => <>

            {rows.map((r) => {
              const x = xs.indexOf(r.dimensions[keys[0]]),
                y = ys.indexOf(r.dimensions[keys[1]]);
              return <g key={r.key}><rect
                  x={160 + (x * 640) / xs.length}
                  y={30 + (y * 210) / ys.length}
                  width={640 / xs.length - 4}
                  height={210 / ys.length - 4} rx={3}
                  fill={r.value === null ? "var(--muted)" : "var(--chart-1)"}
                  fillOpacity={r.value === null ? 0 : tint(Number(r.value))}
                  stroke={r.value === null ? "var(--border)" : "var(--chart-1)"} strokeDasharray={r.value === null ? "3 3" : undefined} vectorEffect="non-scaling-stroke"
                >
                  <title>{["", r.label, ": ", r.value ?? "Unavailable", ""].join("").trim()}</title>
                </rect>
                {xs.length <= 8 && ys.length <= 8 && <text x={160 + ((x + .5) * 640) / xs.length - 2} y={30 + ((y + .5) * 210) / ys.length - 2} dominantBaseline="central" textAnchor="middle" style={{fontSize:11 * unit}}>{compact(r.value)}</text>}
              </g>;
            })}
            {ys.map((y, i) => (
              <text
                key={y}
                x={150}
                y={38 + (i * 210) / ys.length}
                textAnchor="end"
              >
                {y.slice(0, 22)}
              </text>
            ))}
            {xs.map((x, i) => (
              <text
                key={x}
                x={160 + ((i + 0.5) * 640) / xs.length}
                y={270}
                textAnchor="middle"
              >
                {x.slice(0, 12)}
              </text>
            ))}
          </>)}<div className="metric-matrix-scale">{values.length ? <><span>Displayed range</span><span>{compact(low)}</span><i aria-hidden="true" /><span>{compact(high)}</span><span>{data.unit}</span></> : "No measured cells"}</div></div>;
      }
      else {
        const total = rows.reduce((v, r) => v + Number(r.value), 0),
          positions = (labels: string[], key: string) => {
            let y = 25;
            return new Map(
              labels.map((label) => {
                const h =
                    (rows
                      .filter((r) => r.dimensions[key] === label)
                      .reduce((v, r) => v + Number(r.value), 0) /
                      total) *
                    200,
                  start = y;
                y += h + 8;
                return [label, { y: start, h, offset: 0 }];
              }),
            );
          },
          left = positions(xs, keys[0]),
          right = positions(ys, keys[1]);
        plot = frame(
          <>
            {rows.map((r) => {
              const a = left.get(r.dimensions[keys[0]])!,
                b = right.get(r.dimensions[keys[1]])!,
                h = (Number(r.value) / total) * 200,
                ay = a.y + a.offset + h / 2,
                by = b.y + b.offset + h / 2;
              a.offset += h;
              b.offset += h;
              return (
                <path
                  key={r.key}
                  d={`M210,${ay} C400,${ay} 450,${by} 640,${by}`}
                  fill="none"
                  stroke={color(r.dimensions[keys[0]])}
                  strokeWidth={Math.max(0.5, h)}
                  opacity={0.35}
                >
                  <title>{["", r.label, ": ", r.value, ""].join("").trim()}</title>
                </path>
              );
            })}
            {[...left].map(([name, p]) => (
              <g key={name}>
                <rect
                  x={200}
                  y={p.y}
                  width={10}
                  height={p.h}
                  fill={color(name)}
                />
                <text x={190} y={p.y + p.h / 2 + 3} textAnchor="end">
                  {name.slice(0, 25)}
                </text>
              </g>
            ))}
            {[...right].map(([name, p]) => (
              <g key={name}>
                <rect
                  x={640}
                  y={p.y}
                  width={10}
                  height={p.h}
                  fill={color(name)}
                />
                <text x={660} y={p.y + p.h / 2 + 3}>
                  {name.slice(0, 25)}
                </text>
              </g>
            ))}
          </>,
          `0 0 860 ${Math.max(300, 230 + 8 * Math.max(xs.length, ys.length))}`,
        );
      }
    } else if (options.kind === "histogram" || options.kind === "box") {
      const d = data.distribution;
      if (d) {
        const min = Number(d.min),
          max = Number(d.max),
          x = (v: string) =>
            L + ((Number(v) - min) / (max - min || 1)) * (R - L);
        plot = frame(unit =>
          options.kind === "box" ? (
            <>
              <line
                x1={x(d.min)}
                x2={x(d.max)}
                y1={120}
                y2={120}
                stroke="var(--foreground)"
              />
              <rect
                x={x(d.q1)}
                y={90}
                width={Math.max(1, x(d.q3) - x(d.q1))}
                height={60}
                fill="var(--chart-1)"
                fillOpacity={0.3}
                stroke="var(--chart-1)"
              />
              <line
                x1={x(d.median)}
                x2={x(d.median)}
                y1={90}
                y2={150}
                stroke="var(--foreground)"
              />
              {[d.min, d.q1, d.median, d.q3, d.max].map((v, i) => (
                <text
                  key={i}
                  x={x(v)}
                  y={185 + (i % 2) * 20}
                  textAnchor="middle"
                >
                  {compact(v)}
                </text>
              ))}
              <text x={L} y={260}>
                Min / Q1 / median / Q3 / max · {d.count} retained individual
                observations
              </text>
            </>
          ) : (
            <>
              {d.bins.map((b, i) => {
                const width = (R - L) / d.bins.length,
                  height =
                    (b.count / Math.max(1, ...d.bins.map((b) => b.count))) *
                    (B - T);
                return (
                  <rect
                    key={i}
                    x={L + i * width + 1.5 * unit}
                    y={B - height}
                    width={Math.max(0, width - 3 * unit)}
                    height={height}
                    fill="var(--chart-1)" fillOpacity={0.25} stroke="var(--chart-1)" vectorEffect="non-scaling-stroke" rx={2}
                  >
                    <title>{["", b.from, "–", b.to, ": ", b.count, "observations "].join("").trim()}</title>
                  </rect>
                );
              })}
              <text x={L} y={280}>
                <title>{[d.min, "— ", d.max, "", data.unit].join("").trim()}</title>
                {compact(d.min)} — {compact(d.max)} {data.unit} · {d.count} observations
              </text>
            </>
          ),
        );
      }
    } else if (options.kind === "radar") {
      const rows = data.rows.slice(0, 12),
        n = rows.length,
        point = (i: number, f: number) => [
          430 + Math.cos((i / n) * 2 * Math.PI - Math.PI / 2) * f * 105,
          145 + Math.sin((i / n) * 2 * Math.PI - Math.PI / 2) * f * 105,
        ];
      plot = frame(
        <>
          {[0.25, 0.5, 0.75, 1].map((f) => (
            <polygon
              key={f}
              points={rows.map((_, i) => point(i, f).join(",")).join(" ")}
              fill="none"
              stroke="var(--border)"
            />
          ))}
          <polygon
            points={rows
              .map((r, i) =>
                point(
                  i,
                  Math.max(
                    0,
                    Math.min(
                      1,
                      (Number(r.value) - options.min!) /
                        (options.max! - options.min!),
                    ),
                  ),
                ).join(","),
              )
              .join(" ")}
            fill="var(--chart-1)"
            fillOpacity={0.2}
            stroke="var(--chart-1)"
          />
          {rows.map((r, i) => {
            const [x, y] = point(i, 1.22);
            return (
              <text key={r.key} x={x} y={y} textAnchor={x<420?"end":x>440?"start":"middle"}>
                {rowName(r).slice(0, 16)}
              </text>
            );
          })}
          <text x={430} y={285} textAnchor="middle">Scale {options.min}–{options.max}</text>
        </>,
        "210 0 440 300",
      );
    } else if (options.kind === "state")
      plot = <MetricState data={data} series={series} threshold={options.stateThreshold!} />;
  }
  const selectedKey = keys[Math.min(focus ?? keys.length - 1, keys.length - 1)];
  const hasLegend = interactive;
  return (
    <figure data-slot="metric-chart" className={cn("metric-chart grid gap-3", className)} data-chart-kind={options.kind}>
      <figcaption className="flex items-start justify-between gap-3">
        <div className="grid gap-0.5">
          <div className="text-sm leading-snug font-medium">{data.title}</div>
          {description && <div className="text-xs text-muted-foreground">{description}</div>}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {interactive && !showTable && !issue && <span className="font-mono text-xs text-muted-foreground tabular-nums">{focus === null ? "latest" : dateLabel(selectedKey ?? "", data.timezone)}</span>}
          {options.kind !== "table" && <Button
            variant="ghost"
            size="icon-xs"
            aria-pressed={showTable}
            aria-label={showTable ? "Show the chart" : "Show the values as a table"}
            title={showTable ? "Show the chart" : "Show the values as a table"}
            icon={showTable ? <ChartArea /> : <Table2 />}
            onClick={() => setTable(!table)}
          />}
        </div>
      </figcaption>
      {showTable ? dataTable : issue ? <p className="flex min-h-44 items-center justify-center rounded-lg border border-dashed p-4 text-xs text-muted-foreground" role="status">{issue}</p> : (
        <div className={hasLegend ? "metric-chart-body" : undefined}>
          <div className="metric-chart-plot">{plot}</div>
          {hasLegend && <ul className="metric-chart-legend" aria-label="Chart legend">
            {data.series.map(s => {
              const point = s.points.find(p => p.key === selectedKey);
              return <li key={s.id}>
                <button type="button" aria-pressed={!hidden.includes(s.id)} aria-label={`Toggle ${s.name}`} title={`${s.name}: ${point?.value ?? "Unavailable"} ${s.unit} · ${point?.quality ?? "missing"} · ${observationLabel(point ?? {},data.timezone)}`} onClick={() => setHidden(hidden.includes(s.id) ? hidden.filter(id => id !== s.id) : [...hidden, s.id])}>
                  <i className="metric-legend-ink" style={{ "--ink": color(s.id) } as CSSProperties} aria-hidden="true" />
                  <span className="metric-legend-content"><span className="metric-legend-name">{Object.values(s.dimensions).join(" / ") || (s.comparison ? "Previous" : s.name === "All" ? "Current" : s.name)}{s.projection ? " · Estimated" : s.comparison && Object.keys(s.dimensions).length ? " · Previous" : ""}</span><strong>{compact(point?.value ?? null)}</strong></span>
                </button>
              </li>;
            })}
          </ul>}
        </div>
      )}
      {!showTable && !issue && ["dumbbell","slope"].includes(options.kind) && <p className="metric-comparison-key">○ Previous period <span>● Selected period</span></p>}
    </figure>
  );
}
// Keep labels readable when a dashboard card is narrower than SVG coordinates.
function MetricPlot({ viewBox, style, children, ...props }: Omit<SVGProps<SVGSVGElement>, "children"> & {children?: React.ReactNode | ((unit: number) => React.ReactNode)}) {
  const ref = useRef<SVGSVGElement>(null);
  const [width, setWidth] = useState(0);
  useEffect(() => {
    if (!ref.current) return;
    const observer = new ResizeObserver(([entry]) =>
      setWidth(entry.contentRect.width),
    );
    observer.observe(ref.current);
    return () => observer.disconnect();
  }, []);
  const coordinateWidth = Number(viewBox?.split(" ")[2] ?? W);
  const labelSize = width > 0
    ? Math.max(13, Math.min(30, (12 * coordinateWidth) / width))
    : 13;
  return (
    <svg
      {...props}
      ref={ref}
      viewBox={viewBox}
      style={{ ...style, "--metric-label-size": `${labelSize}px` } as CSSProperties}
    >{typeof children === "function" ? children(width > 0 ? coordinateWidth / width : 1) : children}</svg>
  );
}

function Cartesian({ data, kind, color, target, focus, onFocus, allKeys }: {
  data: PlotData;
  kind: string;
  color: (id: string) => string;
  target?: number;
  focus?: number;
  onFocus?: (index: number | null) => void;
  allKeys?: string[];
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(520);
  const titleId = useId();
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const observer = new ResizeObserver(([entry]) => setWidth(Math.max(160, entry.contentRect.width)));
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  const L = 46, R = width - 8, T = 12, B = 188;
  const frame = (children: React.ReactNode) => <div className="metric-cartesian" ref={ref}>
    <svg viewBox={`0 0 ${width} 226`} role="img" aria-labelledby={titleId} tabIndex={onFocus ? 0 : undefined}
      onPointerMove={onFocus ? e => {const rect=e.currentTarget.getBoundingClientRect(); onFocus(Math.max(0,Math.min(keys.length-1,Math.floor(((e.clientX-rect.left-L)/(R-L))*keys.length))));} : undefined}
      onPointerLeave={()=>onFocus?.(null)} onBlur={()=>onFocus?.(null)}
      onKeyDown={e=>{if(!onFocus)return;const current=focus??keys.length-1; const next=e.key==="Home"?0:e.key==="End"?keys.length-1:e.key==="ArrowLeft"?Math.max(0,current-1):e.key==="ArrowRight"?Math.min(keys.length-1,current+1):undefined;if(next!==undefined){e.preventDefault();onFocus(next);}else if(e.key==="Escape"){e.preventDefault();onFocus(null);}}}>
      <title id={titleId}>{`${data.title}. ${data.unit}. Exact values are available in the table.`}</title>{children}
    </svg>
  </div>;
  const series = data.series,
    keys = allKeys ?? [
      ...new Set(series.flatMap((s) => s.points.map((p) => p.key))),
    ].sort();
  const maps = series.map(
      (s) => new Map(s.points.map((p) => [p.key, p.value])),
    ),
    stack = kind.startsWith("stacked"),
    rank = kind === "bump";
  const values = maps.flatMap((m) =>
    [...m.values()].filter((v) => v !== null).map(Number),
  );
  if (target !== undefined) values.push(target);
  const sums = keys.map((k) =>
      maps.reduce((v, m) => v + Number(m.get(k) ?? 0), 0),
    ),
    lower = rank ? 1 : Math.min(0, ...values),
    upper = rank ? Math.max(2, series.length) : Math.max(1, ...values, ...(stack ? sums : [])),
    scale = axis(Math.max(Math.abs(lower), Math.abs(upper)), data.unit),
    step = scale[1] || 1,
    low = rank ? lower : Math.floor(lower / step) * step,
    high = rank ? upper : Math.ceil(upper / step) * step,
    ticks = rank ? Array.from({length: Math.max(2, series.length)}, (_,i)=>i+1) : Array.from({length: Math.round((high-low)/step)+1},(_,i)=>low+i*step),
    x = (i: number) => L + ((i + 0.5) * (R - L)) / Math.max(1, keys.length),
    y = (v: number) =>
      rank
        ? T + ((v - low) / (high - low)) * (B - T)
        : B - ((v - low) / (high - low)) * (B - T);
  if (series.length * keys.length > 10000)
    return (
      <p>
        Choose a coarser interval or fewer groups to display at most 10,000
        points.
      </p>
    );
  return frame(
    <>
      {focus !== undefined && keys.length > 0 && (
        <line
          x1={x(Math.min(focus, keys.length - 1))}
          x2={x(Math.min(focus, keys.length - 1))}
          y1={T}
          y2={B}
          stroke="var(--muted-foreground)"
          strokeDasharray="2 4"
          opacity={0.6}
        />
      )}
      {ticks.map((v, i) => {
        return (
          <g key={i}>
            <line x1={L} x2={R} y1={y(v)} y2={y(v)} stroke="var(--border)" />
            <text x={L - 12} y={y(v) + 4} textAnchor="end">
              {compact(v)}
            </text>
          </g>
        );
      })}
      {target !== undefined && (
        <g>
          <line
            x1={L}
            x2={R}
            y1={y(target)}
            y2={y(target)}
            stroke="var(--foreground)"
            strokeDasharray="4 4"
          />
          <text x={R} y={y(target) - 6} textAnchor="end">
            Target {target}
          </text>
        </g>
      )}
      {series.map((s, si) => {
        const nums = keys.map((k) => {
          const v = maps[si].get(k);
          if (v == null) return null;
          if (rank) {
            const sorted = maps
              .map((m, i) => ({ i, v: m.get(k) }))
              .filter((x) => x.v != null)
              .sort((a, b) => Number(b.v) - Number(a.v) || a.i - b.i);
            return sorted.findIndex((x) => x.i === si) + 1;
          }
          return Number(v);
        });
        const baseline = (i: number) =>
          stack
            ? maps
                .slice(0, si)
                .reduce((v, m) => v + Number(m.get(keys[i]) ?? 0), 0)
            : 0;
        return (
          <g key={s.id} fill={color(s.id)} stroke={color(s.id)}>
            {s.anchor &&
              kind === "line" &&
              s.points[0]?.value != null &&
              keys.includes(s.anchor.key) && (
                <line
                  data-projection-connector="true"
                  x1={x(keys.indexOf(s.anchor.key))}
                  y1={y(Number(s.anchor.value))}
                  x2={x(keys.indexOf(s.points[0].key))}
                  y2={y(Number(s.points[0].value))}
                  strokeWidth={1.5} vectorEffect="non-scaling-stroke"
                  strokeDasharray="5 4"
                />
              )}
            {nums.map((n, i) => {
              if (n === null) return null;
              if (stack && maps.some((m) => m.get(keys[i]) == null))
                return null;
              const base = baseline(i),
                prev = nums[i - 1],
                w = (R - L) / Math.max(1, keys.length),
                bar = kind === "bar" || kind === "stacked_bar";
              return (
                <g key={keys[i]}>
                  <title>{`${s.name} · ${keys[i]}: ${maps[si].get(keys[i])} ${s.unit} · ${observationLabel(s.points.find(p=>p.key===keys[i])??{},data.timezone)}`}</title>
                  {bar ? (
                    <rect
                      x={
                        x(i) -
                        w * 0.4 +
                        (stack ? 0 : (si * w * 0.8) / series.length)
                      }
                      y={Math.min(y(base), y(base + n))}
                      width={Math.max(
                        0.5,
                        (w * 0.8) / (stack ? 1 : series.length) - 1,
                      )}
                      height={Math.max(0.5, Math.abs(y(base + n) - y(base)))}
                      fillOpacity={s.comparison ? 0.16 : 0.22} strokeWidth={1} vectorEffect="non-scaling-stroke" rx={1}
                    />
                  ) : (
                    <>
                      {i > 0 &&
                        prev !== null &&
                        (!stack ||
                          maps.every((m) => m.get(keys[i - 1]) != null)) && (
                          <>
                            {(["line", "step", "area", "stacked_area"].includes(kind)) && (
                              <path
                                d={kind === "step" ? `M${x(i-1)},${y(prev)}H${x(i)}V${y(n)}L${x(i)},${y(0)}L${x(i-1)},${y(0)}Z` : `M${x(i - 1)},${y(prev + baseline(i - 1))}L${x(i)},${y(n + base)}L${x(i)},${y(base)}L${x(i - 1)},${y(baseline(i - 1))}Z`}
                                fillOpacity={kind === "stacked_area" ? 0.22 : kind === "area" ? 0.2 : 0.10}
                                stroke="none"
                              />
                            )}
                            <path
                              d={
                                kind === "step"
                                  ? `M${x(i - 1)},${y(prev)}H${x(i)}V${y(n)}`
                                  : `M${x(i - 1)},${y(prev + baseline(i - 1))}L${x(i)},${y(n + base)}`
                              }
                              fill="none"
                              strokeWidth={1.5} vectorEffect="non-scaling-stroke"
                              strokeDasharray={
                                s.comparison || s.projection || s.points.find(p=>p.key===keys[i])?.quality==='stale' ? "5 4" : undefined
                              }
                            />
                          </>
                        )}
                      {(focus === i || rank || (prev == null && nums[i + 1] == null)) && (
                        <circle
                          cx={x(i)}
                          cy={y(n + base)}
                          r={rank ? 3 : 2.5}
                          strokeWidth={rank ? 1.5 : 1} vectorEffect="non-scaling-stroke"
                          fill={rank ? `color-mix(in oklch, ${color(s.id)} 25%, var(--background))` :
                            s.points.find((p) => p.key === keys[i])?.quality ===
                            "complete"
                              ? color(s.id)
                              : "var(--background)"
                          }
                        />
                      )}
                    </>
                  )}
                </g>
              );
            })}
          </g>
        );
      })}
      {keys.map((key, i) =>
        i % Math.max(1, Math.ceil(keys.length / Math.max(2, Math.floor((R-L)/90)))) === 0 ? (
          <text key={key} x={x(i)} y={207} textAnchor="middle">
            <tspan x={x(i)}>{dateLabel(key, data.timezone, true)}</tspan>
            {["minute", "5_minutes", "15_minutes", "hour"].includes(data.interval) && Number.isFinite(Date.parse(key)) && (
              <tspan x={x(i)} dy={14}>
                {new Intl.DateTimeFormat("en", {timeZone:data.timezone,hour:"2-digit",minute:"2-digit",hourCycle:"h23"}).format(new Date(key))}
              </tspan>
            )}
          </text>
        ) : null,
      )}
    </>,
  );
}
function treeRects(
  items: { i: number; value: number }[],
  x: number,
  y: number,
  w: number,
  h: number,
): { i: number; x: number; y: number; w: number; h: number }[] {
  if (!items.length) return [];
  if (items.length === 1) return [{ i: items[0].i, x, y, w, h }];
  const total = items.reduce((n, i) => n + i.value, 0);
  let cut = 1,
    sum = items[0].value;
  while (cut < items.length - 1 && sum < total / 2) {
    sum += items[cut].value;
    cut++;
  }
  const ratio = sum / total;
  return w >= h
    ? [
        ...treeRects(items.slice(0, cut), x, y, w * ratio, h),
        ...treeRects(items.slice(cut), x + w * ratio, y, w * (1 - ratio), h),
      ]
    : [
        ...treeRects(items.slice(0, cut), x, y, w, h * ratio),
        ...treeRects(items.slice(cut), x, y + h * ratio, w, h * (1 - ratio)),
      ];
}
