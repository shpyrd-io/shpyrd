import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ProjectPlacement } from "./project-placement";
import { api } from "@/api/api";

vi.mock("@/api/api", () => ({ api: { projectPlacement: vi.fn(), projectArchiveStatus: vi.fn(), measureProjectPlacement: vi.fn(), moveProject: vi.fn(), deleteRetainedVolume: vi.fn() } }));
afterEach(() => { cleanup(); vi.resetAllMocks(); });
const button = (name: string | RegExp) => screen.getByRole("button", { name }) as HTMLButtonElement;
function setup(migration = false) {
  vi.mocked(api.projectArchiveStatus).mockResolvedValue({ phase: "idle" });
  vi.mocked(api.projectPlacement).mockResolvedValue({
    retainedVolumes: migration ? [{ name: "old-disk", claim: "vol-uploads", capacity: "50Gi", storageClass: "provider", retainedAt: new Date().toISOString() }] : [],
    groups: [{ needsMigration: migration, storageClasses: [migration ? "provider" : "shpyrd-local"], id: "process:web", processes: ["web"], volumes: ["uploads"], nodes: ["source"], pool: "apps", cpuRequestedMillicores: 500, memoryRequestedBytes: 1024 ** 3 }],
    nodes: [{ name: "target", hostname: "target", pool: "apps", eligible: true, cpuMillicores: 4000, cpuRequestedMillicores: 500, memoryBytes: 8 * 1024 ** 3, memoryRequestedBytes: 1024 ** 3, diskAvailableBytes: 10 * 1024 ** 3 }],
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(<QueryClientProvider client={client}><ProjectPlacement id="project" name="Project" /></QueryClientProvider>);
}

describe("planning resource moves", () => {
  it("shows unknown data, then updates the projection after measuring and locks controls while busy", async () => {
    let finish!: (value: { group: string; diskUsedBytes: number; measuredAt: string }) => void;
    vi.mocked(api.measureProjectPlacement).mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
    setup();
    await waitFor(() => expect(button("Measure data").disabled).toBe(false));
    expect(screen.queryByText("Recommended")).toBeNull();
    fireEvent.click(button("Measure data"));
    await waitFor(() => expect(button("Measuring…").disabled).toBe(true));
    expect(button("Select target").disabled).toBe(true);
    expect(button("Pause and move").disabled).toBe(true);
    await act(async () => finish({ group: "process:web", diskUsedBytes: 2 * 1024 ** 3, measuredAt: new Date().toISOString() }));
    await waitFor(() => expect(screen.getByText("Recommended")).toBeTruthy());
    expect(screen.getByText("2.0 GiB")).toBeTruthy();
    expect(screen.getByText("8.0 GiB")).toBeTruthy();
    expect(api.moveProject).not.toHaveBeenCalled();
  });
  it("requires a migration confirmation and offers separate retained-disk cleanup", async () => {
    setup(true);
    await waitFor(() => expect(button("Select target").disabled).toBe(false));
    expect(screen.getByText("Migrate provider storage to local disk")).toBeTruthy();
    fireEvent.click(button("Select target"));
    fireEvent.click(button("Pause and migrate"));
    expect(api.moveProject).not.toHaveBeenCalled();
    const dialog = screen.getByRole("dialog");
    expect(dialog.textContent).toContain("old provider disks will be retained");
    fireEvent.click(screen.getAllByRole("button", { name: "Pause and migrate" }).at(-1)!);
    await waitFor(() => expect(api.moveProject).toHaveBeenCalledWith("project", { group: "process:web", node: "target", migrateToLocal: true }));
    expect(api.deleteRetainedVolume).not.toHaveBeenCalled();
    await waitFor(() => expect(button("Delete old disk").disabled).toBe(false));
    fireEvent.click(button("Delete old disk"));
    expect(api.deleteRetainedVolume).not.toHaveBeenCalled();
    fireEvent.click(screen.getAllByRole("button", { name: "Delete old disk" }).at(-1)!);
    await waitFor(() => expect(api.deleteRetainedVolume).toHaveBeenCalledWith("project", "old-disk"));
  });

});
