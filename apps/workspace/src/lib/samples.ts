import type { LogLine } from "@shpyrd/ui/components/log-view";
import type { MetricChart, Metrics, MetricsQuery, Point, Series } from "@/api/types";

// What the Mock shows in the charts and the logs. Made up, and made the
// same way every time.

function random(seed: number) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

function wander(seed: number, to: number, { points = 60, step = 60, base = 100, swing = 0.3, peaks = 0.08, peak = 2.5 } = {}): Point[] {
  const next = random(seed);
  let level = base;
  return Array.from({ length: points }, (_, i): Point => {
    level += (next() - 0.5) * base * swing * 0.5;
    level = Math.max(base * (1 - swing), Math.min(base * (1 + swing), level));
    const value = next() < peaks ? level * (1 + next() * (peak - 1)) : level;
    return [to - (points - i) * step, Math.round(value * 100) / 100];
  });
}

function over(under: Point[], seed: number, least: number, most: number): Point[] {
  const next = random(seed);
  return under.map(([t, v]): Point => [t, Math.round(v * (least + next() * (most - least)) * 100) / 100]);
}

function steps(values: [number, number][], to: number, points = 60, step = 60): Point[] {
  return Array.from({ length: points }, (_, i): Point => [to - (points - i) * step, values.filter(([after]) => i >= after).at(-1)?.[1] ?? 0]);
}

const MiB = 1 << 20;

// What one instance of a process was given: the CPU of its size, the
// ceiling it may burst to, and its memory. Every process of the Mock has
// the same size.
const allocation = { cpu: 1, burst: 1.75, memory: 512 * MiB };

// Sum, average or maximum across the instances of a process, named as the
// server names it: "sum web", "avg web in". A sum is measured against the
// sum of the allocations; the others against one instance's.
function collapse(list: Series[], agg: NonNullable<MetricsQuery["agg"]>, name: string): Series[] {
  if (agg === "none" || list.length === 0) return list;
  const sums = new Map<number, number>();
  const counts = new Map<number, number>();
  const maxes = new Map<number, number>();
  for (const s of list) {
    for (const [t, v] of s.points) {
      sums.set(t, (sums.get(t) ?? 0) + v);
      counts.set(t, (counts.get(t) ?? 0) + 1);
      maxes.set(t, Math.max(maxes.get(t) ?? -Infinity, v));
    }
  }
  const points = [...sums.keys()].sort((a, b) => a - b).map((t): Point => {
    const v = agg === "sum" ? sums.get(t)! : agg === "avg" ? sums.get(t)! / counts.get(t)! : maxes.get(t)!;
    return [t, Math.round(v * 100) / 100];
  });
  const scale = agg === "sum" ? list.length : 1;
  const first = list[0]!;
  return [{ name: `${agg} ${name}`, points, reference: first.reference && first.reference * scale, burst: first.burst && first.burst * scale }];
}

