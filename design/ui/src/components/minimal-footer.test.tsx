import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { MinimalFooter } from "./minimal-footer";

describe("MinimalFooter", () => {
  it("shows its links, where else to find you, the mark and its line", () => {
    render(
      <MinimalFooter
        links={[<a key="docs" href="/docs">Docs</a>]}
        social={[{ label: "shpyrd on GitHub", href: "https://github.com", icon: <svg /> }]}
        logo={<a href="/">shpyrd</a>}
        note="Open source under MPL-2.0."
      />,
    );
    expect(screen.getByRole("navigation", { name: "Footer" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Docs" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "shpyrd on GitHub" })).toBeTruthy();
    expect(screen.getByText("Open source under MPL-2.0.")).toBeTruthy();
  });

  it("is a footer by itself, and a div inside a layout's foot", () => {
    const { container, rerender } = render(<MinimalFooter note="x" />);
    expect(container.querySelector("footer[data-slot=minimal-footer]")).toBeTruthy();
    rerender(<MinimalFooter as="div" note="x" />);
    expect(container.querySelector("footer")).toBeNull();
    expect(container.querySelector("div[data-slot=minimal-footer]")).toBeTruthy();
  });

  it("goes back to the top when asked", () => {
    const scrollTo = vi.fn();
    window.scrollTo = scrollTo as unknown as typeof window.scrollTo;
    render(<MinimalFooter note="x" backToTop />);
    fireEvent.click(screen.getByRole("button", { name: "Back to top" }));
    expect(scrollTo).toHaveBeenCalledWith(expect.objectContaining({ top: 0 }));
  });
});
