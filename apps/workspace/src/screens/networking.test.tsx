import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import type { ReactNode } from "react";
import type { Project } from "@/api/types";
import type { Perms } from "@/lib/perms";
import seed from "../../mock/projects.json";
import { Heading } from "./project/heading";
import { Launcher } from "./launcher";

const fake = vi.hoisted(() => ({ config: vi.fn(), me: vi.fn(), projects: vi.fn(), createProject: vi.fn(), setExposure: vi.fn() }));
vi.mock("@/api/api", async () => {
  const original = await vi.importActual<typeof import("@/api/api")>("@/api/api");
  return { ...original, api: fake };
});
vi.mock("@/shell/person", () => ({ ThemeButton: () => null, PersonMenu: () => null }));

const project = seed[0] as unknown as Project;
const perms = { deploy: true, config: false, destroy: false } as Perms;
let queries: QueryClient;
function show(children: ReactNode, supported?: boolean) {
  const config = { workspace: { slug: "acme", name: "Acme", capabilities: supported === undefined ? undefined : { internalExposure: supported } } };
  fake.config.mockResolvedValue(config);
  queries = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queries.setQueryData(["config"], config);
  render(<QueryClientProvider client={queries}><MemoryRouter>{children}</MemoryRouter></QueryClientProvider>);
}
beforeEach(() => {
  vi.clearAllMocks();
  fake.me.mockResolvedValue({ roles: { workspace: "owner", enforced: true } });
  fake.projects.mockResolvedValue([project]);
  fake.setExposure.mockResolvedValue(project);
});
afterEach(() => { cleanup(); queries?.clear(); });

describe("workspace networking in project settings", () => {
  it.each([false, undefined])("does not offer internal exposure when availability is %s", (supported) => {
    show(<Heading project={project} perms={perms} />, supported);
    expect(screen.queryByTitle(/Click to keep it/)).toBeNull();
    expect(screen.getByTitle("Private networking is not enabled for this workspace.")).toBeTruthy();
  });
  it("offers internal exposure when the server enables it", async () => {
    show(<Heading project={project} perms={perms} />, true);
    fireEvent.click(screen.getByTitle(/Click to keep it/));
    fireEvent.click(await screen.findByRole("button", { name: "Local network" }));
    await waitFor(() => expect(fake.setExposure).toHaveBeenCalledWith(project.slug, "internal"));
  });
  it("keeps legacy internal apps visible and allows an explicit move to external", async () => {
    show(<Heading project={{ ...project, exposure: "internal" }} perms={perms} />, false);
    fireEvent.click(screen.getByTitle(/Click to put it/));
    fireEvent.click(await screen.findByRole("button", { name: "Public internet" }));
    await waitFor(() => expect(fake.setExposure).toHaveBeenCalledWith(project.slug, "external"));
  });
  it("still requires the person's deploy permission", () => {
    show(<Heading project={project} perms={{ ...perms, deploy: false }} />, true);
    expect(screen.queryByTitle(/Click to keep it/)).toBeNull();
  });
});

describe("workspace networking in the launcher", () => {
  it.each([false, true])("shows the creation network choice only when supported (%s)", async (supported) => {
    show(<Launcher />, supported);
    fireEvent.click(await screen.findByRole("button", { name: "New project" }));
    expect(!!screen.queryByRole("combobox", { name: "Network" })).toBe(supported);
  });
  it("does not relabel a legacy internal app as public", async () => {
    fake.projects.mockResolvedValue([{ ...project, exposure: "internal" }]);
    show(<Launcher />, false);
    expect(await screen.findByRole("img", { name: "Local network" })).toBeTruthy();
  });
});
