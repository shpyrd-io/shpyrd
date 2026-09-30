import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { SectionIntro } from "./section-intro";

describe("SectionIntro", () => {
  it("needs only its heading, and calls it a section of the page", () => {
    const { container } = render(<SectionIntro heading="Three things you get" />);
    expect(screen.getByRole("heading", { level: 2, name: "Three things you get" })).toBeTruthy();
    for (const part of ["label", "description", "link"]) {
      expect(container.querySelector(`[data-slot=section-intro-${part}]`), part).toBeNull();
    }
  });

  it("takes a lower heading, for a section inside a section", () => {
    render(<SectionIntro as="h3" heading="What people put in it" />);
    expect(screen.getByRole("heading", { level: 3 })).toBeTruthy();
  });

  it("shows every part it is given, in the order it reads", () => {
    const { container } = render(
      <SectionIntro
        label="For the person who maintains it"
        heading="Manage it over time"
        description="Every deploy is a numbered release."
        link={<a href="/docs">Read the deployment docs</a>}
      />,
    );
    const slots = [...container.querySelectorAll("[data-slot^=section-intro-]")].map((el) =>
      el.getAttribute("data-slot"),
    );
    expect(slots).toEqual([
      "section-intro-label",
      "section-intro-heading",
      "section-intro-description",
      "section-intro-link",
    ]);
  });

  it("centres what it is given when it is asked to", () => {
    const { container } = render(<SectionIntro align="center" heading="Sign-in controls who can open an app" />);
    const root = container.querySelector("[data-slot=section-intro]");
    expect(root?.getAttribute("data-align")).toBe("center");
    expect(root?.firstElementChild?.className).toContain("text-center");
  });
});
