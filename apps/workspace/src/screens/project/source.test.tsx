import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Project, SourceStatus } from "@/api/types";
import type { Perms } from "@/lib/perms";
import seed from "../../../mock/projects.json";
import { GitSource } from "./source";

const fake = vi.hoisted(() => ({ updateProject: vi.fn(), projectGitHooks: vi.fn(), projectGitAuth: vi.fn() }));
vi.mock("@/api/api", () => ({ api: fake }));
const toasts = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast: toasts }));
let queries: QueryClient;
beforeEach(() => fake.projectGitAuth.mockResolvedValue({ using: "none" }));
afterEach(() => {
  cleanup();
  queries?.clear();
  vi.resetAllMocks();
});

const commit = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678";
const newer = "b2c3d4e5f60718293a4b5c6d7e8f901234567890";
const minutesAgo = (m: number) => new Date(Date.now() - m * 60_000).toISOString();

function projectWith(
  git: { url: string; revision?: string; autoDeploy?: boolean } | undefined,
  source?: SourceStatus,
): Project {
  const p = structuredClone(seed[0]) as unknown as Project;
  p.spec.source = git ? { git } : { blob: { sha256: "e3b0c442", ref: "local checkout" } };
  p.status.source = source;
  return p;
}

function show(project: Project, perms: Partial<Perms> = { config: true }) {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queries}>
      <GitSource project={project} perms={perms as Perms} />
    </QueryClientProvider>,
  );
}

const repo = { url: "https://github.com/acme/hello-world", revision: "main" };

it("shows the commit that runs, its message and author, and when the branch was last checked", () => {
  show(projectWith(repo, { commit, latest: commit, message: "Fix the login redirect\n\nLonger body", author: "alice", via: "push", checkedAt: minutesAgo(3) }));
  expect(screen.getByText("a1b2c3d")).toBeTruthy();
  expect(screen.getByText("Fix the login redirect")).toBeTruthy();
  expect(screen.getByText("by alice")).toBeTruthy();
  expect(screen.getByText("3m ago · via push")).toBeTruthy();
  expect(screen.getByRole("switch", { name: "Auto-deploy" }).getAttribute("aria-checked")).toBe("true");
});

it("says the branch is being read before a commit is pinned", () => {
  show(projectWith(repo, {}));
  expect(screen.getByText("Reading the branch's latest commit.")).toBeTruthy();
  expect(screen.getByText("not yet")).toBeTruthy();
});

it("says the build follows the branch when shpyrd does not watch it", () => {
  show(projectWith({ url: "git://git.example.test/acme/hello-world", revision: "main" }, undefined));
  expect(screen.getByText("The build follows the branch")).toBeTruthy();
  expect(screen.queryByText("Reading the branch's latest commit.")).toBeNull();
});

it("says a skipped commit was not deployed", () => {
  show(projectWith(repo, { commit, latest: newer, skipped: newer, message: "[skip deploy] typo", author: "bruno", via: "push", checkedAt: minutesAgo(1) }));
  expect(screen.getByText("Skipped b2c3d4e: [skip deploy] typo")).toBeTruthy();
  cleanup();
  show(projectWith(repo, { commit, latest: newer, skipped: newer, via: "push", checkedAt: minutesAgo(1) }));
  expect(screen.getByText("Skipped b2c3d4e: a skip marker")).toBeTruthy();
});

it("says the latest commit waits when auto-deploy is off", () => {
  show(projectWith({ ...repo, autoDeploy: false }, { commit, latest: newer, via: "poll", checkedAt: minutesAgo(2) }));
  expect(screen.getByText("Latest on main: b2c3d4e, not deployed: auto-deploy is off.")).toBeTruthy();
  expect(screen.getByRole("switch", { name: "Auto-deploy" }).getAttribute("aria-checked")).toBe("false");
});

it("shows why the branch cannot be read", () => {
  show(projectWith(repo, { commit, error: "shpyrd could not read the repository: it is private.", checkedAt: minutesAgo(1) }));
  expect(screen.getByText("shpyrd could not read the repository: it is private.")).toBeTruthy();
});

