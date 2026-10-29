// @vitest-environment jsdom
import { act, cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PublishFilm } from "./publish-film";

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

const pill = (c: HTMLElement) => c.querySelector<HTMLElement>(".z-10")!;
// Each moment sets the next one's timer once it is drawn, so the clock moves
// a little at a time, letting each be drawn.
const step = (ms: number) => {
  // 50ms at a time: every moment lasts a multiple of it, so none is late.
  for (let t = 0; t < ms; t += 50) act(() => void vi.advanceTimersByTime(Math.min(50, ms - t)));
};

// The film's moments, in order, as Giovani told it.
const T = { boxes: 900, ups: 500 + 500 + 700, lit: 350 + 350 + 600, send: 1100, pulses: 4 * 900, received: 900, online: 2400, leave: 900 };
const pulses = (c: HTMLElement) => Array.from(c.querySelectorAll<SVGPathElement>("path[pathLength='100']"));
const items = (c: HTMLElement) => Array.from(c.querySelectorAll("li"));
const lines = (c: HTMLElement) => Array.from(c.querySelectorAll<SVGPathElement>("path[pathLength='1']"));
const cloud = (c: HTMLElement) => c.querySelector<HTMLElement>("[class*='w-[min(50cqw']")!;

describe("the publishing film", () => {
  it("opens on the two boxes alone", () => {
    const { container } = render(<PublishFilm />);
    expect(items(container)).toHaveLength(0);
    expect(cloud(container).className).toContain("opacity-0");
    expect(pill(container).className).toContain("opacity-0");
    for (const l of lines(container)) expect(l.style.strokeDashoffset).toBe("1");
  });

  it("lists the three uploads in grey, then turns them orange", () => {
    const { container } = render(<PublishFilm />);
    step(T.boxes + T.ups - 50); // the last upload just in
    expect(items(container).map((li) => li.textContent)).toEqual(["first upload", "Fix checkout button", "Add dark mode"]);
    for (const li of items(container)) expect(li.className).toContain("text-muted-foreground");
    step(T.lit); // the last one just lit
    for (const li of items(container)) expect(li.className).toContain("text-primary");
  });

  it("then raises the lines, brings the cloud, sends pulses up the lines in turn, and only then the check and the address", () => {
    const { container } = render(<PublishFilm />);
    step(T.boxes + T.ups + T.lit);
    for (const l of lines(container)) expect(l.style.strokeDashoffset).toBe("0");
    expect(cloud(container).className).toContain("opacity-100");
    expect(pulses(container)).toHaveLength(0);
    step(T.send);
    // The folder's line first, then GitHub's: each pulse on one line only.
    const first = pulses(container).map((p) => p.getAttribute("d"));
    expect(first.length).toBeGreaterThan(0);
    expect(new Set(first).size).toBe(1);
    step(900);
    const second = pulses(container).map((p) => p.getAttribute("d"));
    expect(new Set(second).size).toBe(1);
    expect(second[0]).not.toBe(first[0]);
    expect(container.querySelector("[aria-label='Received']")).toBeNull();
    expect(pill(container).className).toContain("opacity-0");
    step(T.pulses - 900);
    expect(pulses(container)).toHaveLength(0);
    expect(container.querySelector("[aria-label='Received']")).not.toBeNull();
    expect(pill(container).className).toContain("opacity-0");
    step(T.received);
    expect(pill(container).className).toContain("opacity-100");
    expect(pill(container).textContent).toBe("yourapp.com");
  });

  it("takes the address, the cloud and the lines away, and starts again", () => {
    const { container } = render(<PublishFilm />);
    step(T.boxes + T.ups + T.lit + T.send + T.pulses + T.received + T.online);
    expect(pill(container).className).toContain("opacity-0");
    expect(cloud(container).className).toContain("opacity-0");
    for (const l of lines(container)) expect(l.style.opacity).toBe("0");
    step(T.leave);
    expect(items(container)).toHaveLength(0);
    expect(lines(container).length).toBe(2);
  });
});
