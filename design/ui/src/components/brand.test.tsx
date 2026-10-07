import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { LogoMark, Wordmark } from "./brand";

const symbolFill = (container: HTMLElement) => container.querySelector("g")!.getAttribute("fill");

describe("the logo", () => {
  it("is named shpyrd, the symbol orange and the name black, white in the dark", () => {
    const { container } = render(<Wordmark />);
    const svg = screen.getByRole("img", { name: "shpyrd" });
    expect(symbolFill(container)).toBe("#ff4f00");
    expect(svg.getAttribute("class")).toContain("text-black");
    expect(svg.getAttribute("class")).toContain("dark:text-white");
  });

  it("on dark: the symbol orange, the name white", () => {
    const { container } = render(<Wordmark surface="dark" />);
    expect(symbolFill(container)).toBe("#ff4f00");
    expect(container.querySelector("svg")!.getAttribute("class")).toContain("text-white");
  });

  it("on orange: the symbol white, the name black", () => {
    const { container } = render(<Wordmark surface="orange" />);
    expect(symbolFill(container)).toBe("#ffffff");
    const cls = container.querySelector("svg")!.getAttribute("class")!;
    expect(cls).toContain("text-black");
    expect(cls).not.toContain("text-white");
  });

  it("the symbol alone is white on orange and orange elsewhere", () => {
    expect(symbolFill(render(<LogoMark surface="orange" />).container)).toBe("#ffffff");
    expect(symbolFill(render(<LogoMark />).container)).toBe("#ff4f00");
  });
});
