import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { Carousel } from "./carousel";

// jsdom lays nothing out: the row's sizes are made up here.
let size = { client: 600, scroll: 600 };
beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  Object.defineProperty(HTMLElement.prototype, "clientWidth", { configurable: true, get: () => size.client });
  Object.defineProperty(HTMLElement.prototype, "scrollWidth", { configurable: true, get: () => size.scroll });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
const cards = <><div>A</div><div>B</div></>;

describe("a row of cards that goes sideways", () => {
  it("has no arrows when everything fits", () => {
    size = { client: 600, scroll: 600 };
    render(<Carousel heading="Projects">{cards}</Carousel>);
    expect(screen.getByRole("list", { name: "Projects" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Next" })).toBeNull();
  });

  it("turns each arrow off at its end", () => {
    size = { client: 600, scroll: 1200 };
    render(<Carousel heading="Projects">{cards}</Carousel>);
    expect(screen.getByRole("button", { name: "Previous" }).hasAttribute("disabled")).toBe(true);
    expect(screen.getByRole("button", { name: "Next" }).hasAttribute("disabled")).toBe(false);
    const row = screen.getByRole("list");
    row.scrollLeft = 600;
    fireEvent.scroll(row);
    expect(screen.getByRole("button", { name: "Previous" }).hasAttribute("disabled")).toBe(false);
    expect(screen.getByRole("button", { name: "Next" }).hasAttribute("disabled")).toBe(true);
  });
});
