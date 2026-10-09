import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { Checkbox } from "./checkbox";
import { Label } from "./label";

describe("Checkbox", () => {
  it("is not checked until it is clicked, and says so when it is", () => {
    const onCheckedChange = vi.fn();
    render(<Checkbox aria-label="Single sign-on" onCheckedChange={onCheckedChange} />);
    const box = screen.getByRole("checkbox", { name: "Single sign-on" });
    expect(box.getAttribute("aria-checked")).toBe("false");
    fireEvent.click(box);
    expect(box.getAttribute("aria-checked")).toBe("true");
    expect(onCheckedChange).toHaveBeenCalledWith(true);
  });

  it("is named by the label that points at it", () => {
    render(
      <>
        <Checkbox id="sla" />
        <Label htmlFor="sla">Support with an SLA</Label>
      </>,
    );
    expect(screen.getByRole("checkbox", { name: "Support with an SLA" })).toBeTruthy();
  });

  it("cannot be changed while disabled", () => {
    render(<Checkbox aria-label="Managed" disabled />);
    const box = screen.getByRole("checkbox", { name: "Managed" });
    fireEvent.click(box);
    expect(box.getAttribute("aria-checked")).toBe("false");
  });
});
