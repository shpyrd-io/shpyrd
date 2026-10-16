import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { Launcher } from "./launcher";

// The Mock answers: the workspace Acme and its projects.
vi.mock("@/shell/person", () => ({ ThemeButton: () => null, PersonMenu: () => null }));
let queries: QueryClient;
afterEach(() => { cleanup(); queries?.clear(); });
async function open() {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={queries}><MemoryRouter><Launcher /></MemoryRouter></QueryClientProvider>);
  fireEvent.click(await screen.findByRole("button", { name: "New project" }));
}

describe("a new project from the launcher", () => {
  it("leads with the sentence to give the agent", async () => {
    await open();
    expect(await screen.findByText(/Put this app online on /)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Copy" })).toBeTruthy();
  });

  it("makes the address from the name, until the address is written by hand", async () => {
    await open();
    const name = screen.getByLabelText(/^Name/) as HTMLInputElement;
    const address = screen.getByLabelText(/^Address/) as HTMLInputElement;
    fireEvent.change(name, { target: { value: "My Shop 2" } });
    expect(address.value).toBe("my-shop-2");
    fireEvent.change(address, { target: { value: "shop" } });
    fireEvent.change(name, { target: { value: "My Other Shop" } });
    expect(address.value).toBe("shop");
  });
});
