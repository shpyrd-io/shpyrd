import type { PlacementGroup, PlacementNode } from "@shpyrd/shared/api/project-archives";

export type PlacementOrder = "balanced" | "cpu" | "memory" | "disk";
export const diskReserve = 1024 ** 3;
export const groupTitle = (group: PlacementGroup) => group.database ? `PostgreSQL · ${group.database}` : group.processes.join(" + ") || group.volumes.join(" + ");

export function placementCandidates(group: PlacementGroup, nodes: PlacementNode[], order: PlacementOrder) {
  const sources = nodes.filter((node) => group.nodes.includes(node.name) || group.nodes.includes(node.hostname));
  const candidates = nodes.filter((node) => !group.pool || node.pool === group.pool).map((node) => {
    const current = sources.includes(node);
    const staying = current && group.needsMigration && sources.length === 1;
    const hasData = !!group.database || group.volumes.length > 0;
    const cpu = group.cpuRequestedMillicores == null ? undefined : node.cpuMillicores - node.cpuRequestedMillicores - (staying ? 0 : group.cpuRequestedMillicores);
    const memory = group.memoryRequestedBytes == null ? undefined : node.memoryBytes - node.memoryRequestedBytes - (staying ? 0 : group.memoryRequestedBytes);
    const disk = group.diskUsedBytes == null || node.diskAvailableBytes == null ? undefined : node.diskAvailableBytes - group.diskUsedBytes;
    let reason = current && !staying ? "Current node" : !node.eligible ? node.reason || "Unavailable" : undefined;
    if (!reason && sources.some((source) => source.architecture && node.architecture && source.architecture !== node.architecture)) reason = "Different CPU architecture";
    if (!reason && ((cpu != null && cpu < 0) || (memory != null && memory < 0))) reason = "Insufficient CPU or memory";
    if (!reason && hasData && disk != null && disk < diskReserve) reason = "Insufficient disk (1 GiB reserve required)";
    const known = cpu != null && memory != null && (!hasData || disk != null);
    const score = Math.min(cpu == null || node.cpuMillicores <= 0 ? -1 : cpu / node.cpuMillicores, memory == null || node.memoryBytes <= 0 ? -1 : memory / node.memoryBytes);
    return { node, cpu, memory, disk, reason, current, known, score };
  });
  const value = (c: typeof candidates[number]) => order === "cpu" ? c.cpu : order === "memory" ? c.memory : order === "disk" ? c.disk : c.score;
  return candidates.sort((a, b) => Number(!!a.reason) - Number(!!b.reason) || (order === "balanced" ? Number(b.known) - Number(a.known) : 0) || (value(b) ?? -Infinity) - (value(a) ?? -Infinity) || a.node.name.localeCompare(b.node.name));
}
