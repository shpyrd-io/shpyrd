import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { SegmentedNav } from "./segmented-nav";

const links = (open: number) =>
  ["Overview", "Your first deploy", "Run it yourself"].map((name, i) => (
    <a key={name} href={`#${i}`} aria-current={i === open ? "page" : undefined}>
      {name}
    </a>
  ));

describe("SegmentedNav", () => {
  it("is a navigation of links, named for who cannot see it", () => {
    render(<SegmentedNav aria-label="For developers" links={links(0)} />);
    const nav = screen.getByRole("navigation", { name: "For developers" });
    expect(nav.querySelectorAll("a")).toHaveLength(3);
  });

  it("marks the page that is open, and only that one", () => {
    render(<SegmentedNav aria-label="For developers" links={links(1)} />);
    const open = screen.getAllByRole("link").filter((a) => a.getAttribute("aria-current") === "page");
    expect(open.map((a) => a.textContent)).toEqual(["Your first deploy"]);
  });

  it("lights the open page with a pill, or a line under it", () => {
    const { container, rerender } = render(<SegmentedNav aria-label="x" links={links(0)} />);
    expect(container.querySelector("[data-slot=segmented-nav]")?.getAttribute("data-variant")).toBe("pill");
    rerender(<SegmentedNav aria-label="x" variant="underline" links={links(0)} />);
    expect(container.querySelector("[data-slot=segmented-nav]")?.getAttribute("data-variant")).toBe(
      "underline",
    );
  });
});
