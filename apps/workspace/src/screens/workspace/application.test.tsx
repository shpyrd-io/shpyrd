import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { RegistryToken, WorkspaceInfo } from "@/api/types";
import { Application } from "./application";

const fake = vi.hoisted(() => ({
  workspace: vi.fn(),
  updateWorkspace: vi.fn(),
  reloadWorkspaceUI: vi.fn(),
  registryTokens: vi.fn(),
  setRegistryToken: vi.fn(),
  unsetRegistryToken: vi.fn(),
}));
vi.mock("@/api/api", () => ({ api: fake }));
const perms = vi.hoisted(() => ({ admin: true }));
vi.mock("@/lib/perms", () => ({ usePerms: () => ({ loaded: true, enforced: true, admin: perms.admin }) }));
const toasts = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast: toasts }));

const platformOwn: WorkspaceInfo = {
  slug: "acme",
  name: "Acme",
  ownedByOperator: false,
  joinPolicy: "open",
  ownMethodsOnly: false,
  mcpName: "Acme on shpyrd",
  mcpUrl: "https://acme.shpyrd.test/mcp",
  owners: [],
  createdAt: new Date().toISOString(),
  updatedAt: new Date().toISOString(),
};
const ref = "oci://ghcr.io/acme/workspace-ui@sha256:" + "0".repeat(64);
const ownApp: WorkspaceInfo = {
  ...platformOwn,
  ui: { ref, setBy: "ana@acme.test", setAt: new Date().toISOString(), status: { state: "serving", at: new Date().toISOString(), name: "Acme", version: "2.3.1" } },
};
const credential: RegistryToken = { host: "ghcr.io", username: "acme-bot", updatedBy: "ana@acme.test", updatedAt: new Date().toISOString() };

let queries: QueryClient;
function show() {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queries}>
      <Application />
    </QueryClientProvider>,
  );
}
beforeEach(() => {
  perms.admin = true;
  fake.workspace.mockResolvedValue(platformOwn);
  fake.registryTokens.mockResolvedValue([]);
});
afterEach(() => {
  cleanup();
  queries?.clear();
  vi.resetAllMocks();
});

it("says the door serves the platform's own application when nothing is chosen", async () => {
  show();
  expect(await screen.findByText("The platform's own application.")).toBeTruthy();
  expect(screen.getByText("None. Public repositories need none.")).toBeTruthy();
  expect(screen.queryByText("Back to the platform's own")).toBeNull();
});

it("shows the chosen application, its package, who set it and that it is serving", async () => {
  fake.workspace.mockResolvedValue(ownApp);
  show();
  expect(await screen.findByText("Serving")).toBeTruthy();
  expect(screen.getByText("Acme 2.3.1")).toBeTruthy();
  expect(screen.getByText(/set by ana@acme\.test/)).toBeTruthy();
  expect((screen.getByPlaceholderText("oci://ghcr.io/acme/workspace-ui:2.3.1") as HTMLInputElement).value).toBe(ref);
});

it("shows why an application is not served", async () => {
  fake.workspace.mockResolvedValue({ ...ownApp, ui: { ref, status: { state: "failed", error: "ghcr.io refused the pull as unauthorized" } } });
  show();
  expect(await screen.findByText("Not served")).toBeTruthy();
  expect(screen.getByText(/ghcr\.io refused the pull as unauthorized/)).toBeTruthy();
});

it("sets a reference an admin types, and goes back to the platform's own", async () => {
  fake.workspace.mockResolvedValue(ownApp);
  fake.updateWorkspace.mockResolvedValue(ownApp);
  show();
  await screen.findByText("Serving");
  const input = screen.getByPlaceholderText("oci://ghcr.io/acme/workspace-ui:2.3.1");
  fireEvent.change(input, { target: { value: "oci://ghcr.io/acme/workspace-ui:2.4.0" } });
  fireEvent.click(screen.getByRole("button", { name: "Serve it" }));
  await waitFor(() => expect(fake.updateWorkspace).toHaveBeenCalledWith({ ui: "oci://ghcr.io/acme/workspace-ui:2.4.0" }));
  fireEvent.click(screen.getByRole("button", { name: "Back to the platform's own" }));
  await waitFor(() => expect(fake.updateWorkspace).toHaveBeenCalledWith({ ui: "" }));
  fireEvent.click(screen.getByRole("button", { name: "Load again" }));
  await waitFor(() => expect(fake.reloadWorkspaceUI).toHaveBeenCalled());
});

it("lists the credentials by host without their values, and removes one", async () => {
  fake.registryTokens.mockResolvedValue([credential]);
  fake.unsetRegistryToken.mockResolvedValue(undefined);
  show();
  expect(await screen.findByText("acme-bot")).toBeTruthy();
  expect(screen.getByText(/ana@acme\.test/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Remove the credential for ghcr.io" }));
  fireEvent.click(await screen.findByRole("button", { name: "Remove" }));
  await waitFor(() => expect(fake.unsetRegistryToken).toHaveBeenCalledWith("ghcr.io"));
});

it("adds a credential for a host from the dialog", async () => {
  fake.setRegistryToken.mockResolvedValue(credential);
  show();
  await screen.findByText("None. Public repositories need none.");
  fireEvent.click(screen.getByRole("button", { name: "Add credential" }));
  fireEvent.change(await screen.findByPlaceholderText("ghcr.io"), { target: { value: "GHCR.io" } });
  fireEvent.change(screen.getByPlaceholderText("acme-bot"), { target: { value: "acme-bot" } });
  fireEvent.change(screen.getByLabelText("Token"), { target: { value: "ghp_secret" } });
  fireEvent.click(screen.getByRole("button", { name: "Add" }));
  await waitFor(() => expect(fake.setRegistryToken).toHaveBeenCalledWith("ghcr.io", { username: "acme-bot", token: "ghp_secret" }));
});

it("offers a reader nothing to change", async () => {
  perms.admin = false;
  fake.workspace.mockResolvedValue(ownApp);
  show();
  await screen.findByText("Serving");
  expect(screen.queryByRole("button", { name: "Serve it" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Add credential" })).toBeNull();
});
