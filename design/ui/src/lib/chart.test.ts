import { describe, expect, it } from "vitest";
import { area, axis, edge, shade, stepOf, toneOf, type Point, type Scale } from "./chart";

describe("axis", () => {
  it("goes from zero to the first round value that is not under the highest", () => {
    expect(axis(504, "ms")).toEqual([0, 200, 400, 600]);
    expect(axis(95, "rps")).toEqual([0, 25, 50, 75, 100]);
  });

  it("has no halves for a count", () => {
    expect(axis(4, "count")).toEqual([0, 1, 2, 3, 4]);
    expect(axis(7, "count").every(Number.isInteger)).toBe(true);
  });

  it("is round in powers of two for bytes", () => {
    const MiB = 1 << 20;
    expect(axis(512 * MiB, "bytes")).toEqual([0, 128 * MiB, 256 * MiB, 384 * MiB, 512 * MiB]);
  });

  it("stops at a hundred for a percentage that is under it", () => {
    expect(axis(46, "%")).toEqual([0, 25, 50, 75, 100]);
    expect(axis(140, "%").at(-1)).toBeGreaterThanOrEqual(140);
  });

  it("still has a top when there is nothing to show", () => {
    expect(axis(0)).toEqual([0, 1]);
    expect(axis(Number.NaN)).toEqual([0, 1]);
  });
});

describe("edge", () => {
  const scale: Scale = { from: 0, to: 100, max: 10 };
  const points: Point[] = [
    [0, 5],
    [50, 10],
  ];

  it("holds a value until the next one, in steps", () => {
    expect(edge(points, scale, "step", 100)).toBe("M0,50H500V0H1000");
  });

  it("goes straight from a value to the next, as a line", () => {
    expect(edge(points, scale, "line", 100)).toBe("M0,50L500,0");
  });

  it("draws nothing of a series without points", () => {
    expect(edge([], scale, "step", 100)).toBe("");
  });
});

describe("area", () => {
  const scale: Scale = { from: 0, to: 100, max: 10 };
  const points: Point[] = [
    [0, 5],
    [50, 10],
  ];

  it("closes the edge down to the bottom of the chart", () => {
    expect(area(points, scale, "step", 100)).toBe("M0,50H500V0H1000V100H0Z");
  });

  it("closes the edge down to the series under it, in a stack", () => {
    const under: Point[] = [
      [0, 2],
      [50, 4],
    ];
    expect(area(points, scale, "step", 100, under)).toBe(
      "M0,50H500V0H1000L1000,60H500V80H0Z",
    );
  });
});

describe("the ink of a series", () => {
  it("is taken in a fixed order, and grey from the fifth on", () => {
    expect([0, 1, 2, 3, 4, 9].map((i) => toneOf(undefined, i))).toEqual([
      "orange",
      "blue",
      "green",
      "violet",
      "neutral",
      "neutral",
    ]);
  });

  it("is the one the series names, wherever it comes", () => {
    expect(toneOf("error", 0)).toBe("error");
  });

  it("is stronger for the series in front, when all have the same", () => {
    expect(shade(3, 4).wash).toBeGreaterThan(shade(0, 4).wash);
    expect(shade(3, 4).line).toBe(1);
  });
});

describe("stepOf", () => {
  it("is the most common time between two points", () => {
    expect(stepOf([0, 60, 120, 300, 360])).toBe(60);
  });
});
