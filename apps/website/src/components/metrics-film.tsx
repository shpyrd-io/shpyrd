"use client";

import { RotateCcw, TriangleAlert } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { filmEnter, filmInset, filmPiece, useFilm } from "@/components/film";

// The film of the feature "See that it is healthy" (Giovani, 2026-10-19): the
// app's response time drawing itself over a day. Release v42 is marked on the
// chart and the line jumps; a warning says so; the rollback to v41 is marked
// and the line comes back down. At rest the whole day is there, both marks
// on it.

// An illustration, not a record (Giovani, 2026-10-20): a smooth curve, calm
// with gentle waves, rising softly where v42 went out and easing back down
// after the rollback; no sharp corners. In the chart's 100×50 box. After a
// reference Giovani gave (a glass dashboard's revenue chart, 2026-10-20): a
// broad soft hill rather than a step, a few slow waves, a fuller fading area,
// faint dashed guides, and a point on the peak with its value.
const V42 = 40;
const V41 = 67;
const ease = (x: number) => 1 / (1 + Math.exp(-x));
const value = (t: number) => {
  const slow = ease((t - V42 - 5) / 3.6) - ease((t - V41 - 4) / 3.6);
  const wave = 1.8 * Math.sin(t * 0.16) + 0.9 * Math.sin(t * 0.37 + 1);
  return 40 - slow * 30 - wave * (1 - 0.3 * slow);
};
const samples = Array.from({ length: 41 }, (_, k) => {
  const x = k * 2.5;
  return [x, value(x)] as const;
});
// Through the samples, as a smooth curve (Catmull-Rom, as cubic Béziers).
const curve = samples
  .map(([x, y], k) => {
    if (k === 0) return `M ${x} ${y}`;
    const [x0, y0] = samples[Math.max(0, k - 2)];
    const [x1, y1] = samples[k - 1];
    const [x3, y3] = samples[Math.min(samples.length - 1, k + 1)];
    const c1 = `${x1 + (x - x0) / 6} ${y1 + (y - y0) / 6}`;
    const c2 = `${x - (x3 - x1) / 6} ${y - (y3 - y1) / 6}`;
    return `C ${c1}, ${c2}, ${x} ${y}`;
  })
  .join(" ");
const area = `${curve} L 100 50 L 0 50 Z`;
// The peak, where the point and its value sit while it is slow.
const PEAK = samples.reduce((a, b) => (b[1] < a[1] ? b : a));
const at = (x: number) => x;

const MOMENTS = [
  ["calm", 1400],
  ["v42", 1600],
  ["slow", 1600],
  ["rollback", 1400],
  ["calm-again", 1400],
  ["rest", 2600],
] as const;
// How far the line is drawn at each moment.
const drawn = { calm: 0.36, v42: 0.5, slow: 0.62, rollback: 0.76, "calm-again": 1, rest: 1 } as const;

export function MetricsFilm() {
  const { root, name, past, round } = useFilm(MOMENTS);
  const share = drawn[name];
  const warn = name === "slow";

  return (
    <div ref={root} className={filmInset}>
      <div key={round} className={cn(filmPiece, "flex size-full flex-col gap-2.5 px-4 py-3.5")}>
        <div className="flex items-center justify-between">
          <span className="font-medium text-foreground">Response time · yourapp.com</span>
          <span className="flex gap-1 text-[10px] text-muted-foreground">
            <span className="rounded px-1.5 py-0.5">1h</span>
            <span className="rounded bg-primary px-1.5 py-0.5 text-white">24h</span>
            <span className="rounded px-1.5 py-0.5">7d</span>
          </span>
        </div>

        <div className="relative min-h-20 flex-1">
          <svg viewBox="0 0 100 50" preserveAspectRatio="none" aria-hidden="true" className="absolute inset-0 size-full overflow-visible">
            {[0, 1, 2].map((g) => (
              <line key={g} x1="0" x2="100" y1={8 + g * 19} y2={8 + g * 19} vectorEffect="non-scaling-stroke" className="stroke-foreground/10" strokeWidth={1} strokeDasharray="2 4" />
            ))}
            {/* The releases, marked where they went out. */}
            {past("v42") && <line x1={at(V42)} x2={at(V42)} y1="2" y2="48" vectorEffect="non-scaling-stroke" className="stroke-primary/60" strokeWidth={1} strokeDasharray="3 3" />}
            {past("rollback") && <line x1={at(V41)} x2={at(V41)} y1="2" y2="48" vectorEffect="non-scaling-stroke" className="stroke-success/70" strokeWidth={1} strokeDasharray="3 3" />}
            {/* The curve in orange, a soft orange under it, drawn as far as
                the film has gone. */}
            <defs>
              <linearGradient id="metrics-area" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0" className="[stop-color:var(--primary)]" stopOpacity={0.32} />
                <stop offset="0.85" className="[stop-color:var(--primary)]" stopOpacity={0} />
              </linearGradient>
            </defs>
            <g className="transition-[clip-path] duration-1000 ease-linear" style={{ clipPath: `inset(0 ${100 - share * 100}% 0 0)` }}>
              <path d={area} fill="url(#metrics-area)" />
              <path d={curve} fill="none" vectorEffect="non-scaling-stroke" className="stroke-primary" strokeWidth={1.75} strokeLinecap="round" />
            </g>
          </svg>
          {/* The point on the peak and its value, while it is slow. */}
          {warn && (
            <span className={cn("absolute", filmEnter)} style={{ left: `${PEAK[0]}%`, top: `${PEAK[1] * 2}%` }}>
              <span className="absolute size-2 -translate-1/2 rounded-full bg-background ring-2 ring-primary" />
              <span className="absolute bottom-2 left-0 -translate-x-1/2 rounded-md bg-background px-1.5 py-0.5 text-[9.5px] font-medium whitespace-nowrap text-foreground shadow-[0_2px_6px_-2px_rgb(20_20_30/0.2)] ring-1 ring-foreground/8">
                2.4 s
              </span>
            </span>
          )}
          {past("v42") && (
            <span className={cn("absolute top-0 rounded bg-primary/10 px-1 font-medium text-[9.5px] text-primary", filmEnter)} style={{ left: `calc(${at(V42)}% + 4px)` }}>
              v42
            </span>
          )}
          {past("rollback") && (
            <span
              className={cn("absolute top-0 inline-flex items-center gap-0.5 rounded bg-success/12 px-1 font-medium text-[9.5px] text-success", filmEnter)}
              style={{ left: `calc(${at(V41)}% + 4px)` }}
            >
              <RotateCcw className="size-2.5" />
              v41
            </span>
          )}
        </div>

        <div className="flex h-6 items-center justify-between text-[10px] text-muted-foreground">
          <span>00:00</span>
          {warn ? (
            <span className={cn("inline-flex items-center gap-1 rounded-full bg-destructive/10 px-2 py-0.5 font-medium text-destructive", filmEnter)}>
              <TriangleAlert className="size-3" />
              p95 at 2.4 s since v42
            </span>
          ) : past("rollback") ? (
            <span className={cn("rounded-full bg-success/12 px-2 py-0.5 font-medium text-success", filmEnter)}>Back to 180 ms</span>
          ) : null}
          <span>now</span>
        </div>
      </div>
    </div>
  );
}
