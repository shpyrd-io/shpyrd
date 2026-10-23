import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { ApiError } from "@shpyrd/shared/api/error";
import type { GitHubRepository } from "@/api/types";
import { NewProject } from "./launcher";

const fake = vi.hoisted(() => ({
  config: vi.fn(),
  me: vi.fn(),
  githubAccounts: vi.fn(),
  githubRepositories: vi.fn(),
  githubBranches: vi.fn(),
  githubMe: vi.fn(),
  linkGitHub: vi.fn(),
  createProject: vi.fn(),
}));
vi.mock("@/api/api", async (actual) => ({ ...(await actual<typeof import("@/api/api")>()), api: fake }));
vi.mock("@/lib/networking", () => ({ useInternalExposure: () => false }));
vi.mock("@/lib/perms", async (actual) => ({ ...(await actual<typeof import("@/lib/perms")>()), usePerms: () => ({ admin: false, create: true }) }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const repos: GitHubRepository[] = [
  { installation: 42, fullName: "acme/hello-world", private: false, defaultBranch: "main" },
  { installation: 42, fullName: "acme/billing", private: true, defaultBranch: "trunk" },
];
const accounts = { connected: 1, accounts: [{ installation: 42, account: "acme", type: "Organization", suspended: false, manageURL: "https://github.com/organizations/acme/settings/installations/42" }] };

let queries: QueryClient;
async function open() {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queries}>
      <MemoryRouter>
        <NewProject asButton />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "New project" }));
  await screen.findByRole("tab", { name: "From GitHub" });
}
const create = () => screen.getByRole("button", { name: "Create" }) as HTMLButtonElement;
// Radix tabs switch on the mouse going down.
const tab = (name: string) => fireEvent.mouseDown(screen.getByRole("tab", { name }), { button: 0 });

beforeEach(() => {
  fake.config.mockResolvedValue({ domain: "example.test" });
  fake.githubAccounts.mockResolvedValue(accounts);
  fake.githubRepositories.mockResolvedValue(repos);
  fake.githubBranches.mockResolvedValue(["main"]);
  fake.createProject.mockImplementation(async (p: { slug: string }) => ({ slug: p.slug }));
});
afterEach(() => {
  cleanup();
  queries?.clear();
  vi.resetAllMocks();
});

it("keeps Create off on From GitHub until a repository is picked", async () => {
  await open();
  fireEvent.change(screen.getByPlaceholderText("hello-world"), { target: { value: "shop" } });
  expect(create().disabled).toBe(true);
  fireEvent.click(await screen.findByRole("button", { name: /acme\/billing/ }));
  expect(create().disabled).toBe(false);
});

it("sends the directory only from the Git URL tab", async () => {
  await open();
  tab("Git URL");
  fireEvent.change(await screen.findByPlaceholderText("https://github.com/acme/hello-world"), { target: { value: "https://github.com/acme/other" } });
  fireEvent.change(await screen.findByPlaceholderText("services/api"), { target: { value: "services/api" } });
  tab("From GitHub");
  fireEvent.click(await screen.findByRole("button", { name: /acme\/billing/ }));
  fireEvent.click(create());
  await waitFor(() => expect(fake.createProject).toHaveBeenCalled());
  const sent = fake.createProject.mock.calls[0][0];
  expect(sent.git).toEqual({ url: "https://github.com/acme/billing", revision: "trunk" });
  expect(sent.subPath).toBeUndefined();
});

it("sends the directory from the Git URL tab", async () => {
  await open();
  tab("Git URL");
  fireEvent.change(screen.getByPlaceholderText("hello-world"), { target: { value: "shop" } });
  fireEvent.change(await screen.findByPlaceholderText("https://github.com/acme/hello-world"), { target: { value: "https://github.com/acme/other" } });
  fireEvent.change(await screen.findByPlaceholderText("services/api"), { target: { value: "services/api" } });
  fireEvent.click(create());
  await waitFor(() => expect(fake.createProject).toHaveBeenCalled());
  expect(fake.createProject.mock.calls[0][0]).toMatchObject({ git: { url: "https://github.com/acme/other", revision: "main" }, subPath: "services/api" });
});

it("forgets a picked repository the list no longer has", async () => {
  await open();
  fireEvent.click(await screen.findByRole("button", { name: /acme\/billing/ }));
  expect(create().disabled).toBe(false);
  queries.setQueryData(["github-repositories", 42], [repos[0]]);
  await waitFor(() => expect(create().disabled).toBe(true));
  // Back in the list, it is not picked again by itself.
  queries.setQueryData(["github-repositories", 42], repos);
  await waitFor(() => expect(screen.getByRole("button", { name: /acme\/billing/ })).toBeTruthy());
  expect(create().disabled).toBe(true);
});

it("asks to continue with GitHub without a link, and keeps what was typed once linked", async () => {
  fake.githubAccounts.mockRejectedValueOnce(new ApiError(409, "Link your GitHub account first", "github_link_required"));
  fake.linkGitHub.mockResolvedValue({ authorizeURL: "" });
  await open();
  fireEvent.change(screen.getByPlaceholderText("hello-world"), { target: { value: "shop" } });
  fireEvent.click(await screen.findByRole("button", { name: "Continue with GitHub" }));
  expect(await screen.findByRole("button", { name: /acme\/billing/ })).toBeTruthy();
  expect((screen.getByPlaceholderText("hello-world") as HTMLInputElement).value).toBe("shop");
});

it("offers no GitHub where the workspace has no GitHub account", async () => {
  fake.githubAccounts.mockResolvedValue({ connected: 0, accounts: [] });
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queries}>
      <MemoryRouter>
        <NewProject asButton />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "New project" }));
  expect(await screen.findByPlaceholderText("https://github.com/acme/hello-world")).toBeTruthy();
  expect(screen.queryByRole("tab", { name: "From GitHub" })).toBeNull();
});
