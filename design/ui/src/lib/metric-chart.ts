// Presentation contracts and exact decimal helpers extracted from Warehouse.
export type ObservationMetadata = {observedAt?:string|null;oldestObservedAt?:string|null;knownCount?:number;expectedCount?:number;staleCount?:number};
export const chartKinds = [
  "line",
  "step",
  "bar",
  "area",
  "stacked_bar",
  "stacked_area",
  "small_multiples",
  "table",
  "ranking",
  "pie",
  "donut",
  "treemap",
  "sunburst",
  "pareto",
  "waterfall",
  "bullet",
  "gauge",
  "scatter",
  "bubble",
  "calendar",
  "matrix",
  "dumbbell",
  "slope",
  "bump",
  "histogram",
  "box",
  "funnel",
  "sankey",
  "radar",
  "state",
] as const;
export type ChartKind = (typeof chartKinds)[number];
export const chartNames: Record<ChartKind, string> = {
  line: "Line",
  step: "Step",
  bar: "Grouped bars",
  area: "Area",
  stacked_bar: "Stacked bars",
  stacked_area: "Stacked area",
  small_multiples: "Small multiples",
  table: "Table",
  ranking: "Horizontal ranking",
  pie: "Pie",
  donut: "Donut",
  treemap: "Treemap",
  sunburst: "Sunburst",
  pareto: "Pareto",
  waterfall: "Waterfall",
  bullet: "Bullet",
  gauge: "Gauge",
  scatter: "Scatter",
  bubble: "Bubble",
  calendar: "Calendar heatmap",
  matrix: "Matrix heatmap",
  dumbbell: "Dumbbell",
  slope: "Slope",
  bump: "Rank over time",
  histogram: "Histogram",
  box: "Box plot",
  funnel: "Funnel",
  sankey: "Sankey",
  radar: "Radar",
  state: "State timeline",
};
export type ChartOptions = {
  kind: ChartKind;
  target?: number;
  min?: number;
  max?: number;
  contributions?: boolean;
  stageTotals?: boolean;
  orientation?: "vertical" | "horizontal";
  stateThreshold?: number;
};
export type PlotPoint = ObservationMetadata & {
  key: string;
  value: string | null;
  quality: string;
  original?: string;
};
export type PlotSeries = ObservationMetadata & {
  anchor?: { key: string; value: string };
  id: string;
  name: string;
  unit: string;
  points: PlotPoint[];
  total: string | null;
  dimensions: Record<string, string>;
  comparison?: boolean;
  projection?: boolean;
};
export type PlotRow = {
  other?: boolean;
  key: string;
  label: string;
  dimensions: Record<string, string>;
  value: string | null;
  previous?: string | null;
};
export type Distribution = {
  count: number;
  min: string;
  q1: string;
  median: string;
  q3: string;
  max: string;
  bins: { from: string; to: string; count: number }[];
};
export type PlotData = ObservationMetadata & {
  quality?: string;
  dimensionOrder?: string[];
  measurementKind?: string;
  total?: string | null;
  sources?: PlotData[];
  title: string;
  unit: string;
  timezone: string;
  interval: string;
  startsAt: string;
  endsAt: string;
  series: PlotSeries[];
  rows: PlotRow[];
  additive: boolean;
  partition: boolean;
  completeGroups: boolean;
  distribution?: Distribution;
  notes: string[];
};
export const dimensionKey = (d: Record<string, string>) =>
  JSON.stringify(Object.entries(d).sort(([a], [b]) => a.localeCompare(b)));
export const dimensionLabel = (d: Record<string, string>) =>
  Object.entries(d)
    .map(([k, v]) => `${k}: ${v}`)
    .join(" · ") || "All";
