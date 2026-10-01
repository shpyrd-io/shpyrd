import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { SiteHeader } from "./site-header";

const docs = <a href="/docs">Docs</a>;

describe("SiteHeader", () => {
  it("holds the brand, the pages and what to do", () => {
    render(
      <SiteHeader
        start={<a href="/">shpyrd</a>}
        links={[docs]}
        actions={<button>Sign in</button>}
      />,
    );
    expect(screen.getByRole("link", { name: "shpyrd" })).toBeTruthy();
    expect(screen.getAllByRole("link", { name: "Docs" }).length).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: "Sign in" })).toBeTruthy();
  });

  it("stays at the top by itself, and leaves that to a layout's header when it is a div", () => {
    const { container, rerender } = render(<SiteHeader links={[docs]} />);
    const own = container.querySelector("header[data-slot=site-header]");
    expect(own?.className).toContain("sticky");
    rerender(<SiteHeader as="div" sticky={false} links={[docs]} />);
    expect(container.querySelector("header")).toBeNull();
    expect(container.querySelector("[data-slot=site-header]")?.className).not.toContain("sticky");
  });

  it("holds its contents to the width it is given", () => {
    const { container } = render(<SiteHeader width="large" links={[docs]} />);
    expect(container.querySelector("[data-slot=site-header] > div")?.className).toContain("max-w-[67.5rem]");
  });

  it("opens a group of pages as a panel, in columns with their headings and lines", () => {
    render(
      <SiteHeader
        links={[
          {
            label: "Solutions",
            columns: [
              {
                label: "For",
                links: [{ link: <a href="/for/it">For IT teams</a>, description: "One accepted place." }],
              },
              { label: "What you're shipping", links: [<a key="t" href="/t">Internal tools</a>] },
            ],
          },
        ]}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Solutions" }));
    expect(screen.getByText("What you're shipping")).toBeTruthy();
    expect(screen.getByText("One accepted place.")).toBeTruthy();
    expect(screen.getAllByRole("link", { name: /Internal tools/ }).length).toBeGreaterThan(0);
  });

  it("folds the pages into a menu, where a group's columns are groups of their own", () => {
    render(
      <SiteHeader
        links={[
          { label: "Solutions", columns: [{ label: "For", links: [<a key="it" href="/for/it">For IT teams</a>] }] },
        ]}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Menu" }));
    expect(screen.getAllByText("For").length).toBeGreaterThan(0);
    expect(screen.getAllByRole("link", { name: "For IT teams" }).length).toBeGreaterThan(0);
  });

  it("draws the pages as nav buttons at the large size, standing apart with no ground", () => {
    const { container } = render(
      <SiteHeader links={[docs, { label: "Solutions", links: [<a key="it" href="/for/it">For IT teams</a>] }]} />,
    );
    const list = container.querySelector("[data-slot=navigation-menu-list]");
    expect(list?.className).toContain("gap-5");
    const link = list?.querySelector("a[href='/docs']");
    expect(link?.className).toContain("h-10");
    expect(link?.className).toContain("hover:text-primary");
    expect(link?.className).toContain("hover:bg-transparent");
    const trigger = screen.getByRole("button", { name: "Solutions" });
    expect(trigger.className).toContain("h-10");
    expect(trigger.className).toContain("data-[state=open]:text-primary");
  });
});
