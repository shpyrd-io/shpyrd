import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { Avatar } from "./avatar";
import { initials } from "../lib/avatar";

describe("Avatar", () => {
  it("draws the picture with the name as its alt", () => {
    render(<Avatar src="data:image/svg+xml,x" alt="Ana Souza" />);
    expect(screen.getByRole("img", { name: "Ana Souza" }).tagName).toBe("IMG");
  });

  it("draws the initials when there is no picture, and is read as an image", () => {
    render(<Avatar alt="Ana Souza" />);
    const avatar = screen.getByRole("img", { name: "Ana Souza" });
    expect(avatar.tagName).toBe("SPAN");
    expect(avatar.textContent).toBe("AS");
  });

  it("falls back to the initials when the picture fails", () => {
    render(<Avatar src="https://example.invalid/a.png" alt="Ana Souza" />);
    fireEvent.error(screen.getByRole("img", { name: "Ana Souza" }));
    expect(screen.getByRole("img", { name: "Ana Souza" }).textContent).toBe("AS");
  });

  it("is a circle for a person and a square for what is not", () => {
    render(
      <>
        <Avatar alt="Ana Souza" />
        <Avatar alt="Platform" square />
      </>,
    );
    expect(screen.getByRole("img", { name: "Ana Souza" }).getAttribute("data-shape")).toBe("circle");
    expect(screen.getByRole("img", { name: "Platform" }).getAttribute("data-shape")).toBe("square");
  });

  it("takes its size in pixels", () => {
    render(<Avatar alt="Ana Souza" size={48} />);
    expect(screen.getByRole("img", { name: "Ana Souza" }).style.width).toBe("48px");
  });
});

describe("initials", () => {
  it("takes the first letter of the first and of the last word", () => {
    expect(initials("Ana Souza")).toBe("AS");
    expect(initials("Ana Maria Souza")).toBe("AS");
    expect(initials("Platform")).toBe("P");
    expect(initials("  ")).toBe("");
  });
});
