import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { AddMenu } from "./launcher";

const fake = vi.hoisted(() => ({
  config: vi.fn(),
  githubAccounts: vi.fn(),
  githubRepositories: vi.fn(),
  githubBranches: vi.fn(),
  githubMe: vi.fn(),
}));
vi.mock("@/api/api", async (actual) => ({ ...(await actual<typeof import("@/api/api")>()), api: fake }));
vi.mock("@/lib/networking", () => ({ useInternalExposure: () => false }));
vi.mock("@/lib/perms", async (actual) => ({ ...(await actual<typeof import("@/lib/perms")>()), usePerms: () => ({ admin: false, create: true }) }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

let queries: QueryClient;
function show() {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queries}>
      <MemoryRouter>
        <Routes>
          <Route path="/" element={<AddMenu />} />
          <Route path="/catalog" element={<p>the catalog</p>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  // Radix menus open on the pointer going down.
  fireEvent.pointerDown(screen.getByRole("button", { name: "New project" }), { button: 0, ctrlKey: false });
}
// A Radix menu item acts on the pointer going up over it, or on Enter.
const pick = async (name: string) => {
  const item = await screen.findByRole("menuitem", { name });
  fireEvent.keyDown(item, { key: "Enter" });
};

beforeEach(() => {
  fake.config.mockResolvedValue({ domain: "example.test", catalog: true });
  fake.githubAccounts.mockResolvedValue({ connected: 1, accounts: [{ installation: 42, account: "acme", type: "Organization", suspended: false, manageURL: "" }] });
  fake.githubRepositories.mockResolvedValue([]);
  fake.githubBranches.mockResolvedValue(["main"]);
});
afterEach(() => {
  cleanup();
  queries?.clear();
  vi.resetAllMocks();
});

it("offers an empty project, GitHub and the catalog", async () => {
  show();
  expect(await screen.findByRole("menuitem", { name: "Empty project" })).toBeTruthy();
  expect(screen.getByRole("menuitem", { name: "From GitHub" })).toBeTruthy();
  await pick("From templates");
  expect(await screen.findByText("the catalog")).toBeTruthy();
});

it("opens the dialog on the tab picked", async () => {
  show();
  await pick("From GitHub");
  expect(await screen.findByRole("dialog", { name: "New project" })).toBeTruthy();
  expect((await screen.findByRole("tab", { name: "From GitHub" })).getAttribute("aria-selected")).toBe("true");
});

it("opens the dialog on the Git URL tab for an empty project", async () => {
  show();
  await pick("Empty project");
  expect(await screen.findByRole("dialog", { name: "New project" })).toBeTruthy();
  expect((await screen.findByRole("tab", { name: "Git URL" })).getAttribute("aria-selected")).toBe("true");
});
