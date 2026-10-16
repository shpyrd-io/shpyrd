import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { Home } from "./home";

// The Mock answers, as its owner: the workspace, its people, its projects;
// slowly when the machine is busy.
const slow = { timeout: 5000 };
let queries: QueryClient;
beforeEach(() => vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} }));
afterEach(() => { cleanup(); queries?.clear(); vi.unstubAllGlobals(); });
function Where() {
  return <div data-testid="where">{useLocation().pathname}</div>;
}
function show() {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queries}>
      <MemoryRouter initialEntries={["/workspace"]}>
        <Routes><Route path="*" element={<><Home /><Where /></>} /></Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("the workspace's simple page", () => {
  it("shows its projects as small cards that open the project here, not the application", async () => {
    show();
    const docs = await screen.findByRole("link", { name: "docs.acme.com" }, slow);
    expect(docs.closest("[data-slot=launcher-card]")?.getAttribute("data-size")).toBe("sm");
    fireEvent.click(docs);
    expect(screen.getByTestId("where").textContent).toBe("/projects/docs");
  });

  it("gives the agent its sign-in line, and the administrator people and the four settings", async () => {
    show();
    expect(await screen.findByText(/^shpyrd login --url https:\/\//, undefined, slow)).toBeTruthy();
    expect(await screen.findByText("People", undefined, slow)).toBeTruthy();
    for (const title of ["Teams", "Sign-in", "Shared config vars", "Domain"]) expect(await screen.findByText(title, undefined, slow)).toBeTruthy();
    expect(screen.getByRole("link", { name: /Advanced/ }).getAttribute("href")).toBe("/workspace/advanced");
  });

  it("opens Name and look from the pencil by the name", async () => {
    show();
    fireEvent.click(await screen.findByRole("button", { name: "Name and look" }, slow));
    expect(await screen.findByRole("dialog", { name: "Name and look" }, slow)).toBeTruthy();
    await waitFor(() => expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe("Acme"), slow);
  });
});
