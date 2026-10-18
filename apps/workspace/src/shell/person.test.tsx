import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { PersonMenu } from "./person";

const fake = vi.hoisted(() => ({ me: vi.fn(), config: vi.fn(), githubMe: vi.fn(), linkGitHub: vi.fn(), unlinkGitHub: vi.fn(), logout: vi.fn() }));
vi.mock("@/api/api", () => ({ api: fake }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

let queries: QueryClient;
function show() {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queries}>
      <PersonMenu />
    </QueryClientProvider>,
  );
}
async function openMenu() {
  const trigger = await screen.findByRole("button", { name: "Ana Souza" });
  fireEvent.keyDown(trigger, { key: "Enter" });
}
beforeEach(() => {
  fake.me.mockResolvedValue({ subject: "p-1", email: "ana@acme.com", name: "Ana Souza", provider: "github" });
  fake.config.mockResolvedValue({});
});
afterEach(() => {
  cleanup();
  queries?.clear();
  vi.resetAllMocks();
});

it("shows the linked GitHub account and unlinks it once confirmed", async () => {
  fake.githubMe.mockResolvedValue({ login: "paezao", linkedAt: "2026-10-17T10:00:00Z" });
  fake.unlinkGitHub.mockResolvedValue(undefined);
  show();
  await openMenu();
  expect(await screen.findByText("GitHub: paezao")).toBeTruthy();
  fireEvent.click(screen.getByRole("menuitem", { name: /Unlink GitHub/ }));
  expect(await screen.findByText("Unlink GitHub?")).toBeTruthy();
  expect(screen.getByText(/You won't see your private repositories in New project until you link again/)).toBeTruthy();
  expect(fake.unlinkGitHub).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Unlink" }));
  await waitFor(() => expect(fake.unlinkGitHub).toHaveBeenCalled());
});

it("offers to link GitHub when no account is linked", async () => {
  fake.githubMe.mockResolvedValue(null);
  fake.linkGitHub.mockResolvedValue({ authorizeURL: "" });
  show();
  await openMenu();
  fireEvent.click(await screen.findByRole("menuitem", { name: /Link GitHub/ }));
  await waitFor(() => expect(fake.linkGitHub).toHaveBeenCalled());
});

it("does not unlink when the question is cancelled", async () => {
  fake.githubMe.mockResolvedValue({ login: "paezao", linkedAt: "2026-10-17T10:00:00Z" });
  show();
  await openMenu();
  fireEvent.click(await screen.findByRole("menuitem", { name: /Unlink GitHub/ }));
  fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));
  expect(fake.unlinkGitHub).not.toHaveBeenCalled();
});

it("offers nothing of GitHub to the admin token", async () => {
  fake.me.mockResolvedValue({ subject: "admin-token", provider: "token", name: "Ana Souza" });
  show();
  await openMenu();
  await screen.findByRole("menuitem", { name: /Sign out/ });
  expect(screen.queryByRole("menuitem", { name: /GitHub/ })).toBeNull();
  expect(fake.githubMe).not.toHaveBeenCalled();
});
