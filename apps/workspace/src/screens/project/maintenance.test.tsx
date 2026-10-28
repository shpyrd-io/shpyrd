import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Project } from "@/api/types";
import type { Perms } from "@/lib/perms";
import seed from "../../../mock/projects.json";
import { Maintenance } from "./maintenance";
const fake = vi.hoisted(() => ({ setMaintenance: vi.fn() }));
vi.mock("@/api/api", () => ({ api: fake }));
let queries: QueryClient;
afterEach(() => { cleanup(); queries?.clear(); vi.resetAllMocks(); });

const show = (project: Project, perms: Partial<Perms> = { deploy: true }) => {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={queries}><Maintenance project={project} perms={perms as Perms} /></QueryClientProvider>);
};

it("puts the page up with the message as typed, in one call", async () => {
  const project = seed[0] as unknown as Project;
  fake.setMaintenance.mockResolvedValue({ ...project, maintenance: { enabled: true, message: "Back at 15:00" } });
  show(project);
  expect(screen.getByRole("switch").getAttribute("aria-checked")).toBe("false");
  fireEvent.change(screen.getByLabelText("Message"), { target: { value: "  Back at 15:00 " } });
  fireEvent.click(screen.getByRole("switch"));
  await waitFor(() => expect(fake.setMaintenance).toHaveBeenCalledWith(project.slug, { enabled: true, message: "Back at 15:00" }));
});

it("says since when and by whom the page is up, and changes its words without taking it down", async () => {
  const since = new Date(Date.now() - 2 * 3_600_000).toISOString();
  const project = { ...(seed[0] as unknown as Project), maintenance: { enabled: true, message: "Back at 15:00", since, by: "ana@acme.test" } };
  fake.setMaintenance.mockResolvedValue(project);
  show(project);
  expect(screen.getByRole("switch").getAttribute("aria-checked")).toBe("true");
  expect(screen.getByText(/put up by ana@acme.test/)).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Change what the page says" })).toBeNull();
  fireEvent.change(screen.getByLabelText("Message"), { target: { value: "Back at 16:00" } });
  fireEvent.click(screen.getByRole("button", { name: "Change what the page says" }));
  await waitFor(() => expect(fake.setMaintenance).toHaveBeenCalledWith(project.slug, { enabled: true, message: "Back at 16:00" }));
});

it("is read-only for who may not deploy", () => {
  show(seed[0] as unknown as Project, { deploy: false });
  expect((screen.getByRole("switch") as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByLabelText("Message") as HTMLTextAreaElement).disabled).toBe(true);
});
