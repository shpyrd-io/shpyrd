// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { slides } from "@shpyrd/content/site/tour";
import { Tour } from "./tour";

afterEach(() => {
  cleanup();
  window.history.replaceState(null, "", "/");
});

describe("Tour", () => {
  it("has a slide for every name the copy gives, and reaches the last", () => {
    render(<Tour />);
    expect(screen.getAllByRole("button", { name: /^\d+: / })).toHaveLength(slides.length);
    for (let i = 1; i < slides.length; i++) fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(screen.getByRole("region").getAttribute("aria-label")).toBe(`${slides.length} of ${slides.length}: ${slides.at(-1)}`);
  });

  it("starts the tour from the first slide's button", () => {
    render(<Tour />);
    fireEvent.click(screen.getByRole("button", { name: /Start the tour/ }));
    expect(screen.getByRole("region").getAttribute("aria-label")).toBe(`2 of ${slides.length}: ${slides[1]}`);
  });
});
