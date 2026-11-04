import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import type { CatalogEntry, CatalogView } from "@/api/types";
import { Catalog, CatalogEntryPage, categoryName } from "./catalog";

const fake = vi.hoisted(() => ({
  config: vi.fn(),
  catalog: vi.fn(),
  catalogEntry: vi.fn(),
  installFromCatalog: vi.fn(),
}));
const toasts = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("@/api/api", async (actual) => ({ ...(await actual<typeof import("@/api/api")>()), api: fake }));
vi.mock("@/lib/networking", () => ({ useInternalExposure: () => false }));
vi.mock("@/shell/person", () => ({ PersonMenu: () => null, ThemeButton: () => null }));
vi.mock("sonner", () => ({ toast: toasts }));

const fluxyr: CatalogEntry = {
  name: "fluxyr",
  digest: "sha256:abc",
  displayName: "Fluxyr",
  description: "A Python framework for agents.",
  category: "agents",
  tags: ["agent", "python"],
  icon: "bot",
  iconColor: "teal",
  source: { git: "https://github.com/shpyrd-io/fluxyr", revision: "main" },
  inputs: [
    { name: "FLUXYR_MODEL", label: "Model", default: "minimax/minimax-m3" },
    { name: "OPENROUTER_API_KEY", label: "OpenRouter API key", secret: true, required: true },
    { name: "VAULT_ENCRYPTION_KEY", label: "Vault key", secret: true, generate: 32 },
  ],
  volumes: [{ name: "data", size: "5Gi" }],
  resources: [],
  processes: ["web"],
  build: "dockerfile",
};
const crm: CatalogEntry = {
  name: "twenty",
  digest: "sha256:def",
  displayName: "Twenty CRM",
  description: "Contacts, companies and deals.",
  category: "crm",
  source: { git: "https://github.com/twentyhq/twenty", revision: "main" },
  inputs: [],
  volumes: [],
  resources: [{ kind: "postgres", name: "db" }],
  processes: ["web", "worker"],
  build: "buildpacks",
};
const view: CatalogView = { url: "https://catalog.test/index.json", categories: ["agents", "crm"], entries: [fluxyr, crm] };

let queries: QueryClient;
function show(path: string) {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queries}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/catalog" element={<Catalog />} />
          <Route path="/catalog/:name" element={<CatalogEntryPage />} />
          <Route path="/projects/:slug" element={<p>project page</p>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
// Radix tabs switch on the mouse going down.
const tab = (name: string) => fireEvent.mouseDown(screen.getByRole("tab", { name }), { button: 0 });

beforeEach(() => {
  // Radix selects scroll the picked option into view; jsdom has no such thing.
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: vi.fn() });
  fake.config.mockResolvedValue({ domain: "example.test", catalog: true });
  fake.catalog.mockResolvedValue(view);
  fake.catalogEntry.mockImplementation(async (name: string) => ({ ...(name === "fluxyr" ? fluxyr : crm), readme: "# Readme" }));
  fake.installFromCatalog.mockResolvedValue({ project: { slug: "fluxyr" }, notes: [] });
});
afterEach(() => {
  cleanup();
  queries?.clear();
  vi.resetAllMocks();
});

it("lists the entries, and narrows them by category and by a word", async () => {
  show("/catalog");
  expect(await screen.findByRole("link", { name: "Fluxyr" })).toBeTruthy();
  expect(screen.getByRole("link", { name: "Twenty CRM" })).toBeTruthy();
  expect(screen.getByText("A key")).toBeTruthy();
  expect(screen.getByText("postgres")).toBeTruthy();
  tab("CRM");
  await waitFor(() => expect(screen.queryByRole("link", { name: "Fluxyr" })).toBeNull());
  expect(screen.getByRole("link", { name: "Twenty CRM" })).toBeTruthy();
  tab("All");
  fireEvent.change(screen.getByLabelText("Search the catalog"), { target: { value: "python" } });
  await waitFor(() => expect(screen.queryByRole("link", { name: "Twenty CRM" })).toBeNull());
  expect(screen.getByRole("link", { name: "Fluxyr" })).toBeTruthy();
  fireEvent.change(screen.getByLabelText("Search the catalog"), { target: { value: "nothing like it" } });
  expect(await screen.findByText("Nothing here")).toBeTruthy();
});

it("names a category in words", () => {
  expect(categoryName("crm")).toBe("CRM");
  expect(categoryName("developer-tools")).toBe("Developer tools");
  expect(categoryName("project-management")).toBe("Project management");
});

it("asks what the entry needs, never a generated value, and installs with it", async () => {
  show("/catalog/fluxyr");
  const install = (await screen.findByRole("button", { name: "Install" })) as HTMLButtonElement;
  expect(install.disabled).toBe(true);
  expect(screen.getByText("Needs OpenRouter API key.")).toBeTruthy();
  expect(screen.queryByLabelText(/Vault key/)).toBeNull();
  expect(screen.getByText("# Readme")).toBeTruthy();
  fireEvent.change(screen.getByLabelText(/OpenRouter API key/), { target: { value: "sk-or-1" } });
  fireEvent.change(screen.getByPlaceholderText("fluxyr"), { target: { value: "agent" } });
  await waitFor(() => expect(install.disabled).toBe(false));
  fireEvent.click(install);
  await waitFor(() => expect(fake.installFromCatalog).toHaveBeenCalled());
  const [name, body] = fake.installFromCatalog.mock.calls[0];
  expect(name).toBe("fluxyr");
  expect(body).toMatchObject({ slug: "agent", inputs: { OPENROUTER_API_KEY: "sk-or-1" }, revision: undefined });
  expect(body.inputs.FLUXYR_MODEL).toBeUndefined();
  expect(await screen.findByText("project page")).toBeTruthy();
  expect(toasts.success).toHaveBeenCalled();
});

it("pins a revision when asked to, instead of following the branch", async () => {
  show("/catalog/twenty");
  const install = (await screen.findByRole("button", { name: "Install" })) as HTMLButtonElement;
  expect(install.disabled).toBe(false);
  expect(screen.getByText(/Every new commit of main is built/)).toBeTruthy();
  fireEvent.click(screen.getByRole("combobox", { name: "Updates" }));
  fireEvent.click(await screen.findByRole("option", { name: "Pin a tag or commit" }));
  await waitFor(() => expect(install.disabled).toBe(true));
  fireEvent.change(screen.getByPlaceholderText("v1.2.0"), { target: { value: "v2.0.0" } });
  await waitFor(() => expect(install.disabled).toBe(false));
  fireEvent.click(install);
  await waitFor(() => expect(fake.installFromCatalog).toHaveBeenCalled());
  expect(fake.installFromCatalog.mock.calls[0][1]).toMatchObject({ slug: "twenty", revision: "v2.0.0" });
});
