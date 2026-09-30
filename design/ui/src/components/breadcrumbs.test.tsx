import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Breadcrumbs, BreadcrumbsItem } from "./breadcrumbs";

describe("Breadcrumbs", () => {
  it("is a navigation with a name, and a list in the order of the levels", () => {
    render(
      <Breadcrumbs>
        <BreadcrumbsItem href="/">Home</BreadcrumbsItem>
        <BreadcrumbsItem href="/about">About</BreadcrumbsItem>
      </Breadcrumbs>,
    );
    expect(screen.getByRole("navigation", { name: "Breadcrumbs" })).toBeTruthy();
    expect(screen.getAllByRole("listitem").map((i) => i.textContent)).toEqual(["Home", "About"]);
  });

  it("makes a link of a level with an address, and only a name of one without", () => {
    render(
      <Breadcrumbs>
        <BreadcrumbsItem href="/">Overview</BreadcrumbsItem>
        <BreadcrumbsItem>Foundations</BreadcrumbsItem>
      </Breadcrumbs>,
    );
    expect(screen.getByRole("link", { name: "Overview" }).getAttribute("href")).toBe("/");
    expect(screen.getByText("Foundations").tagName).toBe("SPAN");
  });

  it("marks the page itself as the current one", () => {
    render(
      <Breadcrumbs>
        <BreadcrumbsItem href="/">Home</BreadcrumbsItem>
        <BreadcrumbsItem href="/team" selected>
          Team
        </BreadcrumbsItem>
      </Breadcrumbs>,
    );
    expect(screen.getByRole("link", { name: "Team" }).getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("link", { name: "Home" }).hasAttribute("aria-current")).toBe(false);
  });

  it("becomes the link of a router", () => {
    render(
      <Breadcrumbs>
        <BreadcrumbsItem asChild selected>
          <a href="/x" data-router="yes">
            Here
          </a>
        </BreadcrumbsItem>
      </Breadcrumbs>,
    );
    const link = screen.getByRole("link", { name: "Here" });
    expect(link.getAttribute("data-router")).toBe("yes");
    expect(link.getAttribute("aria-current")).toBe("page");
  });
});
