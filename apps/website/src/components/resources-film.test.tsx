// @vitest-environment jsdom
import { act, cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ResourcesFilm } from "./resources-film";

// The film plays once it is on the screen: jsdom has no screen, so the
// observer says it is, and the clock is ours to move.
beforeEach(() => {
  vi.useFakeTimers();
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
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(private call: (e: { contentRect: { width: number; height: number } }[]) => void) {}
      observe() {
        this.call([{ contentRect: { width: 300, height: 200 } }]);
      }
      disconnect() {}
    },
  );
  window.matchMedia = vi.fn().mockReturnValue({ matches: false }) as unknown as typeof window.matchMedia;
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const step = (ms: number) => {
  for (let t = 0; t < ms; t += 50) act(() => void vi.advanceTimersByTime(Math.min(50, ms - t)));
};
const text = (c: HTMLElement) => c.textContent ?? "";
const lines = (c: HTMLElement) => Array.from(c.querySelectorAll<SVGPathElement>("path[pathLength='1']"));
const pulses = (c: HTMLElement) => Array.from(c.querySelectorAll("path[pathLength='100']"));
const light = (c: HTMLElement) => c.querySelector<HTMLElement>(".relative.size-2.rounded-full")!;

// The moments, in order, as Giovani told it.
const T = { app: 700, part: 700, ready: 450, line: 950 };

describe("the database film", () => {
  it("opens on yourapp.com alone, its light grey", () => {
    const { container } = render(<ResourcesFilm />);
    expect(text(container)).toContain("yourapp.com");
    expect(light(container).className).toContain("bg-foreground/25");
    for (const l of lines(container)) expect(l.style.strokeDashoffset).toBe("1");
    expect(text(container)).not.toContain("DATABASE_URL");
  });

  it("brings each piece, makes it ready, then draws its line with a pulse, one after the other", () => {
    const { container } = render(<ResourcesFilm />);
    step(T.app + T.part - 50);
    expect(text(container)).toContain("Creating");
    step(50 + T.ready);
    // The database ready and connecting: its line drawn, a pulse on it; the others not yet.
    expect(lines(container).map((l) => l.style.strokeDashoffset)).toEqual(["0", "1", "1"]);
    expect(pulses(container).length).toBeGreaterThan(0);
    step(T.line + T.part + T.ready);
    expect(lines(container).map((l) => l.style.strokeDashoffset)).toEqual(["0", "0", "1"]);
    step(T.line + T.part + T.ready);
    expect(lines(container).map((l) => l.style.strokeDashoffset)).toEqual(["0", "0", "0"]);
  });

  it("keeps sending pulses on every connected line, then turns the app's light green", () => {
    const { container } = render(<ResourcesFilm />);
    step(T.app + T.part + T.ready + T.line + T.part + T.ready);
    // The database's line keeps pulsing while the cache connects: two lines pulsing.
    const groups = container.querySelectorAll("svg > g");
    expect(groups.length).toBe(2);
    step(T.line + T.part + T.ready + T.line - 50);
    expect(light(container).className).toContain("bg-foreground/25");
    step(50);
    expect(container.querySelectorAll("svg > g").length).toBe(3);
    expect(light(container).className).toContain("bg-success");
    expect(text(container)).toContain("3 connected");
  });
});
