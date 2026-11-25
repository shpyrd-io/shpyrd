import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { ApiError } from "@shpyrd/shared/api/error";
import type { GitHubRepository, ProjectCard } from "@/api/types";
import { Launcher, NewProject } from "./launcher";

const fake = vi.hoisted(() => ({
  config: vi.fn(),
  me: vi.fn(),
  githubAccounts: vi.fn(),
  githubRepositories: vi.fn(),
  githubBranches: vi.fn(),
  githubMe: vi.fn(),
  linkGitHub: vi.fn(),
  createProject: vi.fn(),
  projects: vi.fn(),
}));
// What usePerms answers: the tests of the launcher set it per person.
const person = vi.hoisted(() => ({ perms: {} as Record<string, unknown> }));
const operator = { admin: false, create: true };
vi.mock("@/api/api", async (actual) => ({ ...(await actual<typeof import("@/api/api")>()), api: fake }));
vi.mock("@/lib/networking", () => ({ useInternalExposure: () => false }));
vi.mock("@/lib/perms", async (actual) => ({ ...(await actual<typeof import("@/lib/perms")>()), usePerms: () => person.perms }));
vi.mock("@/shell/person", () => ({ PersonMenu: () => null, ThemeButton: () => null }));
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
  person.perms = operator;
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

// The launcher: the card of every project the person may open, and the gear
// of those they may view.
const card = (slug: string, more: Partial<ProjectCard> = {}): ProjectCard => ({
  slug,
  displayName: slug.toUpperCase(),
  description: `The ${slug} app`,
  phase: "Running",
  access: "authenticated",
  url: `https://${slug}.example.test`,
  ...more,
});
const gear = (name: string) => screen.queryByRole("button", { name: `Settings of ${name}` });

function showLauncher(projects: ProjectCard[]) {
  fake.config.mockResolvedValue({ domain: "example.test", workspace: { name: "Acme" } });
  fake.projects.mockResolvedValue(projects);
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queries}>
      <MemoryRouter>
        <Launcher />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

it("shows the card of an app the person only opens, with its link and without a gear", async () => {
  person.perms = { enforced: true, admin: false, create: false, me: { roles: { workspace: "", projects: {} } } };
  showLauncher([
    card("shop", { role: "user", description: "Orders and returns", domain: "shop.acme.test", url: "https://shop.example.test", icon: "briefcase", iconColor: "teal", exposure: "internal" }),
    card("blog", { role: "reader", iconUrl: "/api/projects/blog/icon?v=1", iconType: "image/png" }),
  ]);
  const link = await screen.findByRole("link", { name: "shop.acme.test" });
  expect(link.getAttribute("href")).toBe("https://shop.acme.test/.shpyrd/signin?rd=%2F");
  expect(screen.getByText("SHOP")).toBeTruthy();
  expect(screen.getByText("Orders and returns")).toBeTruthy();
  expect(screen.getByRole("link", { name: "blog.example.test" }).getAttribute("href")).toBe("https://blog.example.test/.shpyrd/signin?rd=%2F");
  expect(gear("SHOP")).toBeNull();
  expect(gear("BLOG")).toBeNull();
  expect(screen.queryByRole("button", { name: "New project" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Settings of the workspace" })).toBeNull();
});

it("gives the gear of a project only where the person may view it", async () => {
  // A member of the workspace: user on A, developer on B (whose card says no role).
  person.perms = { enforced: true, admin: false, create: true, me: { roles: { workspace: "member", projects: {} } } };
  showLauncher([card("a", { role: "user" }), card("b")]);
  await screen.findByRole("link", { name: "a.example.test" });
  expect(gear("A")).toBeNull();
  expect(gear("B")).toBeTruthy();
  expect(screen.getByRole("button", { name: "New project" })).toBeTruthy();
});

it("gives every card its gear to an administrator, and while roles are not enforced", async () => {
  person.perms = { enforced: true, admin: true, create: true };
  showLauncher([card("a", { role: "user" }), card("b")]);
  await screen.findByRole("link", { name: "a.example.test" });
  expect(gear("A")).toBeTruthy();
  expect(gear("B")).toBeTruthy();
  cleanup();
  person.perms = { enforced: false, admin: false, create: true };
  showLauncher([card("a", { role: "user" }), card("b")]);
  await screen.findByRole("link", { name: "a.example.test" });
  expect(gear("A")).toBeTruthy();
});

it("lights a card as a viewer's is: asleep and in maintenance", async () => {
  person.perms = { enforced: true, admin: false, create: false, me: { roles: { projects: {} } } };
  showLauncher([
    card("asleep", { role: "user", asleep: true }),
    card("down", { role: "user", maintenance: { enabled: true } }),
    card("broken", { role: "reader", phase: "Failed" }),
    card("up", { role: "reader" }),
  ]);
  await screen.findByRole("link", { name: "up.example.test" });
  const phaseOf = (host: string) => screen.getByRole("link", { name: host }).closest("[data-slot=launcher-card]")?.getAttribute("data-phase");
  expect(phaseOf("asleep.example.test")).toBe("sleeping");
  expect(phaseOf("down.example.test")).toBe("maintenance");
  expect(phaseOf("broken.example.test")).toBe("failed");
  expect(phaseOf("up.example.test")).toBe("running");
});
