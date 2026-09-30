import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Pillar } from "./pillar";

describe("Pillar", () => {
  it("needs only its heading, and sits under the heading of a section", () => {
    const { container } = render(<Pillar heading="Make it available" />);
    expect(screen.getByRole("heading", { level: 3, name: "Make it available" })).toBeTruthy();
    for (const part of ["icon", "description", "link"]) {
      expect(container.querySelector(`[data-slot=pillar-${part}]`), part).toBeNull();
    }
  });

  it("takes a lower heading, for a pillar nested deeper", () => {
    render(<Pillar as="h4" heading="Choose who can use it" />);
    expect(screen.getByRole("heading", { level: 4 })).toBeTruthy();
  });

  it("shows every part it is given, the cue over the heading", () => {
    const { container } = render(
      <Pillar
        icon={<svg data-testid="cue" />}
        heading="Manage it over time"
        description="Every deploy is a numbered release."
        link={<a href="/docs">Read more</a>}
      />,
    );
    expect(screen.getByTestId("cue")).toBeTruthy();
    expect(screen.getByText("Every deploy is a numbered release.")).toBeTruthy();
    const slots = [...container.querySelectorAll("[data-slot^=pillar-]")].map((el) =>
      el.getAttribute("data-slot"),
    );
    expect(slots).toEqual(["pillar-icon", "pillar-heading", "pillar-description", "pillar-link"]);
  });

  it("starts its content at the top, so siblings of unequal length line up", () => {
    const { container } = render(<Pillar heading="Make it available" />);
    expect(container.querySelector("[data-slot=pillar]")?.className).toContain("content-start");
  });
});
