import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Textarea } from "./textarea";

describe("Textarea", () => {
  it("is a textarea, and takes what it is told", () => {
    render(<Textarea placeholder="A few lines" aria-invalid disabled />);
    const area = screen.getByPlaceholderText<HTMLTextAreaElement>("A few lines");
    expect(area.tagName).toBe("TEXTAREA");
    expect(area.getAttribute("aria-invalid")).toBe("true");
    expect(area.disabled).toBe(true);
  });

  it("grows with what is written", () => {
    render(<Textarea aria-label="Description" />);
    expect(screen.getByRole("textbox").className).toContain("field-sizing-content");
  });
});
