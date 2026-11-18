import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import type { CatalogUpdatePreview, CatalogVersion, Project } from "@/api/types";
import type { Perms } from "@/lib/perms";
import seed from "../../../mock/projects.json";
import { CatalogUpdate } from "./catalog_update";
import { Releases } from "./releases";

const fake = vi.hoisted(() => ({
  catalogUpdate: vi.fn(),
  applyCatalogUpdate: vi.fn(),
  builds: vi.fn(),
  rollback: vi.fn(),
  streamBuild: vi.fn(),
}));
vi.mock("@/api/api", () => ({ api: fake }));
let queries: QueryClient;
afterEach(() => {
  cleanup();
  queries?.clear();
  vi.resetAllMocks();
});

const fluxyr = seed.find((p) => p.slug === "fluxyr") as unknown as Project;
const offered = fluxyr.catalog!.offered as CatalogVersion;
const preview: CatalogUpdatePreview = {
  entry: "fluxyr",
  state: "update-available",
  from: fluxyr.catalog!.running,
  to: offered,
  inputs: [
    { name: "FLUXYR_TOKEN", label: "Access token", required: true, secret: true, action: "ask" },
    { name: "VAULT_ENCRYPTION_KEY", secret: true, action: "generate" },
  ],
  volumes: ["cache"],
  resources: [],
  processesAdded: ["worker"],
  processesChanged: [],
  envChanged: [],
  buildChanged: false,
  overlayChanged: false,
};
const everything = { deploy: true, config: true, resource: true } as Perms;

const show = (project: Project, perms: Partial<Perms> = everything) => {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queries}>
      <MemoryRouter>
        <CatalogUpdate project={project} perms={perms as Perms} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
};

it("says which version a project from the catalog runs, and that it is up to date", () => {
  show({ ...fluxyr, catalog: { entry: "fluxyr", running: offered, state: "up-to-date" } });
  expect(screen.getByText(/It runs v1.4.0 \(222222222222\), the version the catalog offers/)).toBeTruthy();
  expect(screen.queryByRole("button")).toBeNull();
});

it("says the update is rolling out while it is", () => {
  show({ ...fluxyr, catalog: { ...fluxyr.catalog!, state: "updating" } });
  expect(screen.getByText(/Updating to v1.4.0 \(222222222222\)/)).toBeTruthy();
  expect(screen.queryByRole("button")).toBeNull();
});

it("shows nothing for a project that is not from the catalog", () => {
  const plain = { ...fluxyr, catalog: undefined, catalogEntry: undefined };
  show(plain);
  expect(screen.queryByText("Catalog")).toBeNull();
});

it("previews the update, asks for the new input, never shows what is generated, and applies with the digest previewed", async () => {
  fake.catalogUpdate.mockResolvedValue(preview);
  fake.applyCatalogUpdate.mockResolvedValue({ changed: true, to: offered });
  show(fluxyr);
  expect(await screen.findByText(/The catalog offers v1.4.0 \(222222222222\)/)).toBeTruthy();
  expect(screen.getByText("Makes volumes: cache.")).toBeTruthy();
  expect(screen.getByText("Adds processes: worker.")).toBeTruthy();
  expect(screen.getByText("Generates VAULT_ENCRYPTION_KEY.")).toBeTruthy();
  const button = screen.getByRole("button", { name: "Update to v1.4.0 (222222222222)" }) as HTMLButtonElement;
  expect(button.disabled).toBe(true);
  fireEvent.change(screen.getByLabelText(/Access token/), { target: { value: "tok" } });
  expect(button.disabled).toBe(false);
  fireEvent.click(button);
  await waitFor(() => expect(fake.applyCatalogUpdate).toHaveBeenCalledWith("fluxyr", { digest: "sha256:bbbb", inputs: { FLUXYR_TOKEN: "tok" } }));
});

it("shows the server's words when it refuses", async () => {
  fake.catalogUpdate.mockResolvedValue({ ...preview, inputs: [] });
  fake.applyCatalogUpdate.mockRejectedValue(new Error("the catalog has published another version since you previewed this one"));
  show(fluxyr);
  fireEvent.click(await screen.findByRole("button", { name: /Update to/ }));
  await waitFor(() => expect(fake.applyCatalogUpdate).toHaveBeenCalled());
  expect(fake.applyCatalogUpdate.mock.calls[0][1]).toEqual({ digest: "sha256:bbbb", inputs: {} });
});

it("offers no action to who may not deploy, or may not make what the update makes", async () => {
  fake.catalogUpdate.mockResolvedValue(preview);
  show(fluxyr, { deploy: false, config: false, resource: false });
  expect(await screen.findByText(/The catalog offers/)).toBeTruthy();
  expect((screen.getByRole("button", { name: /Update to/ }) as HTMLButtonElement).disabled).toBe(true);
  cleanup();
  show(fluxyr, { deploy: true, config: true, resource: false });
  expect(await screen.findByText(/needs the permission to manage resources/)).toBeTruthy();
  expect((screen.getByRole("button", { name: /Update to/ }) as HTMLButtonElement).disabled).toBe(true);
});

it("names the version on the releases that have one", () => {
  fake.builds.mockResolvedValue([]);
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const project = {
    ...fluxyr,
    status: {
      ...fluxyr.status,
      releases: [
        ...fluxyr.status.releases,
        { number: 2, kind: "deploy", description: "Deploy of another commit", createdAt: "2026-10-21T09:00:00Z" },
      ],
    },
  } as Project;
  render(
    <QueryClientProvider client={queries}>
      <MemoryRouter>
        <Releases project={project} perms={everything} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(screen.getAllByText(/runs v1.3.0 \(111111111111\)/)).toHaveLength(1);
});
