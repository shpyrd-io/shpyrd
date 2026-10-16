import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import type { Project } from "@/api/types";
import type { Perms } from "@/lib/perms";
import seed from "../../../mock/projects.json";
import { Home } from "./home";

// The Mock answers: the project's metrics, logs, config and domains.
const projects = seed as unknown as Project[];
const hello = projects.find((p) => p.slug === "hello-world")!;
const docs = projects.find((p) => p.slug === "docs")!;
let queries: QueryClient;
afterEach(() => { cleanup(); queries?.clear(); });
function show(project: Project, perms: Partial<Perms> = { deploy: true }) {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={queries}><MemoryRouter><Home project={project} perms={perms as Perms} /></MemoryRouter></QueryClientProvider>);
}
const releasesCard = () => screen.getByText("Releases").closest("[data-slot=card]") as HTMLElement;

describe("a project's simple page", () => {
  it("lists the four newest releases at most, the newest live, the others to go back to", () => {
    const many = Array.from({ length: 6 }, (_, i) => ({ ...hello.status.releases[0], number: i + 1, description: `Release ${i + 1}` }));
    show({ ...hello, status: { ...hello.status, releases: many } });
    const card = within(releasesCard());
    expect(card.getByText("Release 6")).toBeTruthy();
    expect(card.getByText("Release 3")).toBeTruthy();
    expect(card.queryByText("Release 2")).toBeNull();
    expect(card.getByText("Live")).toBeTruthy();
    expect(card.getAllByRole("button", { name: "Go back" })).toHaveLength(3);
  });

  it("says why going back waits while a release goes out", () => {
    show(docs);
    expect(screen.getByText("A new release is going out. Going back waits until it is done.")).toBeTruthy();
    for (const b of within(releasesCard()).getAllByRole("button", { name: "Go back" })) expect(b.hasAttribute("disabled")).toBe(true);
  });

  it("offers no going back to who may not deploy", () => {
    show(docs, { deploy: false });
    expect(within(releasesCard()).queryByRole("button", { name: "Go back" })).toBeNull();
    expect(screen.queryByText(/Going back waits/)).toBeNull();
  });

  it("has the four things people change, and the way to Advanced", async () => {
    show(hello);
    for (const title of ["Secrets and settings", "Domain", "Database", "Who can open it"]) expect(screen.getByText(title)).toBeTruthy();
    expect(screen.getByRole("link", { name: /Advanced/ }).getAttribute("href")).toBe("/projects/hello-world/advanced");
    expect(await screen.findByText("Visits a minute")).toBeTruthy();
  });
});