export function metricsOf(slug: string, replicas: Record<string, number>, query: MetricsQuery): Metrics {
  const { range, by = "process", agg = "none", mode = "percent", replaced = false } = query;
  const to = Math.floor(Date.now() / 1000 / 60) * 60;
  const step = range === "7d" ? 60 * 168 : range === "24h" ? 60 * 24 : range === "6h" ? 360 : range === "15m" ? 15 : 60;
  const seed = [...slug].reduce((h, c) => (h * 31 + c.charCodeAt(0)) >>> 0, 7);
  const first = Object.keys(replicas)[0] ?? "web";
  const processes = Object.keys(replicas).filter((p) => !query.process || p === query.process);
  const instances = by === "instance";
  const percent = mode === "percent";
  const median = wander(seed + 1, to, { step, base: 38, swing: 0.35, peaks: 0.05, peak: 1.8 });
  const p95 = over(median, seed + 2, 2.2, 5);
  const p99 = over(p95, seed + 3, 1.1, 1.9);
  const series = (name: string, i: number, base: number, swing: number, rest = {}): Series => ({ name, points: wander(seed + 10 + i, to, { step, base, swing, ...rest }) });

  // What a process uses: one series for the process, or one per instance
  // when asked, each a share of the whole and none the same. The first
  // process also had an instance replaced halfway through the window; it
  // is drawn when asked, named as the server names it — never by its pod.
  const usage = (p: string, i: number, perInstance: number, swing: number, rest: object, suffix = ""): Series[] => {
    const n = replicas[p] ?? 1;
    if (!instances) return [series(`${p}${suffix}`, i, perInstance * n, swing, rest)];
    const own = Array.from({ length: n }, (_, k) => series(`${p}.${k + 1}${suffix}`, i + 7 * (k + 1), perInstance * (0.7 + ((k * 2) % 3) * 0.3), swing, rest));
    if (replaced && p === processes[0]) {
      const gone = series(`replaced 1${suffix}`, i + 99, perInstance * 0.8, swing, rest);
      own.push({ ...gone, points: gone.points.slice(0, 30) });
    }
    return own;
  };

  // Against what the process was given: one instance's allocation for an
  // instance series, every instance's for a process series, which is the
  // sum of them. As a share, divided by it; in units, drawn as the
  // reference line, as the server sends it.
  const measured = (list: Series[], p: string, kind: "cpu" | "memory"): Series[] => {
    const count = instances ? 1 : (replicas[p] ?? 1);
    const whole = allocation[kind] * count;
    return list.map((s) => {
      if (percent) return { ...s, points: s.points.map(([t, v]): Point => [t, Math.round((v / whole) * 1000) / 10]) };
      return kind === "cpu" ? { ...s, reference: whole, burst: allocation.burst * count } : { ...s, reference: whole };
    });
  };

  const hidden = instances && !replaced ? "1 replaced instance hidden" : undefined;
  const chart = (list: Series[], unit: string, instanceCapable: boolean, note?: string): MetricChart => ({ series: list, unit, instanceCapable, note });
  const network = (["in", "out"] as const).flatMap((dir, d) => {
    const rest = { peaks: 0.08, peak: 3 };
    // The whole project's traffic, unless one process is named or the
    // instances are asked for: the server joins the pods only then.
    if (!instances) {
      const name = query.process ? `${query.process} ${dir}` : dir;
      return [series(name, 50 + d, (180 - d * 120) * 1024 * processes.length, 0.4, rest)];
    }
    return processes.flatMap((p, i) => collapse(usage(p, 50 + d + i * 3, (90 - d * 60 - i * 20) * 1024, 0.4, rest, ` ${dir}`), agg, `${p} ${dir}`));
  });
  return {
    from: to - 60 * step,
    to,
    responseTime: chart([
      { name: "Max", points: over(p99, seed + 4, 1.05, 1.8) },
      { name: "99th percentile", points: p99 },
      { name: "95th percentile", points: p95 },
      { name: "Median", points: median },
    ], "ms", false),
    throughput: chart([
      { name: "2xx", tone: "success", points: wander(seed + 21, to, { step, base: 42, swing: 0.4 }) },
      { name: "3xx", tone: "info", points: wander(seed + 22, to, { step, base: 6, swing: 0.5 }) },
      { name: "4xx", tone: "warning", points: wander(seed + 23, to, { step, base: 3, swing: 0.6, peaks: 0.1 }) },
      { name: "5xx", tone: "error", points: wander(seed + 24, to, { step, base: 0.6, swing: 0.9, peaks: 0.12, peak: 6 }) },
    ], "rps", false),
    cpu: chart(processes.flatMap((p, i) => collapse(measured(usage(p, i, 0.3 - i * 0.1, 0.5, { peaks: 0.06, peak: 1.8 }), p, "cpu"), agg, p)), percent ? "%" : "cores", true, hidden),
    memory: chart(processes.flatMap((p, i) => collapse(measured(usage(p, 30 + i, (300 - i * 100) * MiB, 0.18, { peaks: 0.04, peak: 1.4 }), p, "memory"), agg, p)), percent ? "%" : "bytes", true, hidden),
    network: chart(network, "bytes/s", true, hidden),
    instances: chart(processes.map((p, i) => ({ name: p, points: steps([[0, Math.max(1, (replicas[p] ?? 1) - 1)], [19 + i * 9, replicas[p] ?? 1]], to, 60, step) })), "count", false),
    releases: [
      { time: to - 41 * step, label: "v11" },
      { time: to - 12 * step, label: "v12" },
    ],
    // The first process crossed its memory twice since the last release;
    // the server answers a process filter with that process's kills only.
    ooms: processes.includes(first)
      ? [
          { time: to - 9 * step, process: first, instance: `${first}.1`, limit: 512 * MiB, size: "shared-l", release: 12 },
          { time: to - 3 * step, process: first, instance: `${first}.1`, limit: 512 * MiB, size: "shared-l", release: 12 },
        ]
      : [],
  };
}

