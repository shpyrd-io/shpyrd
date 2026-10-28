import { describe, expect, it } from "vitest";
import { memoryLimits, oomMarker } from "./metrics";

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

  it("draws one line a memory allocation, named after the processes that have it", () => {
    const limits = memoryLimits({
      memory: [
        { name: "web", points: [], reference: 512 },
        { name: "worker", points: [], reference: 512 },
        { name: "cron", points: [], reference: 128 },
        { name: "replaced 1", points: [] },
      ],
    });
    expect(limits).toEqual([
      { value: 512, label: "web, worker", tone: "neutral" },
      { value: 128, label: "cron", tone: "neutral" },
    ]);
  });
});
