import { describe, expect, it } from "vitest";
import { api } from "./api";

// NEXT_PUBLIC_API_MODE is "mock" in the tests: every call goes to the Mock.
describe("the api picks the Mock when asked to", () => {
  it("answers the config and the workspaces from the JSON files", async () => {
    const config = await api.config();
    expect(config.capabilities).toContain("workspaces");
    const workspaces = await api.workspaces();
    expect(workspaces.map((w) => w.slug)).toContain(config.defaultWorkspaceId);
  });

  it("keeps a change: the sizes saved are the sizes read after", async () => {
    const before = await api.sizes();
    const added = { name: "shared-test", kind: "shared" as const, cpu: "0.5", memory: "128Mi" };
    await api.saveSizes({ ...before, sizes: [...before.sizes, added] });
    const after = await api.sizes();
    expect(after.sizes.map((s) => s.name)).toContain("shared-test");
    expect(after.default).toBe(before.default);
  });
});
