import React from "react";
import { renderToStaticMarkup as renderMarkup } from "react-dom/server";
import { it, expect } from "vitest";
import { MetricChart } from "./metric-chart";
import {
  chartKinds,
  chartIssue,
  type ChartOptions,
  type PlotData,
} from "../lib/metric-chart";
// Ignore the table-toggle icon when asserting chart geometry.
const renderToStaticMarkup = (element: React.ReactNode) => renderMarkup(element).replace(/<svg\b[^>]*class="[^"]*lucide[^>]*>[\s\S]*?<\/svg>/g, "");
function fixture(): PlotData {
  return {
    measurementKind: "latest",
    title: "Chart acceptance",
    unit: "USD",
    timezone: "UTC",
    interval: "day",
    startsAt: "2026-01-01T00:00:00Z",
    endsAt: "2026-01-03T00:00:00Z",
    series: [1, 2, 3].map((i) => ({
      id: String(i),
      name: "Series " + i,
      unit: "USD",
      total: String(i * 30),
      dimensions: { channel: String(i) },
      points: [
        {
          key: "2026-01-01T00:00:00Z",
          value: String(i * 10),
          quality: "complete",
        },
        {
          key: "2026-01-02T00:00:00Z",
          value: String(i * 20),
          quality: "complete",
        },
      ],
    })),
    rows: [1, 2, 3].map((i) => ({
      key: String(i),
      label: "Series " + i,
      value: String(i * 30),
      previous: String(i * 25),
      dimensions: { channel: String(i) },
    })),
    additive: true,
    partition: true,
    completeGroups: true,
    notes: [],
  };
}
for (const kind of chartKinds)
  it(`renders ${kind} with eligible inputs and finite geometry`, () => {
    const d = fixture(),
      options: ChartOptions = {
        kind,
        min: 0,
        max: 100,
        target: 75,
        contributions: true,
        stageTotals: true,
        stateThreshold: 20,
      };
    if (["bullet", "gauge", "histogram", "box", "calendar"].includes(kind))
      d.series = d.series.slice(0, 1);
    if (kind === "scatter") d.series = d.series.slice(0, 2);
    if (["sunburst", "matrix", "sankey"].includes(kind))
      d.rows = d.rows.map((r, i) => ({
        ...r,
        dimensions: { from: i < 2 ? "A" : "B", to: String(i) },
      }));
    if (["box", "histogram"].includes(kind))
      d.distribution = {
        count: 3,
        min: "10",
        q1: "10",
        median: "20",
        q3: "30",
        max: "30",
        bins: [
          { from: "10", to: "20", count: 1 },
          { from: "20", to: "30", count: 2 },
        ],
      };
    expect(chartIssue(d, options)).toBeUndefined();
    const html = renderToStaticMarkup(
      <MetricChart data={d} options={options} />,
    );
    expect(html).not.toMatch(/NaN|Infinity/);
    expect(html).toContain("Chart acceptance");
    expect(html).not.toContain('role="status"');
  });
it("line geometry breaks across unavailable intervals", () => {
  const d = fixture();
  d.series = [
    {
      ...d.series[0],
      points: [
        d.series[0].points[0],
        { key: "2026-01-02T00:00:00Z", value: null, quality: "missing" },
        { key: "2026-01-03T00:00:00Z", value: "20", quality: "complete" },
      ],
    },
  ];
  const html = renderToStaticMarkup(
    <MetricChart data={d} options={{ kind: "line" }} />,
  );
  expect(html).not.toContain("<path");
});
it("preserves a dashed projection connector from validated evidence", () => {
  const d = fixture();
  d.series = [
    d.series[0],
    {
      ...d.series[1],
      projection: true,
      anchor: { key: d.series[0].points[1].key, value: "20" },
      points: [
        { key: "2026-01-03T00:00:00Z", value: "30", quality: "projection" },
      ],
    },
  ];
  const html = renderToStaticMarkup(
    <MetricChart data={d} options={{ kind: "line" }} />,
  );
  expect(html).toContain('data-projection-connector="true"');
  expect(html).toContain('stroke-dasharray="5 4"');
});
it("renders an explanation instead of crashing on incompatible chart data", () => {
  for (const kind of chartKinds) {
    const d = fixture();
    d.partition = false;
    d.rows = d.rows.map((r) => ({ ...r, dimensions: {} }));
    expect(() =>
      renderToStaticMarkup(<MetricChart data={d} options={{ kind }} />),
    ).not.toThrow();
  }
});

it.each(["line", "bar"] as const)(
  "shows isolated readings in a %s chart with more than 80 hourly buckets",
  (kind) => {
    const data = fixture();
    data.interval = "hour";
    data.timezone = "America/Sao_Paulo";
    data.series = [{
      ...data.series[0],
      points: Array.from({ length: 200 }, (_, index) => ({
        key: new Date(Date.UTC(2026, 9, 8, index)).toISOString(),
        value: index === 13 ? "125000.50" : null,
        quality: index === 13 ? "complete" : "missing",
      })),
    }];
    const html = renderToStaticMarkup(<MetricChart data={data} options={{ kind }} />);
    expect(html).toContain(kind === "line" ? "<circle" : "<rect");
    expect(html).not.toContain("<path");
    expect(html).toContain("125000.50");
    // Midnight UTC is 21:00 on the previous reporting day in São Paulo.
    expect(html).toContain("21:00</tspan>");
  },
);

it('shows original observation provenance and dashed stale segments for kept values',()=>{
 const d=fixture();d.series=d.series.slice(0,1);
 d.series[0].points.forEach(p=>Object.assign(p,{quality:'stale',observedAt:'2025-12-31T10:00:00Z',oldestObservedAt:'2025-12-31T10:00:00Z',knownCount:1,expectedCount:1,staleCount:1}));
 const html=renderToStaticMarkup(<MetricChart data={d} options={{kind:'step'}}/>);
 expect(html).toContain('Last observed:');expect(html).toContain('2025');expect(html).toContain('1 stale');expect(html).toContain('stroke-dasharray="5 4"');
});

