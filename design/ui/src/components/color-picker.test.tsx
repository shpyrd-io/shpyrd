import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { ColorPicker, isHex } from "./color-picker";

describe("ColorPicker", () => {
  it("shows the colour on the swatch and writes its hex", () => {
    render(<ColorPicker defaultValue="#ff4f00" />);
    const swatch = screen.getByRole("button", { name: "Choose a colour" });
    expect(swatch.style.backgroundColor).toBe("rgb(255, 79, 0)");
    expect(screen.getByRole<HTMLInputElement>("textbox").value).toBe("#ff4f00");
  });

  it("tells what is typed, and marks what is not a colour", () => {
    const onChange = vi.fn();
    render(<ColorPicker onChange={onChange} />);
    const field = screen.getByRole("textbox");
    fireEvent.change(field, { target: { value: "#ff4" } });
    expect(onChange).toHaveBeenCalledWith("#ff4");
    expect(field.getAttribute("aria-invalid")).toBe("true");
    fireEvent.change(field, { target: { value: "#0284c7" } });
    expect(field.hasAttribute("aria-invalid")).toBe(false);
  });

  it("offers the presets, and takes the one pressed", () => {
    const onChange = vi.fn();
    render(<ColorPicker value="#ff4f00" onChange={onChange} presets={["#ff4f00", "#0284c7"]} />);
    fireEvent.click(screen.getByRole("button", { name: "Choose a colour" }));
    expect(screen.getByRole("button", { name: "#ff4f00" }).getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: "#0284c7" }));
    expect(onChange).toHaveBeenCalledWith("#0284c7");
  });

  it("knows a hex when it sees one", () => {
    expect(isHex("#ff4f00")).toBe(true);
    expect(isHex("#FF4F00")).toBe(true);
    expect(isHex("#ff4")).toBe(false);
    expect(isHex("ff4f00")).toBe(false);
  });
});