it("shows a commit revision as pinned, with nothing to switch", () => {
  show(projectWith({ url: repo.url, revision: "a1b2c3d" }, undefined));
  expect(screen.getByText("Pinned to a commit: a push deploys nothing.")).toBeTruthy();
  expect(screen.queryByRole("switch")).toBeNull();
});

it("renders nothing for a project that does not come from git", () => {
  const { container } = show(projectWith(undefined));
  expect(container.innerHTML).toBe("");
});

it("switches auto-deploy off through the api, and only for those who may change the project", async () => {
  const project = projectWith(repo, { commit, latest: commit, checkedAt: minutesAgo(1) });
  fake.updateProject.mockResolvedValue(projectWith({ ...repo, autoDeploy: false }, project.status.source));
  show(project);
  fireEvent.click(screen.getByRole("switch", { name: "Auto-deploy" }));
  await waitFor(() => expect(fake.updateProject).toHaveBeenCalledWith(project.slug, { autoDeploy: false }));
  cleanup();
  show(project, { config: false });
  expect((screen.getByRole("switch", { name: "Auto-deploy" }) as HTMLButtonElement).disabled).toBe(true);
  expect(screen.queryByRole("button", { name: "Deploy on push" })).toBeNull();
});

it("shows the webhook URL and secret to copy in the dialog", async () => {
  const project = projectWith(repo, { commit, latest: commit, checkedAt: minutesAgo(1) });
  fake.projectGitHooks.mockResolvedValue({ url: "https://acme.shpyrd.app/api/hooks/git/hello-world/t0k3n", secret: "s3cr3t" });
  show(project);
  fireEvent.click(screen.getByRole("button", { name: "Deploy on push" }));
  expect(await screen.findByText("https://acme.shpyrd.app/api/hooks/git/hello-world/t0k3n")).toBeTruthy();
  expect(screen.getByText("s3cr3t")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Copy the secret" })).toBeTruthy();
  expect(fake.projectGitHooks).toHaveBeenCalledWith(project.slug);
});

it("forgets the webhook's secret when the dialog closes", async () => {
  const project = projectWith(repo, { commit, latest: commit, checkedAt: minutesAgo(1) });
  fake.projectGitHooks.mockResolvedValue({ url: "https://acme.shpyrd.app/api/hooks/git/hello-world/t0k3n", secret: "s3cr3t" });
  show(project);
  fireEvent.click(screen.getByRole("button", { name: "Deploy on push" }));
  expect(await screen.findByText("s3cr3t")).toBeTruthy();
  expect(queries.getQueryData(["git-hooks", project.slug])).toBeTruthy();
  fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
  await waitFor(() => expect(screen.queryByText("s3cr3t")).toBeNull());
  expect(queries.getQueryData(["git-hooks", project.slug])).toBeUndefined();
});

it("says when the clipboard cannot be written", async () => {
  const project = projectWith(repo, { commit, latest: commit, checkedAt: minutesAgo(1) });
  fake.projectGitHooks.mockResolvedValue({ url: "https://acme.shpyrd.app/api/hooks/git/hello-world/t0k3n", secret: "s3cr3t" });
  show(project);
  fireEvent.click(screen.getByRole("button", { name: "Deploy on push" }));
  fireEvent.click(await screen.findByRole("button", { name: "Copy the secret" }));
  await waitFor(() => expect(toasts.error).toHaveBeenCalled());
  expect(toasts.success).not.toHaveBeenCalled();

  const writeText = vi.fn().mockRejectedValue(new Error("denied"));
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
  fireEvent.click(screen.getByRole("button", { name: "Copy the URL" }));
  await waitFor(() => expect(toasts.error).toHaveBeenCalledTimes(2));

  writeText.mockResolvedValue(undefined);
  fireEvent.click(screen.getByRole("button", { name: "Copy the URL" }));
  await waitFor(() => expect(toasts.success).toHaveBeenCalledWith("URL copied"));
  Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true });
});

it("says pushes arrive through the GitHub App and offers no webhook to set up", async () => {
  fake.projectGitAuth.mockResolvedValue({ url: repo.url, using: "github-app", githubAccount: "acme" });
  show(projectWith(repo, { commit, latest: commit, via: "push", checkedAt: minutesAgo(1) }));
  expect(await screen.findByText("Through the GitHub App (acme): every push arrives by itself, nothing to set up.")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Deploy on push" })).toBeNull();
});
