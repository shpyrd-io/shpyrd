import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Hero } from "./hero";

describe("Hero", () => {
  it("needs only its heading, and calls it the heading of the page", () => {
    const { container } = render(<Hero heading="One place to share apps" />);
    expect(screen.getByRole("heading", { level: 1, name: "One place to share apps" })).toBeTruthy();
    for (const part of ["label", "description", "actions", "note", "image"]) {
      expect(container.querySelector(`[data-slot=hero-${part}]`), part).toBeNull();
    }
  });

  it("takes a lower heading, for a page that already has an h1", () => {
    render(<Hero as="h2" heading="Give each team the apps it needs" />);
    expect(screen.getByRole("heading", { level: 2 })).toBeTruthy();
  });

  it("shows every part it is given", () => {
    const { container } = render(
      <Hero
        label="Beta"
        heading="One place to share apps"
        description="Bring the apps your team builds."
        actions={<button>Add to Claude Code</button>}
        note="It sees only what your roles allow."
        image={<img alt="" src="/shipyard.webp" />}
      />,
    );
    expect(screen.getByText("Beta")).toBeTruthy();
    expect(screen.getByText("Bring the apps your team builds.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add to Claude Code" })).toBeTruthy();
    expect(screen.getByText("It sees only what your roles allow.")).toBeTruthy();
    expect(container.querySelector("[data-slot=hero-image]")).toBeTruthy();
  });

  it("puts the picture beside the words, in a grid of two", () => {
    const { container } = render(
      <Hero heading="One place" image={<img alt="" src="/shipyard.webp" />} />,
    );
    // The grid is inside the container, never on it: an element cannot answer
    // a query about its own width, so a hero that carries both never splits.
    const hero = container.querySelector("[data-slot=hero]");
    const grid = hero?.firstElementChild;
    expect(hero?.className).toContain("@container/hero");
    expect(grid?.className).toContain("@3xl/hero:grid-cols-2");
    expect(hero?.className).not.toContain("grid-cols-2");
  });

  it("drops the picture when it is centred, where there is no room beside", () => {
    const { container } = render(
      <Hero align="center" heading="Build with the agent you use" image={<img alt="" src="/x.webp" />} />,
    );
    expect(container.querySelector("[data-slot=hero-image]")).toBeNull();
    expect(container.querySelector("[data-slot=hero]")?.getAttribute("data-align")).toBe("center");
  });
});
