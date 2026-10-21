import fixture from "../../mock/overview.json";
import { wait } from "@shpyrd/shared/api/mock-store";
import { byNode } from "@/lib/samples";
import type { OverviewAPI, OverviewNode, OverviewPool } from "./cluster-overview";

const nodes = fixture.nodes as OverviewNode[];
for (const n of nodes) n.values.podUsedPct = 100 * n.values.pods! / n.values.podCapacity!;
const pools: OverviewPool[] = [...new Set(nodes.map(n => n.pool ?? ""))].map(name => {
  const rows = nodes.filter(n => (n.pool ?? "") === name);
  const values: OverviewPool["values"] = {};
  const coverage: Record<string, number> = {};
  const distribution: Record<string, number> = {};
  for (const k of Object.keys(rows[0].values)) {
    const present = rows.filter(n => n.values[k] !== undefined);
    coverage[k] = present.length;
    values[k] = present.reduce((sum, n) => sum + n.values[k]!, 0) / (k.endsWith("Pct") ? present.length : 1);
    distribution[k] = present.filter(n => k === "diskAvailablePct" ? n.values[k]! < (n.values.diskEvictionPct ?? 15) + 10 : k === "inodesAvailablePct" ? n.values[k]! < 10 : n.values[k]! >= 85).length;
  }
  distribution.diskEvicting7d = rows.filter(n => (n.values.diskPressure7d ?? 0) > 0 || (n.values.diskEvictions7d ?? 0) > 0).length;
  return { name, nodes: rows.length, ready: rows.filter(n => n.ready).length, issues: rows.filter(n => n.issues.length).length, missing: 0, values, coverage, distribution };
});
const updatedAt = () => new Date().toISOString();
export const overviewMock: OverviewAPI = {
  overview: async () => { await wait(); return { snapshot: `mock-${Math.floor(Date.now() / 30000)}`, updatedAt: updatedAt(), pools, errors: [], install: fixture.install }; },
  overviewNodes: async q => {
    await wait();
    const rows = nodes.filter(n => (q.pool === "*" || (n.pool ?? "") === q.pool) && (!q.search || n.name.includes(q.search)) && (!q.zone || n.zone === q.zone) && (!q.status || (q.status === "ready") === n.ready) && (q.issues !== "true" || n.issues.length > 0));
    const sort = q.sort ?? "cpuRequestedPct";
    rows.sort((a, b) => sort === "severity" ? Number(a.ready) - Number(b.ready) || b.issues.length - a.issues.length : sort === "name" ? a.name.localeCompare(b.name) : sort === "diskAvailablePct" ? (a.values[sort] ?? Infinity) - (b.values[sort] ?? Infinity) : (b.values[sort] ?? -Infinity) - (a.values[sort] ?? -Infinity));
    const offset = q.offset ?? 0, end = offset + (q.limit ?? 50);
    return { snapshot: "mock", updatedAt: updatedAt(), nodes: rows.slice(offset, end), total: rows.length, next: end < rows.length ? end : -1 };
  },
  overviewCharts: async q => {
    await wait();
    const selected = q.nodes ? nodes.filter(n => q.nodes!.split(",").includes(n.name)) : pools.filter(p => p.name === q.pool);
    let keys = q.view === "compute" ? ["cpuBusyPct", "memoryUsedPct"] : q.view === "storage" ? ["readIOPS", "writeIOPS", "readBytes", "writeBytes"] : ["receiveBytes", "transmitBytes"];
    if (q.mount) keys = ["available", "used"];
    else if (q.device && q.view === "storage") keys.push("readLatency", "writeLatency");
    const titles: Record<string, string> = { available: "Available filesystem space", used: "Used filesystem space", readLatency: "Read latency", writeLatency: "Write latency", cpuBusyPct: "CPU busy", memoryUsedPct: "Memory used", readIOPS: "Disk reads", writeIOPS: "Disk writes", readBytes: "Disk read throughput", writeBytes: "Disk write throughput", receiveBytes: "Network receive", transmitBytes: "Network transmit" };
    return { updatedAt: updatedAt(), charts: keys.map(k => ({ id: k, title: titles[k], unit: k.endsWith("Pct") ? "%" : k.endsWith("IOPS") ? "IOPS" : k.endsWith("Latency") ? "ms" : q.mount ? "bytes" : "bytes/s", series: byNode(selected.map(n => ({ name: q.device || n.name || "Default", base: k === "available" ? n.values.diskAvailable! : k === "used" ? n.values.diskSize! - n.values.diskFree! : k.endsWith("Latency") ? 2 : n.values[k] ?? 0 })), q.range, 3) })) };
  },
  overviewNode: async q => {
    await wait(); const node = nodes.find(n => n.name === q.node)!;
    const devices = q.view === "compute" ? [] : q.view === "storage" ? [
      { name: node.diskMount! + " (/dev/sda1)", device: "/dev/sda1", mount: node.diskMount, values: { size: node.values.diskSize, free: node.values.diskFree, available: node.values.diskAvailable, inodes: 1000000, inodesFree: 750000 } },
      { name: "sda", device: "sda", values: { readIOPS: node.values.readIOPS, writeIOPS: node.values.writeIOPS, readBytes: node.values.readBytes, writeBytes: node.values.writeBytes, readSeconds: 0.2, writeSeconds: 0.8 } },
    ] : [{ name: "eth0", device: "eth0", values: node.values }];
    return { node, devices, errors: [], truncated: false, updatedAt: updatedAt(), snapshotAt: updatedAt() };
  },
  overviewPods: async () => { await wait(); return { pods: [], continue: "" }; },
};
