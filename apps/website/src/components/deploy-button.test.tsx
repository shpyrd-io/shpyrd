// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { deploy } from "@/content/offer";
import { DeployButton } from "./deploy-button";

const said = ({ verb, thing }: (typeof deploy)[number]) => `${verb} your ${thing}`;

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("DeployButton", () => {
  it("says the first phrase, and goes to account signup", () => {
    render(<DeployButton />);
    const link = screen.getByRole("link", { name: `${said(deploy[0])} on shpyrd` });
    expect(link.getAttribute("href")).toBe("https://accounts.shpyrd.io/signup?redirect_url=/signup/cloud");
  });

  it("rolls whole phrases, verb and all, showing one and hiding the others from assistive tech", () => {
    const { container } = render(<DeployButton />);
    const phrases = [...container.querySelectorAll("[data-place]")];
    expect(phrases.map((p) => p.textContent)).toEqual(deploy.map(said));
    expect(phrases.filter((p) => p.getAttribute("data-place") === "here").map((p) => p.textContent)).toEqual([said(deploy[0])]);
    expect(phrases.filter((p) => p.getAttribute("aria-hidden") === "true")).toHaveLength(deploy.length - 1);
  });

  it("says more than one verb", () => {
    expect(new Set(deploy.map((p) => p.verb))).toEqual(new Set(["Deploy", "Publish"]));
  });

  it("carries no icon", () => {
    const { container } = render(<DeployButton />);
    expect(container.querySelector("svg")).toBeNull();
  });

  it("moves on once the browser says the pointer is gone, even with no leave event", () => {
    vi.useFakeTimers();
    render(<DeployButton />);
    const link = screen.getByRole("link");
    const before = link.getAttribute("aria-label");
    // An enter with no leave: the window lost the pointer, or the page changed under it.
    fireEvent.mouseEnter(link);
    vi.spyOn(link, "matches").mockReturnValue(false);
    act(() => vi.advanceTimersByTime(3000));
    expect(link.getAttribute("aria-label")).not.toBe(before);
  });

  it("holds still while the pointer is over it", () => {
    vi.useFakeTimers();
    render(<DeployButton />);
    const link = screen.getByRole("link");
    const before = link.getAttribute("aria-label");
    fireEvent.mouseEnter(link);
    act(() => vi.advanceTimersByTime(6000));
    expect(link.getAttribute("aria-label")).toBe(before);
  });
});
