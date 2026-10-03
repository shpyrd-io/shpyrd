// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mockProjectArchives } from "./project-archives-mock";

beforeEach(() => { localStorage.clear(); vi.useFakeTimers(); });
afterEach(() => vi.useRealTimers());

async function status(api: ReturnType<typeof mockProjectArchives>, id = "project") {
  const read = api.projectArchiveStatus(id);
  await vi.advanceTimersByTimeAsync(400);
  return read;
}

describe("the archive preview", () => {
  it("simulates progress, rejects overlapping operations and returns to idle", async () => {
    const api = mockProjectArchives("test");
    const backup = api.backupProject("project");
    await vi.advanceTimersByTimeAsync(800);
    expect(await status(api)).toMatchObject({ preview: true, active: true, kind: "export", phase: "preparing" });
    await expect(api.backupProject("project")).rejects.toThrow("already running");
    expect(await status(api, "other")).toEqual({ phase: "idle", preview: true });
    await vi.advanceTimersByTimeAsync(1_500);
    expect(await status(api)).toMatchObject({ phase: "paused", active: true });
    // A fresh page still sees the operation that was started before reload.
    expect(await status(mockProjectArchives("test"))).toMatchObject({ active: true });
    await vi.advanceTimersByTimeAsync(5_000);
    await backup;
    expect(await status(api)).toEqual({ phase: "idle", preview: true });
  });

  it("does not read the selected file when simulating restore", async () => {
    const api = mockProjectArchives("restore-test");
    const file = new File(["not a real archive"], "example.tgz");
    const read = vi.fn(() => { throw new Error("must not read a preview file"); });
    Object.defineProperty(file, "arrayBuffer", { value: read });
    Object.defineProperty(file, "stream", { value: read });
    const restore = api.restoreProject("project", file);
    await vi.advanceTimersByTimeAsync(800);
    expect(await status(api)).toMatchObject({ preview: true, kind: "restore", active: true });
    await vi.advanceTimersByTimeAsync(7_000);
    await restore;
    expect(read).not.toHaveBeenCalled();
  });
});
