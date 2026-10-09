// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { deploy } from "@shpyrd/content/site/offer";
import { DeployButton } from "./deploy-button";

afterEach(cleanup);

describe("DeployButton", () => {
  it("asks to deploy the first thing, and goes to the free plan's sign-up", () => {
    render(<DeployButton />);
    const link = screen.getByRole("link", { name: `${deploy.label} ${deploy.things[0]} on shpyrd` });
    expect(link.getAttribute("href")).toBe("https://signup.shpyrd.io/?plan=free");
  });

  it("goes to the sign-up of the plan it is given", () => {
    render(<DeployButton plan="pro" />);
    expect(screen.getByRole("link").getAttribute("href")).toBe("https://signup.shpyrd.io/?plan=pro");
  });

  it("holds every word, showing one and hiding the others from assistive tech", () => {
    const { container } = render(<DeployButton />);
    const words = [...container.querySelectorAll("[data-place]")];
    expect(words.map((w) => w.textContent)).toEqual(deploy.things);
    expect(words.filter((w) => w.getAttribute("data-place") === "here").map((w) => w.textContent)).toEqual([deploy.things[0]]);
    expect(words.filter((w) => w.getAttribute("aria-hidden") === "true")).toHaveLength(deploy.things.length - 1);
  });

  it("carries no icon", () => {
    const { container } = render(<DeployButton />);
    expect(container.querySelector("svg")).toBeNull();
  });
});
