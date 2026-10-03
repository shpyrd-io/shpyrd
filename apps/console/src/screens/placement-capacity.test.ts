import { describe, expect, it } from "vitest";
import type { PlacementGroup, PlacementNode } from "@shpyrd/shared/api/project-archives";
import { placementCandidates } from "./placement-capacity";

const GiB = 1024 ** 3;
const group: PlacementGroup = { id: "process:web", processes: ["web", "worker"], volumes: ["uploads"], nodes: ["source"], pool: "apps", cpuRequestedMillicores: 1000, memoryRequestedBytes: 2 * GiB, diskUsedBytes: 5 * GiB };
const node = (name: string, changes: Partial<PlacementNode> = {}): PlacementNode => ({ name, hostname: name, pool: "apps", architecture: "arm64", eligible: true, cpuMillicores: 4000, memoryBytes: 16 * GiB, cpuRequestedMillicores: 500, memoryRequestedBytes: 2 * GiB, diskAvailableBytes: 30 * GiB, ...changes });

describe("planning a move", () => {
  it("compares projected capacity and chooses balanced headroom instead of CPU alone", () => {
    const nodes = [node("cpu-heavy", { cpuRequestedMillicores: 0, memoryRequestedBytes: 13 * GiB }), node("balanced"), node("source"), node("database", { pool: "data" })];
    const balanced = placementCandidates(group, nodes, "balanced");
    expect(balanced.map((c) => c.node.name)).toEqual(["balanced", "cpu-heavy", "source"]);
    expect(balanced[0]).toMatchObject({ cpu: 2500, memory: 12 * GiB, disk: 25 * GiB, known: true });
    expect(placementCandidates(group, nodes, "cpu")[0].node.name).toBe("cpu-heavy");
  });
  it("keeps unknown disk distinct from an empty volume and does not recommend it over a verified fit", () => {
    const result = placementCandidates(group, [node("unknown", { diskAvailableBytes: undefined }), node("known", { cpuRequestedMillicores: 1000 })], "balanced");
    expect(result[0].node.name).toBe("known");
    expect(result[1]).toMatchObject({ disk: undefined, known: false });
    expect(placementCandidates({ ...group, diskUsedBytes: undefined }, [node("target")], "balanced")[0].known).toBe(false);
    expect(placementCandidates({ ...group, volumes: [], diskUsedBytes: 0 }, [node("target", { diskAvailableBytes: undefined })], "balanced")[0].known).toBe(true);
  });
  it("rejects insufficient capacity, pressure, current nodes, and a different architecture", () => {
    const nodes = [node("source"), node("full-cpu", { cpuRequestedMillicores: 3500 }), node("full-memory", { memoryRequestedBytes: 15 * GiB }), node("full-disk", { diskAvailableBytes: 5.5 * GiB }), node("pressure", { eligible: false, reason: "DiskPressure" }), node("x86", { architecture: "amd64" })];
    expect(placementCandidates(group, nodes, "balanced").every((c) => !!c.reason)).toBe(true);
  });
  it("reserves scratch space even when the volume is empty", () => {
    expect(placementCandidates({ ...group, diskUsedBytes: 0 }, [node("target", { diskAvailableBytes: 0 })], "balanced")[0].reason).toContain("Insufficient disk");
  });
  it("sorts free memory and disk after subtracting the workload", () => {
    const nodes = [node("memory", { diskAvailableBytes: 20 * GiB }), node("disk", { memoryRequestedBytes: 8 * GiB, diskAvailableBytes: 40 * GiB })];
    expect(placementCandidates(group, nodes, "memory")[0].node.name).toBe("memory");
    expect(placementCandidates(group, nodes, "disk")[0].node.name).toBe("disk");
  });
  it("does not interpret a stopped workload's missing reservations as zero", () => {
    const result = placementCandidates({ ...group, cpuRequestedMillicores: undefined, memoryRequestedBytes: undefined }, [node("target")], "balanced")[0];
    expect(result).toMatchObject({ cpu: undefined, memory: undefined, known: false });
  });
  it("allows migration on the current node without counting its existing pods twice", () => {
    const source = node("source", { cpuRequestedMillicores: 3800, memoryRequestedBytes: 15 * GiB });
    const candidate = placementCandidates({ ...group, needsMigration: true }, [source], "balanced")[0];
    expect(candidate.reason).toBeUndefined();
    expect(candidate).toMatchObject({ cpu: 200, memory: GiB, disk: 25 * GiB });
  });

});
