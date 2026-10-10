// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import { VerticalRoute } from "./vertical-route";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

// An IntersectionObserver that remembers what it was asked and can be told
// the list came into view.
function watch() {
  const seen: { options?: IntersectionObserverInit; callback?: IntersectionObserverCallback } = {};
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(callback: IntersectionObserverCallback, options?: IntersectionObserverInit) {
        seen.callback = callback;
        seen.options = options;
      }
      observe() {}
      disconnect() {}
    },
  );
  return seen;
}

const steps = [1, 2, 3].map((n) => ({ icon: <span />, children: <p>step {n}</p> }));

describe("VerticalRoute", () => {
  it("shows its steps as soon as the list's top is in view, however tall the list is", () => {
    const seen = watch();
    const { container } = render(<VerticalRoute steps={steps} />);
    // Any part of the list in view counts: a share of a tall list would wait,
    // on a phone, until the reader had scrolled past the first step.
    expect(seen.options?.threshold).toBe(0);
    const list = container.querySelector("ol")!;
    expect(list.dataset.shown).toBeUndefined();
    act(() => seen.callback!([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver));
    expect(list.dataset.shown).toBe("true");
  });
});
