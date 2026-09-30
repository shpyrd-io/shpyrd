import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { BrowserFrame } from "./browser-frame";

describe("BrowserFrame", () => {
  it("shows the address and, under it, what is at that address", () => {
    render(
      <BrowserFrame address="purchases.acme.shpyrd.app">
        <p>Sign in to open Purchase requests</p>
      </BrowserFrame>,
    );
    expect(screen.getByText("purchases.acme.shpyrd.app")).toBeTruthy();
    expect(screen.getByText("Sign in to open Purchase requests")).toBeTruthy();
  });

  it("marks a secure address with a padlock, and leaves it off when asked", () => {
    const { rerender } = render(<BrowserFrame address="acme.shpyrd.app">page</BrowserFrame>);
    expect(screen.getByLabelText("Secure")).toBeTruthy();
    rerender(
      <BrowserFrame address="localhost:3000" secure={false}>
        page
      </BrowserFrame>,
    );
    expect(screen.queryByLabelText("Secure")).toBeNull();
  });
});
