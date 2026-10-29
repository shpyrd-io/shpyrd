// @vitest-environment jsdom
import { act, cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SleepFilm } from "./sleep-film";

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
const tags = (c: HTMLElement) => {
  const [ana, bot] = Array.from(c.querySelectorAll<HTMLElement>("[class*='rounded-full'][class*='absolute']"));
  return { ana: ana.className, bot: bot.className };
};
const below = "translate-y-[150%] opacity-0";
const shown = "translate-y-0 opacity-100";
const gone = "-translate-y-[150%] opacity-0";

describe("the sleeping film", () => {
  it("names the app yourapp.com", () => {
    const { container } = render(<SleepFilm />);
    expect(container.textContent).toContain("yourapp.com");
    expect(container.textContent).not.toContain("shop");
    expect(container.textContent).not.toContain("acme");
  });

  it("tells who comes as a vertical carousel: Ana rises in, the bot pushes her up, then rises away", () => {
    const { container } = render(<SleepFilm />);
    expect(tags(container).ana).toContain(below);
    expect(tags(container).bot).toContain(below);
    step(2000); // ana
    expect(tags(container).ana).toContain(shown);
    expect(tags(container).bot).toContain(below);
    step(1800 + 2400 + 3000 + 3600 + 1800); // the bot
    expect(tags(container).ana).toContain(gone);
    expect(tags(container).bot).toContain(shown);
    step(1800 + 2800); // rest
    expect(tags(container).bot).toContain(gone);
  });
});
