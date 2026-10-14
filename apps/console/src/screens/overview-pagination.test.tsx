import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Overview } from "./overview";
import { api } from "@/api/api";
import type { Overview as Snapshot, OverviewNode } from "@/api/cluster-overview";

vi.mock("@/api/api", () => ({ api: { clusterScheduling: vi.fn(), overview: vi.fn(), overviewNodes: vi.fn(), overviewCharts: vi.fn(), overviewNode: vi.fn(), overviewPods: vi.fn() } }));
afterEach(() => { cleanup(); vi.resetAllMocks(); });
const node: OverviewNode = { name: "apps-1", pool: "apps", ready: true, roles: "worker", arch: "arm64", os: "linux", kubeletVersion: "v1", cpu: "4", memory: "8Gi", pods: "110", values: {}, conditions: [], issues: [], unschedulable: false, misplaced: 0 };
const snapshot: Snapshot = { snapshot: "first", updatedAt: new Date().toISOString(), errors: [], pools: [{ name: "apps", nodes: 100000, ready: 100000, issues: 0, missing: 1, values: {}, coverage: {}, distribution: {} }] };
function setup() {
  vi.mocked(api.clusterScheduling).mockResolvedValue({ pending: [{ namespace: "app-shop", name: "web-1", capacity: true, message: "Insufficient cpu" }] });
  vi.mocked(api.overview).mockResolvedValue(snapshot);
  vi.mocked(api.overviewNodes).mockImplementation(async q => ({ snapshot: q.snapshot, updatedAt: snapshot.updatedAt, nodes: q.issues === "true" ? [] : [node], total: q.issues === "true" ? 0 : 100000, next: (q.offset ?? 0) + 50 }));
  vi.mocked(api.overviewCharts).mockResolvedValue({ charts: [], updatedAt: snapshot.updatedAt });
  vi.mocked(api.overviewNode).mockResolvedValue({ node, devices: [], errors: [], truncated: false, updatedAt: snapshot.updatedAt, snapshotAt: snapshot.updatedAt });
  vi.mocked(api.overviewPods).mockResolvedValue({ pods: [], continue: "" });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><Overview /></QueryClientProvider>);
  return client;
}
describe("bounded cluster browsing", () => {
  it("shows scheduling warnings alongside the pool overview", async () => {
    setup();
    await screen.findByText("Instances waiting for cluster resources");
    expect(screen.getByText("app-shop/web-1")).toBeTruthy();
    expect(api.clusterScheduling).toHaveBeenCalled();
    expect(api.overviewPods).not.toHaveBeenCalled();
  });
  it("keeps later pages on their original snapshot when the overview refreshes", async () => {
    const client = setup();
    await screen.findByRole("button", { name: "apps-1" });
    fireEvent.click(screen.getByRole("button", { name: "Next 50" }));
    await waitFor(() => expect(api.overviewNodes).toHaveBeenCalledWith(expect.objectContaining({ snapshot: "first", offset: 50 }), expect.anything()));
    await act(async () => { client.setQueryData(["cluster-overview"], { ...snapshot, snapshot: "second" }); });
    expect(vi.mocked(api.overviewNodes).mock.calls.filter(([q]) => q.pool === "apps").at(-1)?.[0]).toMatchObject({ snapshot: "first", offset: 50 });
    expect(api.overviewPods).not.toHaveBeenCalled();
  });
  it("loads pod data only after the node's Pods tab is opened", async () => {
    setup();
    fireEvent.click(await screen.findByRole("button", { name: "apps-1" }));
    await waitFor(() => expect(api.overviewNode).toHaveBeenCalled());
    expect(api.overviewPods).not.toHaveBeenCalled();
    fireEvent.mouseDown(screen.getByRole("tab", { name: /^Pods$/ }), { button: 0, ctrlKey: false });
    await waitFor(() => expect(api.overviewPods).toHaveBeenCalledWith(expect.objectContaining({ node: "apps-1" }), expect.anything()));
  });
});
