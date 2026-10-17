import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import type { GitHubRepository } from "@/api/types";
import { branchesInOrder, GitHubPicker, type Picked, slugFromRepo } from "./github-picker";

const fake = vi.hoisted(() => ({ githubBranches: vi.fn() }));
vi.mock("@/api/api", () => ({ api: fake }));

const repos: GitHubRepository[] = [
  { installation: 42, fullName: "acme/hello-world", private: false, defaultBranch: "main" },
  { installation: 42, fullName: "acme/billing", private: true, defaultBranch: "trunk" },
];

let queries: QueryClient;
let last: Picked | undefined;
function Harness() {
  const [value, setValue] = useState<Picked>();
  last = value;
  return <GitHubPicker repositories={repos} value={value} onChange={setValue} />;
}
function show() {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queries}>
      <Harness />
    </QueryClientProvider>,
  );
}
afterEach(() => {
  cleanup();
  queries?.clear();
  vi.resetAllMocks();
  last = undefined;
});

it("filters the repositories as one types, in any case", () => {
  show();
  fireEvent.change(screen.getByRole("textbox", { name: "Filter the repositories" }), { target: { value: "BILL" } });
  expect(screen.getByRole("button", { name: /acme\/billing/ })).toBeTruthy();
  expect(screen.queryByRole("button", { name: /acme\/hello-world/ })).toBeNull();
  fireEvent.change(screen.getByRole("textbox", { name: "Filter the repositories" }), { target: { value: "nothing" } });
  expect(screen.getByText("No repository matches.")).toBeTruthy();
});

it("picks a repository on its default branch, then offers its branches with the default first", async () => {
  fake.githubBranches.mockResolvedValue(["feature", "trunk", "hotfix"]);
  show();
  fireEvent.click(screen.getByRole("button", { name: /acme\/billing/ }));
  expect(last).toEqual({ fullName: "acme/billing", branch: "trunk" });
  await waitFor(() => expect(fake.githubBranches).toHaveBeenCalledWith("acme/billing"));
  expect(await screen.findByRole("combobox", { name: "Branch" })).toBeTruthy();
});

it("shows why the branches cannot be listed", async () => {
  fake.githubBranches.mockRejectedValue(new Error("The GitHub connection on acme does not include billing: add it to the installation on GitHub"));
  show();
  fireEvent.click(screen.getByRole("button", { name: /acme\/billing/ }));
  expect(await screen.findByText(/does not include billing/)).toBeTruthy();
});

it("orders the branches with the default first, once", () => {
  expect(branchesInOrder(["dev", "main", "release"], "main")).toEqual(["main", "dev", "release"]);
  expect(branchesInOrder([], "main")).toEqual(["main"]);
});

it("makes a slug from the repository's name", () => {
  expect(slugFromRepo("Acme/Hello_World.js")).toBe("hello-world-js");
  expect(slugFromRepo("acme/shop")).toBe("shop");
});
