// What the log views of the gallery show. Made up, and fixed: the times
// are written, not computed, so the page is the same built and in the
// browser.

import type { LogLine } from "@shpyrd/ui/components/log-view";

const web = (n: number) => `web-${n}`;

export const lines: LogLine[] = [
  { time: "17:04:01", instance: web(1), level: "info", levelText: "info", message: "listening on :3000" },
  { time: "17:04:01", instance: web(2), level: "info", levelText: "info", message: "listening on :3000" },
  { time: "17:04:03", instance: "worker-1", message: "Sat Sep 29 17:04:03 2026 booting worker" },
  {
    time: "17:04:12",
    instance: web(1),
    level: "info",
    levelText: "INFO",
    message: "request",
    fields: [
      { key: "method", value: "GET" },
      { key: "path", value: "/api/projects" },
      { key: "status", value: "200" },
      { key: "duration_ms", value: "23" },
    ],
  },
  {
    time: "17:04:12",
    instance: web(2),
    level: "debug",
    levelText: "debug",
    message: "cache hit",
    fields: [
      { key: "key", value: "projects:acme" },
      { key: "age_s", value: "41" },
    ],
  },
  {
    time: "17:04:14",
    instance: web(1),
    level: "warn",
    levelText: "WARN",
    message: "slow query",
    fields: [
      { key: "duration_ms", value: "1840" },
      { key: "sql", value: "SELECT * FROM releases WHERE project_id = $1 ORDER BY number DESC" },
    ],
  },
  {
    time: "17:04:15",
    instance: "worker-1",
    level: "error",
    levelText: "error",
    message: "job failed",
    fields: [
      { key: "job", value: "reports.monthly" },
      { key: "attempt", value: "3" },
      {
        key: "error",
        value: '{"name":"TimeoutError","message":"the database did not answer in 30s","retry":true}',
        json: { name: "TimeoutError", message: "the database did not answer in 30s", retry: true },
      },
      {
        key: "context",
        value: '{"project":"acme","month":"2026-09","rows":[12,48,3]}',
        json: { project: "acme", month: "2026-09", rows: [12, 48, 3] },
      },
    ],
  },
  { time: "17:04:16", instance: web(2), level: "error", message: "Error: connection reset by peer" },
  { time: "17:04:16", instance: web(2), message: "    at Socket.emit (node:events:519:28)" },
  { time: "17:04:20", instance: web(1), level: "info", levelText: "info", message: "request", fields: [{ key: "method", value: "POST" }, { key: "path", value: "/api/deploys" }, { key: "status", value: "202" }, { key: "duration_ms", value: "118" }] },
  { time: "17:04:31", instance: "worker-1", level: "info", levelText: "info", message: "job done", fields: [{ key: "job", value: "mail.digest" }, { key: "sent", value: "132" }] },
  { time: "17:04:40", instance: web(1), message: "\u001b[32mGET\u001b[0m /healthz 200 1ms" },
];

export const build = [
  "===> Cloning github.com/acme/hello-world at a3f9c21",
  "Receiving objects: 100% (1284/1284), 2.1 MiB",
  "===> Detecting",
  "node: 20.11.1",
  "npm:  10.2.4",
  "===> Building with buildpacks",
  "[builder] Installing 312 packages",
  "[builder] added 312 packages in 8s",
  "[builder] > hello-world@1.4.0 build",
  "[builder] > next build",
  "[builder] ✓ Compiled successfully in 6.2s",
  "[builder] warning: 2 packages are looking for funding",
  "===> Exporting",
  "Layer 'buildpacksio/lifecycle:launch' saved",
  "Image sha256:9f2c…41ab pushed to the registry",
  "===> Releasing v13",
  "web: 4 instances, rolling",
  "worker: 1 instance",
  "Error: worker exited with status 1 (see the logs)",
];
