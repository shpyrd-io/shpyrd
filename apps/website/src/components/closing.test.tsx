// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { Closing } from "./proposals";

afterEach(cleanup);

describe("the home page's closing", () => {
  it("sends Get started to the sign-up, on the free plan", () => {
    render(<Closing />);
    expect(screen.getByRole("link", { name: "Get started" }).getAttribute("href")).toBe("https://signup.shpyrd.io/?plan=free");
  });
});
