import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { DialogHeader } from "./dialog";

describe("DialogHeader", () => {
  it("may have a line under it, and has none by default", () => {
    render(
      <>
        <DialogHeader divider>With</DialogHeader>
        <DialogHeader>Without</DialogHeader>
      </>,
    );
    expect(screen.getByText("With").getAttribute("data-divider")).toBe("true");
    expect(screen.getByText("With").className).toContain("border-b");
    expect(screen.getByText("Without").className).not.toContain("border-b");
  });
});
