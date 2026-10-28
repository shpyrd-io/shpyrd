import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { Field } from "./field";
import { Slider } from "./slider";

describe("Slider", () => {
  it("is a slider that says its value and its bounds", () => {
    render(<Slider aria-label="CPU target" defaultValue={[70]} min={1} max={100} />);
    const thumb = screen.getByRole("slider", { name: "CPU target" });
    expect(thumb.getAttribute("aria-valuenow")).toBe("70");
    expect(thumb.getAttribute("aria-valuemin")).toBe("1");
    expect(thumb.getAttribute("aria-valuemax")).toBe("100");
  });

  it("moves a step with the arrow keys, to the end with End, and tells", () => {
    const onValueChange = vi.fn();
    render(<Slider aria-label="CPU target" defaultValue={[70]} min={1} max={100} onValueChange={onValueChange} />);
    const thumb = screen.getByRole("slider");
    fireEvent.focus(thumb);
    fireEvent.keyDown(thumb, { key: "ArrowRight" });
    expect(onValueChange).toHaveBeenLastCalledWith([71]);
    expect(thumb.getAttribute("aria-valuenow")).toBe("71");
    fireEvent.keyDown(thumb, { key: "End" });
    expect(onValueChange).toHaveBeenLastCalledWith([100]);
  });

  it("has a thumb for each bound of a range, each with its name", () => {
    const onValueChange = vi.fn();
    render(
      <Slider thumbLabels={["At least", "At most"]} defaultValue={[2, 10]} min={1} max={20} onValueChange={onValueChange} />,
    );
    expect(screen.getByRole("slider", { name: "At least" }).getAttribute("aria-valuenow")).toBe("2");
    const most = screen.getByRole("slider", { name: "At most" });
    expect(most.getAttribute("aria-valuenow")).toBe("10");
    fireEvent.focus(most);
    fireEvent.keyDown(most, { key: "ArrowLeft" });
    expect(onValueChange).toHaveBeenLastCalledWith([2, 9]);
  });

  it("follows what it is told when controlled", () => {
    const { rerender } = render(<Slider aria-label="Requests" value={[20]} />);
    expect(screen.getByRole("slider").getAttribute("aria-valuenow")).toBe("20");
    rerender(<Slider aria-label="Requests" value={[35]} />);
    expect(screen.getByRole("slider").getAttribute("aria-valuenow")).toBe("35");
  });

  it("is told of the hint of the field it is in", () => {
    render(
      <Field label="CPU target" hint="Percent of the size's CPU.">
        <Slider aria-label="CPU target" defaultValue={[70]} />
      </Field>,
    );
    const thumb = screen.getByRole("slider", { name: "CPU target" });
    const hint = document.getElementById(thumb.getAttribute("aria-describedby")!);
    expect(hint?.textContent).toBe("Percent of the size's CPU.");
  });

  it("cannot be moved when disabled", () => {
    const onValueChange = vi.fn();
    render(<Slider aria-label="CPU target" defaultValue={[70]} disabled onValueChange={onValueChange} />);
    const thumb = screen.getByRole("slider");
    expect(thumb.getAttribute("data-disabled")).not.toBeNull();
    fireEvent.keyDown(thumb, { key: "ArrowRight" });
    expect(onValueChange).not.toHaveBeenCalled();
  });
});
