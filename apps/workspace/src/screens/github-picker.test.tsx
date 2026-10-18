import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import type { GitHubAccount, GitHubRepository } from "@/api/types";
import { branchesInOrder, GitHubLinkStep, GitHubPicker, type Picked, slugFromRepo } from "./github-picker";

const fake = vi.hoisted(() => ({ githubBranches: vi.fn(), githubRepositories: vi.fn(), connectGitHub: vi.fn(), linkGitHub: vi.fn(), githubMe: vi.fn() }));
vi.mock("@/api/api", () => ({ api: fake }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const acme: GitHubAccount = { installation: 42, account: "acme", type: "Organization", suspended: false, manageURL: "https://github.com/organizations/acme/settings/installations/42" };
const labs: GitHubAccount = { installation: 77, account: "acme-labs", type: "Organization", suspended: false, manageURL: "https://github.com/organizations/acme-labs/settings/installations/77" };
const repos: Record<number, GitHubRepository[]> = {
  42: [
    { installation: 42, fullName: "acme/hello-world", private: false, defaultBranch: "main" },
    { installation: 42, fullName: "acme/billing", private: true, defaultBranch: "trunk" },
  ],
  77: [{ installation: 77, fullName: "acme-labs/ml", private: true, defaultBranch: "main" }],
};

let queries: QueryClient;
let last: Picked | undefined;
function Harness({ accounts, canAdd = true }: { accounts: GitHubAccount[]; canAdd?: boolean }) {
  const [value, setValue] = useState<Picked>();
  last = value;
  return <GitHubPicker accounts={accounts} value={value} onChange={setValue} canAdd={canAdd} />;
}
function show(accounts: GitHubAccount[] = [acme], canAdd = true) {
  fake.githubRepositories.mockImplementation(async (installation: number) => repos[installation] ?? []);
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queries}>
      <Harness accounts={accounts} canAdd={canAdd} />
    </QueryClientProvider>,
  );
}
// Radix's Select scrolls its options into view; jsdom has no scrolling.
beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: vi.fn() });
});
afterEach(() => {
  cleanup();
  queries?.clear();
  vi.restoreAllMocks();
  vi.resetAllMocks();
  last = undefined;
});

it("lists the repositories of the account and filters them as one types, in any case", async () => {
  show();
  expect(await screen.findByRole("button", { name: /acme\/billing/ })).toBeTruthy();
  expect(fake.githubRepositories).toHaveBeenCalledWith(42);
  fireEvent.change(screen.getByRole("textbox", { name: "Filter the repositories" }), { target: { value: "BILL" } });
  expect(screen.queryByRole("button", { name: /acme\/hello-world/ })).toBeNull();
  fireEvent.change(screen.getByRole("textbox", { name: "Filter the repositories" }), { target: { value: "nothing" } });
  expect(screen.getByText("No repository matches.")).toBeTruthy();
});

it("picks a repository on its default branch, then offers its branches with the default first", async () => {
  fake.githubBranches.mockResolvedValue(["feature", "trunk", "hotfix"]);
  show();
  fireEvent.click(await screen.findByRole("button", { name: /acme\/billing/ }));
  expect(last).toEqual({ fullName: "acme/billing", branch: "trunk" });
  await waitFor(() => expect(fake.githubBranches).toHaveBeenCalledWith("acme/billing"));
  expect(await screen.findByRole("combobox", { name: "Branch" })).toBeTruthy();
});

it("switches the account, and lists that account's repositories with nothing picked", async () => {
  show([acme, labs]);
  fireEvent.click(await screen.findByRole("button", { name: /acme\/billing/ }));
  expect(last?.fullName).toBe("acme/billing");
  fireEvent.click(screen.getByRole("combobox", { name: "GitHub account" }));
  fireEvent.click(await screen.findByRole("option", { name: "acme-labs" }));
  expect(await screen.findByRole("button", { name: /acme-labs\/ml/ })).toBeTruthy();
  expect(fake.githubRepositories).toHaveBeenCalledWith(77);
  expect(last).toBeUndefined();
});

it("sends one who cannot find a repository to the installation's settings on GitHub", async () => {
  const open = vi.spyOn(window, "open").mockReturnValue(null);
  show();
  fireEvent.click(await screen.findByRole("button", { name: "Can't find a repository?" }));
  expect(open).toHaveBeenCalledWith(acme.manageURL, "_blank", "noopener");
});

it("adds an account through Connect for a workspace admin, and says who adds them to anyone else", async () => {
  fake.connectGitHub.mockResolvedValue({ installURL: "https://github.test/apps/shpyrd/installations/new?state=s", authorizeURL: "" });
  const open = vi.spyOn(window, "open").mockReturnValue(null);
  show([acme], true);
  fireEvent.click(await screen.findByRole("button", { name: "Add GitHub account or organisation" }));
  await waitFor(() => expect(open).toHaveBeenCalledWith("https://github.test/apps/shpyrd/installations/new?state=s", "shpyrd-github-install", expect.stringContaining("popup")));
  cleanup();
  show([acme], false);
  expect(await screen.findByText("A workspace admin adds accounts in Workspace settings → Git.")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Add GitHub account or organisation" })).toBeNull();
});

it("says so when the person's GitHub reads none of the workspace's accounts", async () => {
  show([]);
  expect(await screen.findByText(/Your GitHub account cannot read any of this workspace's GitHub accounts/)).toBeTruthy();
});

it("asks to continue with GitHub, and says when the link is done", async () => {
  fake.linkGitHub.mockResolvedValue({ authorizeURL: "" });
  const onLinked = vi.fn();
  queries = new QueryClient();
  render(
    <QueryClientProvider client={queries}>
      <GitHubLinkStep onLinked={onLinked} />
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Continue with GitHub" }));
  await waitFor(() => expect(onLinked).toHaveBeenCalled());
});

it("says so when the server cut the list, and not otherwise", async () => {
  Object.assign(repos[42], { truncated: true });
  show();
  expect(await screen.findByText("Showing the first 2. Filter to narrow.")).toBeTruthy();
  cleanup();
  delete (repos[42] as { truncated?: boolean }).truncated;
  show();
  await screen.findByRole("button", { name: /acme\/billing/ });
  expect(screen.queryByText(/Filter to narrow/)).toBeNull();
});

it("says so when the server cut the branches", async () => {
  fake.githubBranches.mockResolvedValue(Object.assign(["main", "dev"], { truncated: true }));
  show();
  fireEvent.click(await screen.findByRole("button", { name: /acme\/billing/ }));
  expect(await screen.findByText("Showing the first 2. Filter to narrow.")).toBeTruthy();
});

it("orders the branches with the default first, once", () => {
  expect(branchesInOrder(["dev", "main", "release"], "main")).toEqual(["main", "dev", "release"]);
  expect(branchesInOrder([], "main")).toEqual(["main"]);
});

it("makes a slug from the repository's name", () => {
  expect(slugFromRepo("Acme/Hello_World.js")).toBe("hello-world-js");
  expect(slugFromRepo("acme/shop")).toBe("shop");
});
