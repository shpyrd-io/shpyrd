import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Prose } from "./prose";

describe("Prose", () => {
  it("holds a text given as children", () => {
    const { container } = render(
      <Prose>
        <h2>Before you start</h2>
        <p>You need two things.</p>
      </Prose>,
    );
    expect(container.firstElementChild?.getAttribute("data-slot")).toBe("prose");
    expect(screen.getByRole("heading", { level: 2, name: "Before you start" })).toBeTruthy();
  });

  it("holds a text given as HTML, as it comes", () => {
    render(<Prose html={'<h2>From HTML</h2><p>With <a href="/x">a link</a>.</p>'} />);
    expect(screen.getByRole("heading", { level: 2, name: "From HTML" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "a link" }).getAttribute("href")).toBe("/x");
  });

  it("says when it is as wide as its place", () => {
    const { container } = render(
      <>
        <Prose>one</Prose>
        <Prose fullWidth>two</Prose>
      </>,
    );
    const [narrow, wide] = container.querySelectorAll("[data-slot=prose]");
    expect(narrow.getAttribute("data-full-width")).toBe("false");
    expect(wide.getAttribute("data-full-width")).toBe("true");
  });
});
