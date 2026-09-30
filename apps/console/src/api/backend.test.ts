import { describe, expect, it } from "vitest";
import { toClusterMetrics, type MetricsAnswer } from "./backend";

const usage = { name: "", cpu: { used: 1, reserved: 2, capacity: 8 }, memory: { used: 1, reserved: 2, capacity: 16 } } as unknown as MetricsAnswer["total"];

const answer = (charts: MetricsAnswer["charts"]): MetricsAnswer => ({ range: "1h", total: usage, nodes: [usage], charts });

describe("the metrics of the cluster, as the screens read them", () => {
  it("names the charts cpu and memory, one series for each node", () => {
    const m = toClusterMetrics(
      answer([
        { id: "cpu", series: [{ name: "node-a", points: [[1, 0.5]] }, { name: "node-b", points: [[1, 0.7]] }] },
        { id: "memory", series: [{ name: "node-a", points: [[1, 2]] }] },
      ]),
    );
    expect(m.range).toBe("1h");
    expect(m.total).toBe(usage);
    expect(m.nodes).toHaveLength(1);
    expect(m.cpu.map((s) => s.name)).toEqual(["node-a", "node-b"]);
    expect(m.cpu[0].points).toEqual([[1, 0.5]]);
    expect(m.memory.map((s) => s.name)).toEqual(["node-a"]);
  });

  it("gives each node a tone of its own, and starts over after the fifth", () => {
    const series = ["a", "b", "c", "d", "e", "f"].map((name) => ({ name, points: [] as [number, number][] }));
    const m = toClusterMetrics(answer([{ id: "cpu", series }]));
    expect(m.cpu.map((s) => s.tone)).toEqual(["orange", "blue", "green", "violet", "neutral", "orange"]);
  });

  it("has no series for a chart the server could not draw, or did not send", () => {
    const m = toClusterMetrics(answer([{ id: "cpu", series: [{ name: "node-a", points: [[1, 1]] }], error: "prometheus is not reachable" }]));
    expect(m.cpu).toEqual([]);
    expect(m.memory).toEqual([]);
  });
});
