// What the charts of the gallery show. The numbers are made up, and made
// the same way every time: a page drawn when the gallery is built and
// drawn again in the browser has to draw the same thing.

export type Point = [time: number, value: number];

// 29 September 2026, 17:00 UTC, in seconds.
export const NOW = Date.UTC(2026, 8, 29, 17, 0, 0) / 1000;

function random(seed: number) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

// A series that wanders around a base, with a peak now and then.
export function wander(
  seed: number,
  { points = 60, step = 60, base = 100, swing = 0.3, peaks = 0.08, peak = 2.5, floor = 0 } = {},
): Point[] {
  const next = random(seed);
  let level = base;
  return Array.from({ length: points }, (_, i): Point => {
    level += (next() - 0.5) * base * swing * 0.5;
    level = Math.max(base * (1 - swing), Math.min(base * (1 + swing), level));
    const value = next() < peaks ? level * (1 + next() * (peak - 1)) : level;
    return [NOW - (points - i) * step, Math.max(floor, Math.round(value * 100) / 100)];
  });
}

// The same times, each value a share more than that of another series.
export function over(under: Point[], seed: number, least = 1.2, most = 2.2): Point[] {
  const next = random(seed);
  return under.map(([time, value]): Point => [
    time,
    Math.round(value * (least + next() * (most - least)) * 100) / 100,
  ]);
}

// Steps: a count that changes a few times.
export function steps(values: [after: number, value: number][], points = 60, step = 60): Point[] {
  return Array.from({ length: points }, (_, i): Point => {
    const value = values.filter(([after]) => i >= after).at(-1)?.[1] ?? 0;
    return [NOW - (points - i) * step, value];
  });
}

// Moments, some of them with more than one thing in them.
export function moments(seed: number, share: number, points = 60, step = 60, most = 1) {
  const next = random(seed);
  return Array.from({ length: points }, (_, i) => i)
    .filter(() => next() < share)
    .map((i) => ({
      time: NOW - (points - i) * step + step / 2,
      count: 1 + Math.floor(next() * most),
    }));
}

export const FROM = NOW - 60 * 60;

export const releases = [
  { time: NOW - 41 * 60, label: "v11" },
  { time: NOW - 12 * 60, label: "v12" },
];

const median = wander(11, { base: 38, swing: 0.35, peaks: 0.05, peak: 1.8 });
const p95 = over(median, 12, 2.2, 5);
const p99 = over(p95, 13, 1.1, 1.9);
const highest = over(p99, 14, 1.05, 1.8);

export const responseTime = [
  { name: "Max", points: highest },
  { name: "99th percentile", points: p99 },
  { name: "95th percentile", points: p95 },
  { name: "Median", points: median },
];

export const throughput = [
  { name: "2xx", tone: "success" as const, points: wander(21, { base: 42, swing: 0.4 }) },
  { name: "3xx", tone: "info" as const, points: wander(22, { base: 6, swing: 0.5 }) },
  { name: "4xx", tone: "warning" as const, points: wander(23, { base: 3, swing: 0.6, peaks: 0.1 }) },
  { name: "5xx", tone: "error" as const, points: wander(24, { base: 0.6, swing: 0.9, peaks: 0.12, peak: 6 }) },
];

const MiB = 1 << 20;
export const memory = [
  { name: "web", points: wander(31, { base: 340 * MiB, swing: 0.18, peaks: 0.04, peak: 1.4 }) },
  { name: "worker", points: wander(32, { base: 190 * MiB, swing: 0.25 }) },
];
export const memoryReferences = [
  { value: 512 * MiB, label: "Allocated", tone: "error" as const },
];

export const cpu = [
  { name: "web", points: wander(41, { base: 46, swing: 0.5, peaks: 0.06, peak: 1.8 }) },
  { name: "worker", points: wander(42, { base: 22, swing: 0.6 }) },
];

export const instances = [
  { name: "web", points: steps([[0, 2], [19, 3], [48, 4]]) },
  { name: "worker", points: steps([[0, 1], [30, 2], [52, 1]]) },
];

export const events = [
  { label: "Failed", tone: "error" as const, events: moments(51, 0.12, 60, 60, 4) },
  { label: "Restart", tone: "warning" as const, events: moments(52, 0.05) },
  {
    label: "Release",
    tone: "info" as const,
    events: releases.map((r) => ({ time: r.time, title: `The release ${r.label} went out` })),
  },
  { label: "Scale", tone: "neutral" as const, events: moments(54, 0.04) },
];

export const trend = wander(61, { points: 24, base: 210, swing: 0.4 }).map((p) => p[1]);
export const trendOfRequests = wander(62, { points: 24, base: 48, swing: 0.5 }).map((p) => p[1]);
export const trendOfErrors = wander(63, { points: 24, base: 2, swing: 0.9, peaks: 0.2, peak: 5 }).map(
  (p) => p[1],
);
