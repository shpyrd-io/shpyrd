import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { WorkspaceInfo } from "@/api/types";
import workspace from "../../../mock/workspace.json";
import { Plan } from "./plan";

// The queries answer by the first word of their key: "workspace",
// "projects". Permissions say the person administers the workspace.
const state = vi.hoisted(() => ({ answers: {} as Record<string, unknown> }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: unknown[] }) => ({ data: state.answers[String(queryKey[0])], isLoading: false, error: undefined }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  useMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));
vi.mock("@/lib/perms", () => ({ usePerms: () => ({ loaded: true, enforced: true, admin: true, me: { provider: "github" } }) }));
vi.mock("react-router-dom", () => ({ Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a> }));

const paid = workspace as unknown as WorkspaceInfo;
const draw = (w: WorkspaceInfo) => {
  state.answers = { workspace: w, projects: [{ slug: "hello-world", displayName: "Hello World" }] };
  return renderToStaticMarkup(<Plan />);
};
afterEach(() => {
  state.answers = {};
});

describe("the plan card", () => {
  it("draws the three storage ceilings beside the plan's five, with what the plan includes", () => {
    const html = draw(paid);
    for (const label of ["Projects", "Instances", "CPU", "Memory", "Block storage", "Object storage", "Buckets", "Databases"]) {
      expect(html).toContain(label);
    }
    expect(html).toContain("A bucket holds up to 10 GB.");
    expect(html).toContain("Shared and dedicated databases, shared by default; a shared one holds up to 1 GB.");
    expect(html).toContain("A dedicated database runs up to 3 instances.");
    expect(html).toContain("Caps per project");
    expect(html).toContain("Hello World");
  });

  it("says the free workspace has no disks and one shared database, and hides the caps", () => {
    const html = draw({
      ...paid,
      limits: { projects: 2, allowDisks: false, objectStorage: { buckets: 1, bucketCap: "1Gi" }, postgres: { types: ["shared"], defaultType: "shared", sharedCap: "1Gi", databases: 1 } },
      usage: { projects: 1, instances: 1, cpu: "0.5", memory: "256Mi", storage: "0", buckets: 0, databases: 1, sharedDatabases: 1 },
      projectCaps: undefined,
    });
    expect(html).not.toContain("Block storage");
    expect(html).toContain("Disks are not included; files go in a bucket.");
    expect(html).toContain("Shared databases, each up to 1 GB; a dedicated one comes with a paid plan.");
    expect(html).not.toContain("Caps per project");
  });

  it("reads a usage above a ceiling that shrank as a percentage, and says everything keeps working", () => {
    const html = draw({ ...paid, limits: { ...paid.limits, storage: "100Gi" }, usage: { ...paid.usage!, storage: "120Gi" } });
    expect(html).toContain("Block storage · 120%");
    expect(html).toContain("Over the line on block storage: nothing more fits there until something is freed, and everything that exists keeps working.");
  });

  it("draws nothing of the plan when the workspace has no ceilings", () => {
    const html = draw({ ...paid, limits: undefined, projectCaps: undefined });
    expect(html).not.toContain("What this workspace may use");
  });
});
