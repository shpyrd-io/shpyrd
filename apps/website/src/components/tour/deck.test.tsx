// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { slides } from "@shpyrd/content/site/tour";
import { Deck } from "./deck";

afterEach(() => {
  cleanup();
  window.history.replaceState(null, "", "/");
});

// Three plain slides stand for the tour's.
const three = [0, 1, 2].map((n) => (go: (to: number) => void) => (
  <button type="button" onClick={() => go(2)}>
    slide {n}
  </button>
));
const current = () => screen.getByRole("region").getAttribute("aria-label");

describe("Deck", () => {
  it("opens on the first slide and says which it is", () => {
    render(<Deck slides={three} />);
    expect(screen.getByText("slide 0")).toBeTruthy();
    expect(current()).toBe(`1 of 3: ${slides[0]}`);
  });

  it("goes forward with the right arrow and back with the left, and stops at either end", () => {
    render(<Deck slides={three} />);
    fireEvent.keyDown(window, { key: "ArrowLeft" });
    expect(screen.getByText("slide 0")).toBeTruthy();
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(screen.getByText("slide 1")).toBeTruthy();
    fireEvent.keyDown(window, { key: "End" });
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(screen.getByText("slide 2")).toBeTruthy();
    fireEvent.keyDown(window, { key: "ArrowLeft" });
    expect(screen.getByText("slide 1")).toBeTruthy();
  });

  it("writes the slide in the address, and opens on the slide an address names", () => {
    render(<Deck slides={three} />);
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(window.location.hash).toBe("#2");
    cleanup();
    window.history.replaceState(null, "", "/#3");
    render(<Deck slides={three} />);
    expect(screen.getByText("slide 2")).toBeTruthy();
  });

  it("goes to a slide from its dot, and lets a slide move the deck", () => {
    render(<Deck slides={three} />);
    fireEvent.click(screen.getByRole("button", { name: `2: ${slides[1]}` }));
    expect(screen.getByText("slide 1")).toBeTruthy();
    fireEvent.click(screen.getByText("slide 1"));
    expect(screen.getByText("slide 2")).toBeTruthy();
  });

  it("leaves the keys a slide's own control took to that control", () => {
    render(<Deck slides={three} />);
    const event = new KeyboardEvent("keydown", { key: "ArrowRight", cancelable: true });
    event.preventDefault();
    window.dispatchEvent(event);
    expect(screen.getByText("slide 0")).toBeTruthy();
  });
});
