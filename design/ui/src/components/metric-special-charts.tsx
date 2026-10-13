"use client";
import { useId, type CSSProperties } from "react";
import { ProgressBar } from "./progress-bar";
import { compact, type ChartOptions, type PlotData, type PlotSeries } from "../lib/metric-chart";
import { observationLabel } from "../lib/metric-chart";

const wash = (ink: string) => `color-mix(in oklch, ${ink} 25%, var(--background))`;
const rowLabel = (r: PlotData["rows"][number]) => (Object.values(r.dimensions).join(" / ") || r.label).replace(/^\d+ · /, "");

export function MetricFunnel({ data, orientation = "vertical", color }: {
  data: PlotData; orientation?: ChartOptions["orientation"]; color: (key: string) => string;
}) {
  const gradientId = useId();
  const rows = data.rows, max = (Math.max(...rows.map(r => Number(r.value))) || 1), first = Number(rows[0]?.value);
  return <div className={`metric-funnel metric-funnel-${orientation}`}>
    <ol style={{ "--stages": rows.length } as CSSProperties} aria-label="Ordered stage totals">
      {rows.map((r, i) => {
        const current = Number(r.value) / max, next = Number(rows[i + 1]?.value ?? r.value) / max;
        // Width (vertical) or height (horizontal) encodes the measured stage total.
        // The final stage stays flat; no unobserved terminal value is invented.
        const path = orientation === "horizontal"
          ? `M1,${160 - current * 158} C50,${160 - current * 158} 50,${160 - next * 158} 99,${160 - next * 158} L99,160 L1,160 Z`
          : `M${150 - current * 148},1 L${150 + current * 148},1 C${150 + current * 148},29 ${150 + next * 148},29 ${150 + next * 148},57 L${150 - next * 148},57 C${150 - next * 148},29 ${150 - current * 148},29 ${150 - current * 148},1 Z`;
        return <li key={r.key}>
          <div className="metric-funnel-label">
            <small>{first > 0 ? `${(Number(r.value) / first * 100).toFixed(1)}%` : "First stage is zero"}</small>
            <span title={r.label}>{rowLabel(r)}</span>
            <strong title={`${r.value} ${data.unit}`}>{compact(r.value)}</strong>
          </div>
          <svg viewBox={orientation === "horizontal" ? "0 0 100 162" : "0 0 300 58"} preserveAspectRatio="none" role="img" aria-label={`${rowLabel(r)}: ${r.value} ${data.unit}`}>
            <defs><linearGradient id={`${gradientId}-${i}`} x1="0" y1="0" x2={orientation === "horizontal" ? "1" : "0"} y2={orientation === "horizontal" ? "0" : "1"}><stop offset="0%" stopColor={color(r.key)} stopOpacity={.16} /><stop offset="100%" stopColor={color(r.key)} stopOpacity={.36} /></linearGradient></defs>
            <path d={path} fill={`url(#${gradientId}-${i})`} stroke={color(r.key)} strokeWidth={1} vectorEffect="non-scaling-stroke" />
          </svg>
        </li>;
      })}
    </ol>
  </div>;
}

export function MetricBullet({ data, options }: { data: PlotData; options: ChartOptions }) {
  const s = data.series.find(s => !s.comparison)!, min = options.min!, max = options.max!, target = options.target!;
  return <div className="metric-bullet">
    <div className="metric-bullet-value"><strong title={s.total!}>{compact(s.total)}</strong><span>{data.unit}</span></div>
    <p>Target <strong>{compact(target)} {data.unit}</strong></p>
    <div className="metric-bullet-track">
      <ProgressBar label={s.name} value={Number(s.total) - min} max={max - min} format={v => compact(v + min)} />
      <span className="metric-bullet-target" style={{ left: `${(target - min) / (max - min) * 100}%` }} title={`Target: ${target} ${data.unit}`} aria-hidden="true" />
    </div>
    <div className="metric-scale"><span>{compact(min)}</span><span>{compact(max)}</span></div>
  </div>;
}

