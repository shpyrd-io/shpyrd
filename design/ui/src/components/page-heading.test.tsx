import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { PageHeading } from "./page-heading";

describe("PageHeading", () => {
  it("is the first heading of the page, and only its title, when nothing else is given", () => {
    const { container } = render(<PageHeading title="Projects" />);
    expect(screen.getByRole("heading", { level: 1, name: "Projects" })).toBeTruthy();
    expect(container.querySelector("[data-slot=page-heading-description]")).toBeNull();
    expect(container.querySelector("[data-slot=page-heading-actions]")).toBeNull();
    expect(container.querySelector("[data-slot=page-heading-context]")).toBeNull();
  });

  it("takes another level inside a page that has its own", () => {
    render(<PageHeading as="h2" title="Releases" />);
    expect(screen.getByRole("heading", { level: 2, name: "Releases" })).toBeTruthy();
  });

  it("shows what explains it, what can be done and where the page is", () => {
    const { container } = render(
      <PageHeading
        title="Teams"
        icon={<svg data-testid="icon" />}
        iconEnd={<span>Running</span>}
        description="Groups of users."
        actions={<button>New team</button>}
        context={<a href="/">Projects</a>}
        border
      />,
    );
    expect(screen.getByRole("heading").contains(screen.getByTestId("icon"))).toBe(true);
    expect(screen.getByRole("heading").textContent).toBe("Teams");
    expect(screen.getByText("Running")).toBeTruthy();
    expect(screen.getByText("Groups of users.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "New team" })).toBeTruthy();
    expect(container.firstElementChild?.firstElementChild?.textContent).toBe("Projects");
    expect(container.firstElementChild?.className).toContain("border-b");
  });
});
