import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import type { ReactNode } from "react";
import type { Project } from "@/api/types";
import type { Perms } from "@/lib/perms";
import seed from "../../../mock/projects.json";
import { Heading } from "./heading";

const fake = vi.hoisted(() => ({ streamLogs: vi.fn() }));
vi.mock("@/api/api", () => ({ api: fake }));
vi.mock("@/lib/networking", () => ({ useInternalExposure: () => false }));
const project = seed[0] as unknown as Project;
const perms = { deploy: true, config: false, destroy: false } as Perms;
let queries: QueryClient;
function show(children: ReactNode) {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={queries}><MemoryRouter>{children}</MemoryRouter></QueryClientProvider>);
}
beforeEach(() => { vi.clearAllMocks(); fake.streamLogs.mockResolvedValue(undefined); });
afterEach(() => { cleanup(); queries?.clear(); });


describe("release progress in the project heading", () => {
  it.each([
    "Waiting for room: a new machine is being added to the cluster. Waiting 2m0s.",
    "Downloading the app's image (1.9 GB). Waiting 1m0s.",
    "Running migrate before the new release starts.",
  ])("shows the server's current diagnostic: %s", (message) => {
    const deploying = { ...project, status: { ...project.status, phase: "Deploying", release: { target: "new", state: "Running", message, job: "release-new" } } } as Project;
    show(<Heading project={deploying} perms={perms} />);
    expect(screen.getByText(message)).toBeTruthy();
    expect(screen.getByText("Release step")).toBeTruthy();
    expect(screen.queryByText("Release command running")).toBeNull();
    expect(screen.getByText("The current release keeps answering meanwhile.")).toBeTruthy();
  });
  it("keeps failure details and retry available", () => {
    const failed = { ...project, status: { ...project.status, phase: "Failed", release: { target: "new", state: "Failed", message: "The release step cannot start: use a numeric USER.", job: "release-new" } } } as Project;
    show(<Heading project={failed} perms={perms} />);
    expect(screen.getByText("The release step cannot start: use a numeric USER.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Run it again" })).toBeTruthy();
  });
});


it("opens the logs again when a waiting release starts running", async () => {
  const waiting = { ...project, status: { ...project.status, phase: "Deploying", release: { target: "new", state: "Running", message: "Downloading the app's image.", job: "release-new" } } } as Project;
  const view = show(<Heading project={waiting} perms={perms} />);
  await waitFor(() => expect(fake.streamLogs).toHaveBeenCalledTimes(1));
  fake.streamLogs.mockImplementation(async (_slug, _options, _signal, push) => { push({ message: "Migration finished" }); });
  const started = { ...waiting, status: { ...waiting.status, release: { ...waiting.status.release!, message: "Running migrate before the new release starts." } } };
  view.rerender(<QueryClientProvider client={queries}><MemoryRouter><Heading project={started} perms={perms} /></MemoryRouter></QueryClientProvider>);
  await waitFor(() => expect(fake.streamLogs).toHaveBeenCalledTimes(2));
  expect(await screen.findByText("Migration finished")).toBeTruthy();
});
