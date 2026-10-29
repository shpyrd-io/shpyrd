// @vitest-environment jsdom
import { act, cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ShipyardBar } from "./shipyard-bar";

vi.mock("@shpyrd/ui/components/shipyard", () => ({ Shipyard: () => <svg data-testid="yard" /> }));

let top = 0;
beforeEach(() => {
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(private call: (e: { isIntersecting: boolean }[]) => void) {}
      observe() {
        this.call([{ isIntersecting: true }]);
      }
      disconnect() {}
      unobserve() {}
    },
  );
  vi.useFakeTimers();
  // A frame is the next turn of the clock.
  vi.stubGlobal("requestAnimationFrame", (f: FrameRequestCallback) => setTimeout(() => f(0), 0));
  vi.stubGlobal("cancelAnimationFrame", (id: number) => clearTimeout(id));
  window.matchMedia = vi.fn().mockReturnValue({ matches: false }) as unknown as typeof window.matchMedia;
  Object.defineProperty(window, "innerHeight", { value: 1000, configurable: true });
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(() => ({ top, height: 400 }) as DOMRect);
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const yard = (c: HTMLElement) => c.querySelector<HTMLElement>("[class*='will-change']")!;
const scrollTo = (t: number) => {
  top = t;
  act(() => {
    window.dispatchEvent(new Event("scroll"));
    vi.advanceTimersByTime(1);
  });
};

describe("the yard in the logo's bar", () => {
  it("opens the bar once on the screen", () => {
    const { container } = render(<ShipyardBar />);
    expect(container.querySelector<HTMLElement>("[style*='clip-path']")!.style.clipPath).toContain("95.7%");
  });

  it("moves the yard slower than the page, up to 110px either way", () => {
    const { container } = render(<ShipyardBar />);
    scrollTo(800); // its middle at the window's foot
    expect(yard(container).style.translate).toBe("0 -110.0px");
    scrollTo(300); // its middle at the window's middle
    expect(yard(container).style.translate).toBe("0 0.0px");
    scrollTo(-200); // its middle at the window's top
    expect(yard(container).style.translate).toBe("0 110.0px");
  });
});
