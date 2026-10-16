import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import type { GitHubRepository } from "@/api/types";
import { NewProject } from "./launcher";

const fake = vi.hoisted(() => ({
  config: vi.fn(),
  githubRepositories: vi.fn(),
  githubBranches: vi.fn(),
  createProject: vi.fn(),
}));
vi.mock("@/api/api", async (actual) => ({ ...(await actual<typeof import("@/api/api")>()), api: fake }));
vi.mock("@/lib/networking", () => ({ useInternalExposure: () => false }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const repos: GitHubRepository[] = [
  { installation: 42, fullName: "acme/hello-world", private: false, defaultBranch: "main" },
  { installation: 42, fullName: "acme/billing", private: true, defaultBranch: "trunk" },
];

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
  fireEvent.click(screen.getByRole("button", { name: /acme\/billing/ }));
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
  fireEvent.click(screen.getByRole("button", { name: /acme\/billing/ }));
  expect(create().disabled).toBe(false);
  queries.setQueryData(["github-repositories"], [repos[0]]);
  await waitFor(() => expect(create().disabled).toBe(true));
  // Back in the list, it is not picked again by itself.
  queries.setQueryData(["github-repositories"], repos);
  await waitFor(() => expect(screen.getByRole("button", { name: /acme\/billing/ })).toBeTruthy());
  expect(create().disabled).toBe(true);
});