const at = (h: number, m: number, s: number) => `${String(h).padStart(2, "0")}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;

export function logsOf(slug: string, processes: string[]): LogLine[] {
  const web = processes.includes("web") ? "web-1" : `${processes[0] ?? "worker"}-1`;
  const worker = processes.includes("worker") ? "worker-1" : web;
  return [
    { time: at(17, 4, 1), instance: web, level: "info", levelText: "info", message: "listening on :3000" },
    { time: at(17, 4, 3), instance: worker, message: `Sat Sep 29 17:04:03 2026 booting ${slug}` },
    { time: at(17, 4, 12), instance: web, level: "info", levelText: "INFO", message: "request", fields: [{ key: "method", value: "GET" }, { key: "path", value: "/api/projects" }, { key: "status", value: "200" }, { key: "duration_ms", value: "23" }] },
    { time: at(17, 4, 12), instance: web, level: "debug", levelText: "debug", message: "cache hit", fields: [{ key: "key", value: `projects:${slug}` }, { key: "age_s", value: "41" }] },
    { time: at(17, 4, 14), instance: web, level: "warn", levelText: "WARN", message: "slow query", fields: [{ key: "duration_ms", value: "1840" }, { key: "sql", value: "SELECT * FROM releases WHERE project_id = $1 ORDER BY number DESC" }] },
    { time: at(17, 4, 15), instance: worker, level: "error", levelText: "error", message: "job failed", fields: [{ key: "job", value: "reports.monthly" }, { key: "attempt", value: "3" }, { key: "error", value: '{"name":"TimeoutError","message":"the database did not answer in 30s","retry":true}', json: { name: "TimeoutError", message: "the database did not answer in 30s", retry: true } }] },
    { time: at(17, 4, 16), instance: web, level: "error", message: "Error: connection reset by peer" },
    { time: at(17, 4, 16), instance: web, message: "    at Socket.emit (node:events:519:28)" },
    { time: at(17, 4, 20), instance: web, level: "info", levelText: "info", message: "request", fields: [{ key: "method", value: "POST" }, { key: "path", value: "/api/deploys" }, { key: "status", value: "202" }, { key: "duration_ms", value: "118" }] },
    { time: at(17, 4, 31), instance: worker, level: "info", levelText: "info", message: "job done", fields: [{ key: "job", value: "mail.digest" }, { key: "sent", value: "132" }] },
    { time: at(17, 4, 40), instance: web, message: "\u001b[32mGET\u001b[0m /healthz 200 1ms" },
  ];
}

// One more line, as if the application had just written it.
export function liveLine(slug: string, process: string, n: number): LogLine {
  const d = new Date();
  const time = at(d.getHours(), d.getMinutes(), d.getSeconds());
  const kinds: LogLine[] = [
    { time, instance: `${process}-1`, level: "info", levelText: "info", message: "request", fields: [{ key: "method", value: "GET" }, { key: "path", value: `/${["", "api/projects", "healthz", "assets/app.js"][n % 4]}` }, { key: "status", value: "200" }, { key: "duration_ms", value: String(8 + ((n * 37) % 90)) }] },
    { time, instance: `${process}-1`, level: "debug", levelText: "debug", message: "cache hit", fields: [{ key: "key", value: `projects:${slug}` }] },
    { time, instance: `${process}-1`, message: `\u001b[32mGET\u001b[0m /healthz 200 ${1 + (n % 3)}ms` },
    { time, instance: `${process}-1`, level: "warn", levelText: "WARN", message: "slow query", fields: [{ key: "duration_ms", value: String(900 + ((n * 131) % 1200)) }] },
  ];
  return kinds[n % kinds.length]!;
}

export const buildOutput = [
  "===> Cloning at a3f9c21",
  "Receiving objects: 100% (1284/1284), 2.1 MiB",
  "===> Detecting",
  "node: 20.11.1",
  "npm:  10.2.4",
  "===> Building with buildpacks",
  "[builder] Installing 312 packages",
  "[builder] added 312 packages in 8s",
  "[builder] > build",
  "[builder] ✓ Compiled successfully in 6.2s",
  "===> Exporting",
  "Image sha256:9f2c…41ab pushed to the registry",
  "===> Releasing",
  "web: rolling",
];