it("the initial legend uses the latest interval, never a period total or an earlier available sample", () => {
  const d=fixture();
  d.series=d.series.slice(0,1);
  d.series[0].points[1].value=null;
  const html=renderToStaticMarkup(<MetricChart data={d} options={{kind:"line"}}/>);
  const legend=html.split('aria-label="Chart legend"')[1].split('</ul>')[0];
  expect(legend).toContain('Unavailable');
  expect(legend).not.toContain('<strong>10</strong>');
  expect(legend).not.toContain('<strong>30</strong>');
});
it("negative time-series values have finite axes and preserve the zero baseline", () => {
  const d=fixture();d.series[0].points[0].value='-25';
  const html=renderToStaticMarkup(<MetricChart data={d} options={{kind:"bar"}}/>);
  expect(html).not.toMatch(/NaN|Infinity/);
  expect(html).toMatch(/>-\d+(?:\.\d+)?<\/text>/);
  expect(html).toContain('>0</text>');
  const zero=html.match(/<line [^>]*y1="([^"]+)"[^>]*><\/line><text[^>]*>0<\/text>/);
  const negativeBar=html.match(/<rect[^>]*y="([^"]+)"[^>]*height="([^"]+)"/);
  expect(Number(negativeBar?.[1])).toBe(Number(zero?.[1]));
  expect(Number(negativeBar?.[2])).toBeGreaterThan(0);
});

it.each(["vertical", "horizontal"] as const)("renders %s stages including zero without fabricating a final drop", orientation => {
  const d = fixture();
  d.rows[0].value = "0";
  const html = renderToStaticMarkup(<MetricChart data={d} options={{kind:"funnel",stageTotals:true,orientation}} />);
  expect(html).toContain(`metric-funnel-${orientation}`);
  expect(html).toContain("First stage is zero");
  expect(html).not.toMatch(/(?:NaN|Infinity)/);
  expect((html.match(/<path /g) ?? []).length).toBe(3);
});
it("aligns sampled state intervals and preserves absent samples as missing", () => {
  const d = fixture(); d.series[0].points.splice(0,1);
  const html = renderToStaticMarkup(<MetricChart data={d} options={{kind:"state",stateThreshold:20}} />);
  expect((html.match(/class="metric-state-cell"/g) ?? []).length).toBe(6);
  expect((html.match(/data-missing="true"/g) ?? []).length).toBe(1);
  expect(html).toContain("Unavailable USD · Missing");
});

it("keeps a missing comparison endpoint absent in a dumbbell", () => {
  const d = fixture(); d.rows[0].previous = null;
  const html = renderToStaticMarkup(<MetricChart data={d} options={{kind:"dumbbell"}} />);
  expect(html).toContain("previous Unavailable, current 30 USD");
  expect((html.match(/class="metric-dumbbell-line"/g) ?? []).length).toBe(2);
  expect((html.match(/class="metric-dumbbell-point/g) ?? []).length).toBe(5);
});
it("does not invent a numeric matrix range when all cells are missing", () => {
  const d = fixture();
  d.rows = d.rows.map(r=>({...r,value:null,dimensions:{channel:r.label,region:"North"}}));
  const html = renderToStaticMarkup(<MetricChart data={d} options={{kind:"matrix"}} />);
  expect(html).toContain("No measured cells");
  expect(html).not.toMatch(/(?:NaN|Infinity)/);
});


it("classifies state values with zero and missing critical, below-limit warning and at-limit normal", () => {
  const d = fixture(); d.series = d.series.slice(0, 1);
  d.series[0].points = ["1", "0.5", "0.00", null, "2"].map((value, i) => ({ key: String(i), value, quality: value === null ? "missing" : "complete" }));
  const html = renderToStaticMarkup(<MetricChart data={d} options={{kind:"state",stateThreshold:1}} />);
  const cells = html.match(/<span class="metric-state-cell"[^>]+>/g) ?? [];
  expect(cells).toHaveLength(5);
  ["success", "warning", "error", "error", "success"].forEach((tone, i) => expect(cells[i]).toContain(`border-color:var(--chart-${tone})`));
  expect(cells[2]).toContain("0.00 USD · Zero");
  expect(cells[3]).toContain("Unavailable USD · Missing");
  expect(cells[3]).toContain('data-missing="true"');
  expect(d.series[0].points[3].value).toBeNull();
});
it("zero remains critical even when the state threshold is zero", () => {
  const d = fixture(); d.series[0].points[0].value = "0";
  const html = renderToStaticMarkup(<MetricChart data={d} options={{kind:"state",stateThreshold:0}} />);
  expect(html).toMatch(/class="metric-state-cell" data-state="critical"[^>]*border-color:var\(--chart-error\)/);
});
it("renders all-missing known state intervals without fabricating observations", () => {
  const d = fixture();
  d.rows = d.rows.map(r => ({...r, value: null}));
  d.series = d.series.map(s => ({...s, total: null, points: s.points.map(p => ({...p, value: null, quality: "missing"}))}));
  expect(chartIssue(d, {kind:"state",stateThreshold:1})).toBeUndefined();
  const html = renderToStaticMarkup(<MetricChart data={d} options={{kind:"state",stateThreshold:1}} />);
  expect((html.match(/data-state="critical" data-missing="true"/g) ?? []).length).toBe(6);
  d.series = [];
  expect(chartIssue(d, {kind:"state",stateThreshold:1})).toBe("No eligible measurements for this period.");
});

