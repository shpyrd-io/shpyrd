import { describe, expect, it } from "vitest";
import { oomMarker, references } from "./metrics";

// #119: the Memory chart marks each kill, apart from the releases.
describe("the Memory chart's markers", () => {
  it("marks a kill in the ink of an error and says what died, at what limit", () => {
    expect(oomMarker({ time: 100, process: "web", instance: "web.1", limit: 256 * 1024 * 1024, size: "shared-m", release: 12 })).toEqual({
      time: 100,
      label: "OOM",
      tone: "error",
      title: "web.1 killed: out of memory at its 256.0 MiB (shared-m)",
    });
  });

  it("names a release step or a build by its role, and a limit alone when the size is not one the process has", () => {
    expect(oomMarker({ time: 1, process: "release", limit: 64 * 1024 * 1024 }).title).toBe("the release step killed: out of memory at its 64.0 MiB");
    expect(oomMarker({ time: 1, process: "build", limit: 0 }).title).toBe("the build killed: out of memory");
  });
});

// RFC-0027: the lines CPU and memory are measured against.
describe("the lines a chart is measured against", () => {
  it("draws one line an allocation and one a burst, named by process only where they differ", () => {
    const lines = references(
      {
        unit: "cores",
        instanceCapable: true,
        series: [
          { name: "web.1", points: [], reference: 0.5, burst: 1 },
          { name: "web.2 in", points: [], reference: 0.5, burst: 1 },
          { name: "sum worker", points: [], reference: 1, burst: 1 },
          { name: "replaced 1", points: [], reference: 0.5, burst: 1 },
        ],
      },
      false,
    );
    expect(lines).toEqual([
      { value: 0.5, label: "web allocation", tone: "neutral" },
      { value: 1, label: "worker allocation", tone: "neutral" },
      { value: 1, label: "burst", tone: "warning" },
    ]);
  });

  it("calls the one line every series shares the allocation", () => {
    const chart = { unit: "bytes", instanceCapable: true, series: [{ name: "web", points: [], reference: 512 }, { name: "worker", points: [], reference: 512 }] };
    expect(references(chart, false)).toEqual([{ value: 512, label: "allocation", tone: "neutral" }]);
  });

  it("draws the size as 100% as a share, and nothing on an empty chart", () => {
    expect(references({ unit: "%", instanceCapable: true, series: [{ name: "web", points: [] }] }, true)).toEqual([{ value: 100, label: "the size", tone: "error" }]);
    expect(references({ unit: "%", instanceCapable: true, series: [] }, true)).toEqual([]);
    expect(references({ unit: "cores", instanceCapable: true, series: [{ name: "web", points: [] }] }, false)).toEqual([]);
  });
});