// Exact decimal arithmetic for composition and rankings. Floating point is only for SVG coordinates.
function decimalParts(s: string) {
  if (!/^-?\d+(\.\d+)?$/.test(s)) throw Error("Invalid decimal");
  const [a, b = ""] = s.split(".");
  return { value: BigInt(a + b), scale: b.length };
}
export function decimalSum(values: string[]) {
  const p = values.map(decimalParts),
    scale = Math.max(0, ...p.map((x) => x.scale));
  const n = p.reduce(
      (v, x) => v + x.value * 10n ** BigInt(scale - x.scale),
      0n,
    ),
    negative = n < 0n;
  const digits = (negative ? -n : n).toString().padStart(scale + 1, "0");
  return `${negative ? "-" : ""}${scale ? digits.slice(0, -scale) + "." + digits.slice(-scale) : digits}`;
}
export function decimalCompare(a: string, b: string) {
  const x = decimalParts(a),
    y = decimalParts(b),
    scale = Math.max(x.scale, y.scale);
  const d =
    x.value * 10n ** BigInt(scale - x.scale) -
    y.value * 10n ** BigInt(scale - y.scale);
  return d < 0n ? -1 : d > 0n ? 1 : 0;
}
export function chartIssue(d: PlotData, o: ChartOptions): string | undefined {
  const k = o.kind,
    current = d.series.filter((s) => !s.comparison),
    valid = d.rows.filter((r) => r.value !== null);
  if (
    !current.length ||
    (!current.some((s) => s.points.some((p) => p.value !== null)) &&
      !valid.length &&
      !(k === "state" && current.some((s) => s.points.length > 0)))
  )
    return "No eligible measurements for this period.";
  if (["histogram", "box"].includes(k) && !d.distribution)
    return "Load retained individual samples to display a distribution. Aggregated values cannot reconstruct it.";
  if (
    ["bullet", "gauge", "radar"].includes(k) &&
    (!Number.isFinite(o.min) || !Number.isFinite(o.max) || o.max! <= o.min!)
  )
    return "Set an explicit minimum and maximum.";
  if (
    ["bullet", "gauge"].includes(k) &&
    current.length === 1 &&
    (current[0].total === null ||
      Number(current[0].total) < o.min! ||
      Number(current[0].total) > o.max!)
  )
    return "Choose scale bounds that include the measured value. No value will be silently clipped.";
  if (
    ["bullet", "gauge"].includes(k) &&
    (!Number.isFinite(o.target) || o.target! < o.min! || o.target! > o.max!)
  )
    return "Set a target inside the scale.";
  if (k === "state" && d.measurementKind !== "latest")
    return "Use a snapshot metric for observed states; flow totals and averages do not describe states.";
  if (k === "state" && !Number.isFinite(o.stateThreshold))
    return "Set a threshold to classify observed interval values. These are sampled intervals, not exact transitions.";
  if (
    ["waterfall", "funnel", "radar"].includes(k) &&
    valid.length !== d.rows.length
  )
    return "Every displayed category needs a value. Missing values cannot be interpreted as zero.";
  if (k === "waterfall" && !o.contributions)
    return "Confirm that these rows are ordered, signed contributions to the same total.";
  if (k === "funnel" && !o.stageTotals)
    return "Confirm an ordered list of stage totals. This does not infer cohort conversion.";
  if (
    [
      "pie",
      "donut",
      "treemap",
      "sunburst",
      "pareto",
      "sankey",
      "stacked_bar",
      "stacked_area",
    ].includes(k) &&
    (!d.additive || !d.partition)
  )
    return "Choose disjoint dimension groups of an additive metric. Rates, averages and independent queries are not additive parts.";
  if (
    ["pie", "donut", "treemap", "sunburst", "pareto", "sankey"].includes(k) &&
    (!d.completeGroups || valid.length !== d.rows.length)
  )
    return "Composition needs the complete selected partition with no missing group values. Use dimension filters or a complete Top + Other result.";
  if (
    [
      "pie",
      "donut",
      "treemap",
      "sunburst",
      "pareto",
      "sankey",
      "stacked_bar",
      "stacked_area",
      "funnel",
    ].includes(k) &&
    valid.some((r) => Number(r.value) < 0)
  )
    return "This visualization requires nonnegative values.";
  if (
    ["pie", "donut", "treemap", "sunburst", "pareto", "sankey"].includes(k) &&
    !valid.some((r) => Number(r.value) > 0)
  )
    return "Composition needs a positive total.";
  if (
    ["matrix", "sunburst", "sankey"].includes(k) &&
    Object.keys(d.rows[0]?.dimensions ?? {}).length !== 2
  )
    return "Group by exactly two dimensions in the intended hierarchy or source → target order.";
  if (
    ["scatter", "bubble"].includes(k) &&
    d.sources?.some((s) => s.partition) !== d.sources?.every((s) => s.partition)
  )
    return "Use the same dimension grouping in each relationship query.";
  if (
    ["scatter", "bubble"].includes(k) &&
    (d.sources?.length ?? current.length) !== (k === "bubble" ? 3 : 2)
  )
    return `Choose ${k === "bubble" ? 3 : 2} query series, aligned by the same time or dimension keys.`;
  if (k === "radar" && (valid.length < 3 || valid.length > 12))
    return "Choose 3–12 measured categories for the radar.";
  if (
    ["dumbbell", "slope"].includes(k) &&
    !valid.some((r) => r.previous !== undefined && r.previous !== null)
  )
    return "Enable a comparison period with matching dimension groups.";
  if (k === "calendar" && d.interval !== "day")
    return "Choose daily intervals for the calendar heatmap.";
  if (
    !["scatter", "bubble", "small_multiples", "table"].includes(k) &&
    new Set(current.map((s) => s.unit)).size > 1
  )
    return "These queries have different units. Use small multiples or separate axes in Scatter / Bubble.";
  if (
    ["bullet", "gauge", "calendar", "histogram", "box"].includes(k) &&
    current.length !== 1
  )
    return "Choose one series for this visualization.";
  if (k === "bump" && d.series.some((s) => s.comparison))
    return "Disable period comparison for ranking over time. Use Dumbbell or Slope to compare periods.";
  if (
    ["stacked_bar", "stacked_area"].includes(k) &&
    d.series.some((s) => s.comparison)
  )
    return "Disable period comparison for stacked composition, or choose grouped bars / lines.";
  if (k === "pareto" && d.rows.length > 50)
    return "Use a Top 5/10 selection or filter to at most 50 groups for Pareto.";
  if (
    ["stacked_bar", "stacked_area"].includes(k) &&
    current.some((s) =>
      s.points.some((p) => p.value !== null && Number(p.value) < 0),
    )
  )
    return "Stacked composition requires nonnegative interval values.";
}
export function compact(value: string | number | null) {
  return value === null
    ? "—"
    : new Intl.NumberFormat("en", {
        maximumFractionDigits: 2,
        notation: "compact",
      }).format(Number(value));
}
export function seriesColor(id: string) {
  if (id === "[]") return "var(--chart-1)";
  if (id === "[]:previous") return "var(--chart-2)";
  let hash = 0;
  for (const c of id) hash = (hash * 31 + c.charCodeAt(0)) >>> 0;
  return `var(--chart-${(hash % 4) + 1})`;
}

export function observationLabel(value:ObservationMetadata,timezone:string){
 const format=(at:string)=>new Intl.DateTimeFormat('en',{timeZone:timezone,year:'numeric',month:'short',day:'numeric',hour:'2-digit',minute:'2-digit'}).format(new Date(at));
 const parts:string[]=[];
 if(value.observedAt){
  const range=value.oldestObservedAt&&value.oldestObservedAt!==value.observedAt;
  parts.push(range?`Readings: ${format(value.oldestObservedAt!)} – ${format(value.observedAt)}`:`Last observed: ${format(value.observedAt)}`);
 }
 if(value.expectedCount!==undefined)parts.push(`${value.knownCount??0}/${value.expectedCount} series known`);
 if(value.staleCount)parts.push(`${value.staleCount} stale`);
 return parts.join(' · ');
}
