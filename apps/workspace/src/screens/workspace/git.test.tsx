import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { GitHubConnections } from "@/api/types";
import { Git } from "./git";

const fake = vi.hoisted(() => ({ github: vi.fn(), connectGitHub: vi.fn(), disconnectGitHub: vi.fn() }));
vi.mock("@/api/api", () => ({ api: fake }));
const went = vi.hoisted(() => ({ go: vi.fn() }));
vi.mock("@/lib/go", () => went);
const perms = vi.hoisted(() => ({ admin: true }));
vi.mock("@/lib/perms", () => ({ usePerms: () => ({ loaded: true, enforced: true, admin: perms.admin }) }));
const toasts = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast: toasts }));

const connected: GitHubConnections = {
  configured: true,
  appURL: "https://github.com/apps/shpyrd-acme",
  installations: [
    { id: 42, account: "acme", type: "Organization", connectedBy: "ana@acme.test", createdAt: new Date().toISOString(), suspended: false, manageURL: "https://github.com/organizations/acme/settings/installations/42" },
    { id: 7, account: "ana", type: "User", connectedBy: "ana@acme.test", createdAt: new Date().toISOString(), suspended: true, manageURL: "https://github.com/settings/installations/7" },
  ],
};

let queries: QueryClient;
function show() {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queries}>
      <Git />
    </QueryClientProvider>,
  );
}
beforeEach(() => {
  perms.admin = true;
  fake.github.mockResolvedValue(connected);
});
afterEach(() => {
  cleanup();
  queries?.clear();
  vi.resetAllMocks();
});

it("lists each connection with its kind, who connected it, whether it is suspended, and a way to manage it on GitHub", async () => {
  show();
  expect(await screen.findByText("acme")).toBeTruthy();
  expect(screen.getByText("Organisation")).toBeTruthy();
  expect(screen.getByText("Personal account")).toBeTruthy();
  expect(screen.getByText("Suspended")).toBeTruthy();
  expect(screen.getAllByText(/ana@acme\.test/)).toHaveLength(2);
  const manage = screen.getAllByRole("link", { name: "Manage on GitHub" });
  expect(manage[0].getAttribute("href")).toBe("https://github.com/organizations/acme/settings/installations/42");
});

const urls = {
  installURL: "https://github.com/apps/shpyrd-acme/installations/new?state=s",
  authorizeURL: "https://github.com/login/oauth/authorize?client_id=Iv1.acme&state=s",
};

it("sends an admin's browser to GitHub's authorize page from Connect GitHub", async () => {
  fake.connectGitHub.mockResolvedValue(urls);
  show();
  fireEvent.click(await screen.findByRole("button", { name: "Connect GitHub" }));
  await waitFor(() => expect(went.go).toHaveBeenCalledWith(urls.authorizeURL));
  expect(went.go).toHaveBeenCalledTimes(1);
});

it("sends an admin's browser to GitHub's install page from Install the App on another account", async () => {
  fake.connectGitHub.mockResolvedValue(urls);
  show();
  fireEvent.click(await screen.findByRole("button", { name: "Install the App on another account" }));
  await waitFor(() => expect(went.go).toHaveBeenCalledWith(urls.installURL));
  expect(fake.connectGitHub).toHaveBeenCalledTimes(1);
  expect(went.go).toHaveBeenCalledTimes(1);
});

it("asks before disconnecting, then disconnects", async () => {
  fake.disconnectGitHub.mockResolvedValue(undefined);
  show();
  fireEvent.click(await screen.findByRole("button", { name: "Disconnect acme" }));
  expect(fake.disconnectGitHub).not.toHaveBeenCalled();
  fireEvent.click(await screen.findByRole("button", { name: "Disconnect" }));
  await waitFor(() => expect(fake.disconnectGitHub).toHaveBeenCalledWith(42));
  await waitFor(() => expect(toasts.success).toHaveBeenCalled());
});

it("says GitHub is not set up, in the server's words, and offers nothing to click", async () => {
  fake.github.mockResolvedValue({ configured: false, note: "GitHub is not set up on this platform. An operator sets it up with `shpyrd-ctl github app create`.", installations: [] });
  show();
  expect(await screen.findByText(/An operator sets it up with/)).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Connect GitHub" })).toBeNull();
});

it("sets the commands in the server's note as code", async () => {
  fake.github.mockResolvedValue({ configured: false, note: "GitHub is not set up on this platform. An operator sets it up with `shpyrd-ctl github app create`.", installations: [] });
  show();
  const code = await screen.findByText("shpyrd-ctl github app create");
  expect(code.tagName).toBe("CODE");
  expect(code.getAttribute("data-slot")).toBe("inline-code");
  expect(screen.queryByText(/`/)).toBeNull();
});

it("shows someone who is not an admin the connections without Connect or Disconnect", async () => {
  perms.admin = false;
  show();
  expect(await screen.findByText("acme")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Connect GitHub" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Install the App on another account" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Disconnect acme" })).toBeNull();
});