export function MetricGauge({ data, options }: { data: PlotData; options: ChartOptions }) {
  const s = data.series.find(s => !s.comparison)!, min = options.min!, max = options.max!;
  const fraction = (v: number) => Math.max(0, Math.min(1, (v - min) / (max - min)));
  const point = (f: number, r: number) => [200 + Math.cos(Math.PI + f * Math.PI) * r, 176 + Math.sin(Math.PI + f * Math.PI) * r];
  const ring = (f: number) => `M${point(0,156)} A156,156 0 0 1 ${point(f,156)} L${point(f,138)} A138,138 0 0 0 ${point(0,138)} Z`;
  const at = fraction(Number(s.total)), goal = fraction(options.target!);
  return <svg className="metric-gauge" viewBox="0 0 400 224" role="img" aria-label={`${s.total} ${data.unit}. Target ${options.target}. Scale ${min} to ${max}.`}>
    <path d={ring(1)} fill="none" stroke="var(--border)" vectorEffect="non-scaling-stroke" />
    {at > 0 && <path d={ring(at)} fill="var(--chart-1)" fillOpacity={0.25} stroke="var(--chart-1)" vectorEffect="non-scaling-stroke" />}
    <line x1={point(goal,133)[0]} y1={point(goal,133)[1]} x2={point(goal,161)[0]} y2={point(goal,161)[1]} stroke="var(--foreground)" strokeWidth={1.5} vectorEffect="non-scaling-stroke"><title>{["Target: ", options.target].join("").trim()}</title></line>
    <text className="metric-gauge-value" x={200} y={145} textAnchor="middle">{compact(s.total)}</text>
    <text x={200} y={168} textAnchor="middle">{data.unit}</text>
    <text x={44} y={200}>{compact(min)}</text><text x={356} y={200} textAnchor="end">{compact(max)}</text>
    <text x={200} y={220} textAnchor="middle">Target {compact(options.target!)}</text>
  </svg>;
}

export function MetricState({ data, series, threshold }: { data: PlotData; series: PlotSeries[]; threshold: number }) {
  const keys = [...new Set(series.flatMap(s => s.points.map(p => p.key)))].sort();
  const tones = { normal: "var(--chart-success)", warning: "var(--chart-warning)", critical: "var(--chart-error)" };
  const legend = [
    { label: `≥ ${threshold}`, ink: tones.normal },
    { label: `< ${threshold}, ≠ 0`, ink: tones.warning },
    { label: "0", ink: tones.critical },
    { label: "Missing", ink: tones.critical, missing: true },
  ];
  return <div className="metric-state">
    <div className="metric-state-scroll">
      <div className="metric-state-rows" style={{ "--points": keys.length, minWidth: `${120 + keys.length * 8}px` } as CSSProperties}>
        {series.map(s => {
          const points = new Map(s.points.map(p => [p.key, p]));
          return <div className="metric-state-row" key={s.id}>
            <span className="metric-state-name" title={s.name}>{Object.values(s.dimensions).join(" / ") || s.name}</span>
            <div className="metric-state-cells" aria-label={s.name} role="group">
              {keys.map(key => {
                const p = points.get(key), missing = p?.value == null, zero = !missing && Number(p.value) === 0;
                const state = missing || zero ? "critical" : Number(p.value) >= threshold ? "normal" : "warning";
                const ink = tones[state];
                const title = `${key} · ${p?.value ?? "Unavailable"} ${data.unit} · ${missing ? "Missing" : zero ? "Zero" : state === "normal" ? "At or above threshold" : "Below threshold"} · ${observationLabel(p ?? {}, data.timezone)}`;
                return <span key={key} className="metric-state-cell" data-state={state} data-missing={missing || undefined} style={{ borderColor: ink, background: wash(ink) }} title={title} role="img" aria-label={title} />;
              })}
            </div>
          </div>;
        })}
      </div>
    </div>
    <div className="metric-state-key">
      {legend.map(({ label, ink, missing }) => <span key={label}><i style={{ borderColor: ink, background: wash(ink), borderStyle: missing ? "dashed" : "solid" }} />{label}</span>)}
    </div>
  </div>;
}


export function MetricDumbbell({ data, color }: { data: PlotData; color: (key: string) => string }) {
  const rows = data.rows.slice(0, 50);
  const values = rows.flatMap(r => [r.value, r.previous]).filter(v => v != null).map(Number);
  const low = Math.min(0, ...values), high = Math.max(0, ...values);
  const position = (v: string) => (Number(v) - low) / (high - low || 1) * 100;
  return <div className="metric-dumbbell">
    {rows.map(r => <div className="metric-dumbbell-row" key={r.key}>
      <span className="metric-dumbbell-label" title={r.label}>{rowLabel(r)}</span>
      <div className="metric-dumbbell-track" role="img" aria-label={`${r.label}: previous ${r.previous ?? "Unavailable"}, current ${r.value ?? "Unavailable"} ${data.unit}`} style={{"--ink": color(r.key)} as CSSProperties}>
        <span className="metric-dumbbell-rail" />
        {r.value != null && r.previous != null && <span className="metric-dumbbell-line" style={{left:`${Math.min(position(r.value),position(r.previous))}%`,width:`${Math.abs(position(r.value)-position(r.previous))}%`}} />}
        {r.previous != null && <span className="metric-dumbbell-point" style={{left:`${position(r.previous)}%`}} />}
        {r.value != null && <span className="metric-dumbbell-point metric-dumbbell-current" style={{left:`${position(r.value)}%`}} />}
      </div>
      <span className="metric-dumbbell-value" title={`${r.value ?? "Unavailable"} ${data.unit}`}>{compact(r.value)}</span>
    </div>)}
    <div className="metric-dumbbell-axis"><span>{compact(low)}</span><span>{compact(high)}</span></div>
  </div>;
}
