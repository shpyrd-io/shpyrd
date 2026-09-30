import type { LogLine } from "@shpyrd/ui/components/log-view";
import type { Metrics, Point, Series } from "@/api/types";

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

export function metricsOf(slug: string, processes: string[], range: string): Metrics {
  const to = Math.floor(Date.now() / 1000 / 60) * 60;
  const step = range === "7d" ? 60 * 168 : range === "24h" ? 60 * 24 : range === "6h" ? 360 : 60;
  const seed = [...slug].reduce((h, c) => (h * 31 + c.charCodeAt(0)) >>> 0, 7);
  const median = wander(seed + 1, to, { step, base: 38, swing: 0.35, peaks: 0.05, peak: 1.8 });
  const p95 = over(median, seed + 2, 2.2, 5);
  const p99 = over(p95, seed + 3, 1.1, 1.9);
  const series = (name: string, i: number, base: number, swing: number, rest = {}): Series => ({ name, points: wander(seed + 10 + i, to, { step, base, swing, ...rest }) });
  return {
    from: to - 60 * step,
    to,
    responseTime: [
      { name: "Max", points: over(p99, seed + 4, 1.05, 1.8) },
      { name: "99th percentile", points: p99 },
      { name: "95th percentile", points: p95 },
      { name: "Median", points: median },
    ],
    throughput: [
      { name: "2xx", tone: "success", points: wander(seed + 21, to, { step, base: 42, swing: 0.4 }) },
      { name: "3xx", tone: "info", points: wander(seed + 22, to, { step, base: 6, swing: 0.5 }) },
      { name: "4xx", tone: "warning", points: wander(seed + 23, to, { step, base: 3, swing: 0.6, peaks: 0.1 }) },
      { name: "5xx", tone: "error", points: wander(seed + 24, to, { step, base: 0.6, swing: 0.9, peaks: 0.12, peak: 6 }) },
    ],
    cpu: processes.map((p, i) => ({ ...series(p, i, 0.46 - i * 0.2, 0.5, { peaks: 0.06, peak: 1.8 }), reference: 1, burst: 1.75 })),
    memory: processes.map((p, i) => ({ ...series(p, 30 + i, (340 - i * 150) * MiB, 0.18, { peaks: 0.04, peak: 1.4 }), reference: 512 * MiB })),
    network: processes.map((p, i) => series(p, 50 + i, (180 - i * 120) * 1024, 0.4, { peaks: 0.08, peak: 3 })),
    instances: processes.map((p, i) => ({ name: p, points: steps([[0, 2 + i], [19, 3 + i], [48, 4]], to, 60, step) })),
    releases: [
      { time: to - 41 * step, label: "v11" },
      { time: to - 12 * step, label: "v12" },
    ],
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
