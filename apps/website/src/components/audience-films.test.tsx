// @vitest-environment jsdom
import { act, cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DevelopersFilm, PartnersFilm, TeamsFilm } from "./audience-films";

// The films play once on the screen: jsdom has no screen, so the observer
// says they are, and the clock is ours to move.
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
        this.call([{ contentRect: { width: 300, height: 120 } }]);
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

describe("the developers' film", () => {
  it("deploys v1, pushes v2, shows its errors in the logs, rolls back to v1", () => {
    const { container } = render(<DevelopersFilm />);
    expect(text(container)).toContain("first deployment");
    expect(text(container)).not.toContain("next change");
    step(2000);
    expect(text(container)).toContain("next change");
    step(2000);
    expect(text(container)).toContain("GET /checkout 500");
    step(2200 + 2000);
    expect(text(container)).toContain("rolled back");
    expect(text(container)).toContain("GET /checkout 200");
    expect(text(container)).not.toContain("GET /checkout 500");
  });
});

describe("the internal apps' film", () => {
  it("signs Ana in, shows her portal with Payroll locked, then a new release of Approvals", () => {
    const { container } = render(<TeamsFilm />);
    expect(text(container)).toContain("Continue with company login");
    expect(text(container)).not.toContain("Your apps");
    step(2400);
    expect(text(container)).toContain("Your apps");
    expect(text(container)).toContain("Payroll");
    expect(text(container)).not.toContain("v2");
    step(2000 + 2200);
    expect(text(container)).toContain("Approvalsv2");
  });
});

describe("the partners' film", () => {
  it("delivers the client's project, then shows who does what in each way on", () => {
    const { container } = render(<PartnersFilm />);
    expect(text(container)).toContain("Deploying");
    step(1600);
    expect(text(container)).toContain("Delivered");
    const row = (label: string) => Array.from(container.querySelectorAll("span")).find((s) => s.firstElementChild?.textContent === label)!.textContent;
    step(1200);
    expect(row("Runs the app")).toContain("Client");
    step(3000);
    expect(row("Runs the app")).toContain("You");
    expect(row("Pays the servers")).toContain("Client");
    step(3000);
    expect(row("Pays the servers")).toContain("Your brand");
  });
});
