import {
  Area,
  AreaChart,
  CartesianGrid,
  Legend,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { Chart } from "@/lib/api";
import { metricValue, timeLabel } from "@/lib/format";
import { cn } from "@/lib/utils";

const palette = [
  "var(--color-chart-1)",
  "var(--color-chart-2)",
  "var(--color-chart-3)",
  "var(--color-chart-4)",
  "var(--color-chart-5)",
];

// Status classes get semantic colours in the throughput chart.
const classColors: Record<string, string> = {
  "2xx": "oklch(0.75 0.16 150)",
  "3xx": "var(--color-chart-2)",
  "4xx": "oklch(0.80 0.16 85)",
  "5xx": "oklch(0.65 0.22 25)",
};

const subtitles: Record<string, string> = {
  throughput: "requests per second by response class",
  latency: "response time percentiles",
  instances: "running instances per process type",
  cpu: "per process; shared sizes can burst above their allocation",
  memory: "per process",
  network: "instance network traffic",
};

// What a chart ignores, appended to its subtitle when the API reports it
// cannot break down per instance. It is driven off that flag rather than a
// list of chart ids kept in the page, which is how the instances chart came to
// be the one uncovered case — but the flag alone cannot supply the wording:
// the edge charts have no process either, while instances does honour the
// process filter and merely has a single number to draw per process. So the
// text stays per chart, here with the rest of the copy.
const ignoredControls: Record<string, string> = {
  throughput:
    "measured at the edge, so the process, instance and aggregation controls do not apply",
  latency:
    "measured at the edge, so the process, instance and aggregation controls do not apply",
  instances:
    "one number per process, so the instance and aggregation controls do not apply",
};

function chartSubtitle(chart: Chart): string | undefined {
  const parts = [subtitles[chart.id]];
  if (!chart.instanceCapable) parts.push(ignoredControls[chart.id]);
  return parts.filter(Boolean).join(" — ") || undefined;
}

// A line the chart draws across itself: the allocation a series is measured
// against, or the burst ceiling above it.
type RefLine = { value: number; label: string; stroke: string; dash: string };

// referenceLines collects one line per distinct value the API sent, labelled
// with the processes that share it.
//
// Per distinct value, not per series and not one agreed number: forty
// instances of web at one size are one allocation, while web at shared-s and
// worker at dedicated-m are two, and a single line would be wrong for one of
// them. Requiring agreement instead — which is what this did — drew no line at
// all on exactly that project, and the subtitle went on to claim it had no
// allocation set.
function referenceLines(chart: Chart): RefLine[] {
  const kinds = [
    {
      name: "Allocated",
      stroke: "oklch(0.65 0.22 25)",
      dash: "2 4",
      of: (s: Chart["series"][number]) => s.reference,
    },
    {
      name: "Burst",
      stroke: "oklch(0.80 0.16 85)",
      dash: "1 5",
      of: (s: Chart["series"][number]) => s.burst,
    },
  ];
  const lines: RefLine[] = [];
  for (const kind of kinds) {
    const byValue = new Map<number, Set<string>>();
    for (const s of chart.series) {
      const value = kind.of(s);
      if (!value) continue;
      const processes = byValue.get(value) ?? new Set<string>();
      const p = processOf(s.name);
      if (p) processes.add(p);
      byValue.set(value, processes);
    }
    for (const [value, processes] of byValue) {
      // One line needs no attribution; several do, because the process is then
      // the only thing saying which line to read which series against.
      const who = byValue.size > 1 ? [...processes].sort().join(", ") : "";
      lines.push({
        value,
        label: who
          ? `${who} ${kind.name.toLowerCase()} ${metricValue(value, chart.unit)}`
          : `${kind.name} ${metricValue(value, chart.unit)}`,
        stroke: kind.stroke,
        dash: kind.dash,
      });
    }
  }
  return lines.sort((a, b) => a.value - b.value);
}

// The process a series belongs to, for labelling only: "web", "web.2" and
// "web.2 in" all read as web, and an aggregate carries its process after the
// aggregation ("sum web", "avg web in"). A replaced instance is "replaced 1"
// and names no process at all — which is exactly why the server stopped
// reading the process out of these names and sends it with the series instead.
// Nothing here has to be right for a number to be right; it only labels a line.
function processOf(name: string): string {
  const parts = name.split(" ");
  let head = parts[0];
  if (head === "replaced") return "";
  if (parts.length > 1 && ["sum", "avg", "max"].includes(head)) head = parts[1];
  return head.split(".")[0];
}

type Props = {
  chart: Chart;
  range: string;
  releases?: { number: number; time: number; label: string }[];
  className?: string;
  // Overrides the per-project copy keyed by chart id (the cluster page reuses
  // the cpu/memory ids for node utilisation).
  subtitle?: string;
};

export function MetricChart({
  chart,
  range,
  releases = [],
  className,
  subtitle: subtitleOverride,
}: Props) {
  // Merge series into one row per timestamp: { t, [name]: value }.
  const rows = new Map<number, Record<string, number>>();
  for (const s of chart.series) {
    for (const [t, v] of s.points) {
      const row = rows.get(t) ?? { t };
      row[s.name] = v;
      rows.set(t, row);
    }
  }
  const data = [...rows.values()].sort((a, b) => a.t - b.t);
  const last = chart.series.map((s) => ({
    name: s.name,
    v: s.points.length ? s.points[s.points.length - 1][1] : undefined,
  }));
  const stacked = chart.kind === "stacked" || chart.kind === "step";
  const step = chart.kind === "step";
  const percent = chart.unit === "%";
  // In absolute mode the API sends, per series, the allocation it is measured
  // against and the ceiling it may burst to.
  const refLines = percent ? [] : referenceLines(chart);
  // Whether an allocation is known at all is a property of the series, not of
  // whether they agree on one number: a project with two process types at two
  // sizes has two allocations, and telling it "no allocation set yet" was
  // simply untrue. An empty chart says nothing either way.
  const noAllocation =
    chart.series.length > 0 && chart.series.every((s) => !s.reference);
  const absoluteUnit = chart.unit === "cores" || chart.unit === "bytes";
  const subtitle =
    subtitleOverride ??
    (absoluteUnit && noAllocation
      ? "absolute usage (no allocation set yet)"
      : chartSubtitle(chart));
  const hot = percent && last.some((l) => (l.v ?? 0) >= 85);

  return (
    <Card size="sm" className={className}>
      <CardHeader>
        <CardTitle className="grid gap-0.5 text-sm font-medium">
          <div className="flex items-baseline justify-between gap-2">
            <span>{chart.title}</span>
            <span
              className={cn(
                "truncate font-mono text-xs text-muted-foreground",
                hot && "text-red-500",
              )}
            >
              {last
                .filter((l) => l.v !== undefined)
                .map(
                  (l) =>
                    `${chart.series.length > 1 || chart.id === "cpu" || chart.id === "memory" ? l.name + " " : ""}${metricValue(l.v as number, chart.unit)}`,
                )
                .join(" · ") || "-"}
            </span>
          </div>
          {subtitle && (
            <span className="text-xs font-normal text-muted-foreground">
              {subtitle}
            </span>
          )}
          {/* The note is about the data, not about the chart's meaning — "3
              more instances not shown" is something to act on — so it does not
              render as a second line of the same muted subtitle. */}
          {chart.note && (
            <span className="text-xs font-medium text-foreground/75">
              {chart.note}
            </span>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {chart.error ? (
          <div className="flex h-44 items-center justify-center text-center text-xs text-muted-foreground">
            {chart.error}
          </div>
        ) : data.length === 0 ? (
          <div className="flex h-44 items-center justify-center text-xs text-muted-foreground">
            No data in this range
          </div>
        ) : (
          <div className="h-44">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart
                data={data}
                margin={{ top: 4, right: 4, bottom: 0, left: -12 }}
              >
                <defs>
                  {chart.series.map((s, i) => (
                    <linearGradient
                      key={s.name}
                      id={`g-${chart.id}-${i}`}
                      x1="0"
                      y1="0"
                      x2="0"
                      y2="1"
                    >
                      <stop
                        offset="0%"
                        stopColor={color(chart, s.name, i)}
                        stopOpacity={stacked ? 0.55 : 0.3}
                      />
                      <stop
                        offset="100%"
                        stopColor={color(chart, s.name, i)}
                        stopOpacity={stacked ? 0.35 : 0}
                      />
                    </linearGradient>
                  ))}
                </defs>
                <CartesianGrid
                  vertical={false}
                  stroke="var(--color-border)"
                  strokeDasharray="3 3"
                />
                <XAxis
                  dataKey="t"
                  type="number"
                  domain={["dataMin", "dataMax"]}
                  tickFormatter={(t: number) => timeLabel(t, range)}
                  tick={{ fontSize: 10, fill: "var(--color-muted-foreground)" }}
                  tickLine={false}
                  axisLine={false}
                  minTickGap={48}
                />
                <YAxis
                  tick={{ fontSize: 10, fill: "var(--color-muted-foreground)" }}
                  tickLine={false}
                  axisLine={false}
                  tickFormatter={(v: number) =>
                    metricValue(v, chart.unit).replace(/ (rps|cores|ms)$/, "")
                  }
                  width={64}
                  allowDecimals={chart.unit !== "count"}
                  // Every horizontal line the chart draws has to be inside
                  // this domain: ReferenceLine defaults to
                  // ifOverflow="discard" and renders nothing at all when it
                  // falls outside, while an axis left to compute itself only
                  // reaches the highest data point. A process using 0.02 of
                  // its 0.5 allocated cores therefore lost both its
                  // allocation and its burst line — the one thing Total mode
                  // exists to show — until usage caught up with the
                  // allocation. Anything added below as a ReferenceLine with
                  // a y value must be folded in here too.
                  domain={
                    percent
                      ? [
                          0,
                          (max: number) =>
                            Math.max(100, Math.ceil(max / 10) * 10),
                        ]
                      : step
                        ? [0, (max: number) => Math.max(1, Math.ceil(max))]
                        : refLines.length > 0
                          ? [
                              0,
                              (max: number) =>
                                Math.max(max, ...refLines.map((l) => l.value)) *
                                1.05,
                            ]
                          : undefined
                  }
                />
                <Tooltip
                  contentStyle={{
                    background: "var(--color-popover)",
                    border: "1px solid var(--color-border)",
                    borderRadius: 8,
                    fontSize: 12,
                  }}
                  labelStyle={{ color: "var(--color-muted-foreground)" }}
                  labelFormatter={(t) =>
                    new Date(Number(t) * 1000).toLocaleString()
                  }
                  formatter={(v, name) => [
                    metricValue(Number(v), chart.unit),
                    String(name),
                  ]}
                />
                {chart.series.length > 1 && (
                  <Legend wrapperStyle={{ fontSize: 11 }} iconSize={8} />
                )}
                {percent && (
                  <ReferenceLine
                    y={100}
                    stroke="oklch(0.65 0.22 25)"
                    strokeDasharray="2 4"
                  />
                )}
                {refLines.map((l, i) => (
                  <ReferenceLine
                    key={`${l.label}-${l.value}`}
                    y={l.value}
                    stroke={l.stroke}
                    strokeDasharray={l.dash}
                    label={{
                      value: l.label,
                      // Sorted by value and alternating sides, so two lines
                      // close together — a process and another process one
                      // size up — do not print their labels on top of each
                      // other.
                      position: i % 2 ? "insideTopRight" : "insideTopLeft",
                      fontSize: 10,
                    }}
                  />
                ))}
                {releases.map((r) => (
                  <ReferenceLine
                    key={r.number}
                    x={r.time}
                    stroke="var(--color-primary)"
                    strokeDasharray="4 3"
                    label={{
                      value: r.label,
                      position: "insideTopRight",
                      fontSize: 10,
                      fill: "var(--color-primary)",
                    }}
                  />
                ))}
                {chart.series.map((s, i) => (
                  <Area
                    key={s.name}
                    type={step ? "stepAfter" : "monotone"}
                    dataKey={s.name}
                    name={s.name}
                    stackId={stacked ? "a" : undefined}
                    stroke={color(chart, s.name, i)}
                    fill={`url(#g-${chart.id}-${i})`}
                    strokeWidth={1.5}
                    connectNulls
                    isAnimationActive={false}
                  />
                ))}
              </AreaChart>
            </ResponsiveContainer>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function color(chart: Chart, name: string, i: number): string {
  if (chart.id === "throughput" && classColors[name]) return classColors[name];
  return palette[i % palette.length];
}
