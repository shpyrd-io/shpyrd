// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import NotFound from "./not-found";

afterEach(cleanup);

describe("the page for an address the site does not have", () => {
  it("says so, and offers the way home first, then the docs and Discord", () => {
    render(<NotFound />);
    expect(screen.getByText("Nothing is docked here")).toBeTruthy();
    expect(screen.getAllByRole("link").map((l) => l.getAttribute("href"))).toEqual(["/", "/docs/getting-started", "/discord"]);
  });
});
