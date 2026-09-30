import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { CounterLabel } from "./counter-label";

describe("CounterLabel", () => {
  it("writes the count, grey by default", () => {
    render(<CounterLabel>12</CounterLabel>);
    expect(screen.getByText("12").getAttribute("data-variant")).toBe("default");
  });

  it("is primary for what asks to be seen", () => {
    render(<CounterLabel variant="primary">3</CounterLabel>);
    expect(screen.getByText("3").className).toContain("bg-primary");
  });
});
