import {
  cleanup,
  configure,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Costs } from "./costs";
import { api } from "@/api/api";
import { exploreMockCosts } from "@/api/costs-mock";

vi.mock("@/api/api", () => ({
  api: {
    costs: vi.fn(),
    createCostExport: vi.fn(),
    costExport: vi.fn(),
    costExportURL: vi.fn(),
  },
}));
configure({ asyncUtilTimeout: 5000 });
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});
function setup() {
  vi.mocked(api.costs).mockImplementation((q) =>
    Promise.resolve(
      exploreMockCosts({ ...q, from: "2026-10-01", to: "2026-10-14" }),
    ),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <Costs />
    </QueryClientProvider>,
  );
}
describe("exploring cluster costs", () => {
  it("requests pages from the server and resets to the first page when searching", async () => {
    setup();
    await screen.findByRole("button", { name: "signup-111" });
    expect(
      screen.queryByRole("button", { name: "tower" }),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() =>
      expect(api.costs).toHaveBeenCalledWith(
        expect.objectContaining({ offset: 50 }),
      ),
    );
    fireEvent.change(screen.getByRole("searchbox", { name: "Search" }), {
      target: { value: "tower-108" },
    });
    await screen.findByRole("button", { name: "tower-108" });
    await waitFor(() =>
      expect(api.costs).toHaveBeenCalledWith(
        expect.objectContaining({ offset: 0, search: "tower-108" }),
      ),
    );
    expect(screen.queryByRole("button", { name: "signup-111" })).toBeNull();
  });
  it("loads drilldown children only when expanded and keeps the project context", async () => {
    setup();
    await screen.findByRole("button", { name: "signup-111" });
    expect(
      vi.mocked(api.costs).mock.calls.some(([q]) => q.group === "workload"),
    ).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "signup-111" }));
    await screen.findByRole("button", { name: "Applications" });
    expect(api.costs).toHaveBeenCalledWith(
      expect.objectContaining({
        group: "workload",
        project: "project-111",
        workspace: "staging",
      }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Applications" }),
    );
    await screen.findByRole("button", { name: "web" });
    expect(api.costs).toHaveBeenCalledWith(
      expect.objectContaining({
        group: "process",
        project: "project-111",
        workload: "applications",
      }),
    );
  });
  it("exports the filtered dataset asynchronously and surfaces failures", async () => {
    setup();
    vi.mocked(api.createCostExport).mockResolvedValue({
      id: "export-1",
      status: "queued",
      expiresAt: "2026-10-15",
    });
    vi.mocked(api.costExport).mockResolvedValue({
      id: "export-1",
      status: "failed",
      expiresAt: "2026-10-15",
    });
    await screen.findByRole("button", { name: "Export CSV" });
    fireEvent.click(screen.getByRole("button", { name: "Export CSV" }));
    await screen.findByText("Export failed or expired. Try again.");
    expect(api.createCostExport).toHaveBeenCalledWith(
      expect.objectContaining({ group: "project", currency: "USD" }),
    );
    expect(screen.queryByRole("link", { name: "Download CSV" })).toBeNull();
  });
  it("resolves the asynchronous download URL before rendering the link", async () => {
    setup();
    const job = { id: "export-ready", status: "ready" as const, expiresAt: "2026-10-15" };
    vi.mocked(api.createCostExport).mockResolvedValue(job);
    vi.mocked(api.costExport).mockResolvedValue(job);
    vi.mocked(api.costExportURL).mockResolvedValue("/api/cluster/costs/exports/export-ready/download");
    fireEvent.click(await screen.findByRole("button", { name: "Export CSV" }));
    const link = await screen.findByRole("link", { name: "Download CSV" });
    expect(link.getAttribute("href")).toBe("/api/cluster/costs/exports/export-ready/download");
  });

});
