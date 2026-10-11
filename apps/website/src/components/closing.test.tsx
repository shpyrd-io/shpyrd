// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { Closing } from "./proposals";

afterEach(cleanup);

describe("the home page's closing", () => {
  it("sends Get started to account signup", () => {
    render(<Closing />);
    expect(screen.getByRole("link", { name: "Get started" }).getAttribute("href")).toBe("https://accounts.shpyrd.io/signup?redirect_url=/signup/cloud");
  });
});
