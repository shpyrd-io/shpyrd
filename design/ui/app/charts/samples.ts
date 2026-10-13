import { chartNames, type ChartKind, type ChartOptions, type PlotData } from "../../src/lib/metric-chart";

// Fixed observations keep the server and browser render identical.
export function chartSample(kind: ChartKind): {data: PlotData; options: ChartOptions} {
  const names = ["Direct", "Search", "Referral", "Social"];
  const values = [84, 62, 43, 28];
  const series = names.map((name, i) => ({
    id: name, name, unit: "events", dimensions: {channel: name}, total: String(values[i]),
    points: Array.from({length: 14}, (_, day) => ({
      key: `2026-09-${String(day + 1).padStart(2, "0")}T00:00:00Z`,
      value: String(12 + ((day * (i + 3) + i * 11) % 70)), quality: "complete",
    })),
  }));
  const data: PlotData = {
    title: chartNames[kind], unit: "events", timezone: "UTC", interval: "day",
    startsAt: "2026-09-01T00:00:00Z", endsAt: "2026-09-15T00:00:00Z",
    quality: "complete", series,
    rows: names.map((name, i) => ({key: name, label: name, dimensions: {channel: name}, value: String(values[i]), previous: String(values[i] - 14 + i * 7)})),
    additive: true, partition: true, completeGroups: true,
    notes: ["Illustrative observations. Missing values are never silently replaced with zero."],
  };
  const options: ChartOptions = {kind, min: 0, max: 100, target: 75, contributions: true, stageTotals: true, stateThreshold: 40};
  if (["bullet", "gauge", "calendar", "histogram", "box"].includes(kind)) data.series = series.slice(0, 1);
  if (kind === "scatter") data.series = series.slice(0, 2);
  if (kind === "bubble") data.series = series.slice(0, 3);
  if (["matrix", "sunburst", "sankey"].includes(kind)) {
    data.dimensionOrder = ["channel", "plan"];
    data.rows = names.flatMap((name, i) => ["Pro", "Team"].map((plan, j) => ({key: `${name}-${plan}`, label: `${name} / ${plan}`, dimensions: {channel: name, plan}, value: String(values[i] - j * 12), previous: "30"})));
  }
  if (kind === "waterfall") data.rows = [120, -25, 40, -15].map((v, i) => ({key: String(i), label: ["Opening", "Churn", "Expansion", "Discounts"][i], dimensions: {}, value: String(v)}));
  if (kind === "state") {
    data.measurementKind = "latest";
    data.series = series.map(s => ({...s, points: s.points.map((p, i) => ({...p, value: i === 5 ? null : i === 9 ? "0" : p.value, quality: i === 5 ? "missing" : "complete"}))}));
  }
  if (["histogram", "box"].includes(kind)) data.distribution = {count: 100, min: "0", q1: "20", median: "40", q3: "60", max: "100", bins: [8, 17, 35, 26, 14].map((count, i) => ({from: String(i * 20), to: String((i + 1) * 20), count}))};
  return {data, options};
}
