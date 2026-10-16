import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { Frame } from "./frame";

vi.mock("./person", () => ({ ThemeButton: () => null, PersonMenu: () => null }));
afterEach(cleanup);
function show(nav?: React.ReactNode) {
  render(<QueryClientProvider client={new QueryClient()}><MemoryRouter><Frame nav={nav} crumbs={[{ label: "Projects", to: "/" }]}>Page</Frame></MemoryRouter></QueryClientProvider>);
}

describe("the frame of a page", () => {
  it("has a side and a menu when it is given a navigation", () => {
    show(<a href="/x">X</a>);
    expect(screen.getByRole("complementary", { name: "Navigation" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Navigation" })).toBeTruthy();
  });

  it("has neither without one, and the launcher is the logo in the header", () => {
    show();
    expect(screen.queryByRole("complementary", { name: "Navigation" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Navigation" })).toBeNull();
    expect(screen.getByRole("link", { name: "Launcher" }).getAttribute("href")).toBe("/");
  });
});
