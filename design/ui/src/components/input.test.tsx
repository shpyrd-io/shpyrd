import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { Input } from "./input";

const Icon = () => <svg data-testid="icon" />;

describe("Input", () => {
  it("is only the field when nothing goes beside its text", () => {
    const { container } = render(<Input placeholder="hello-world" />);
    expect(container.firstElementChild?.tagName).toBe("INPUT");
  });

  it("draws one box around the field and what goes beside it", () => {
    const { container } = render(
      <Input icon={<Icon />} prefix="https://" suffix=".shpyrd.app" placeholder="hello" />,
    );
    const box = container.firstElementChild as HTMLElement;
    expect(box.getAttribute("data-slot")).toBe("input-box");
    expect(box.textContent).toBe("https://.shpyrd.app");
    expect(box.contains(screen.getByTestId("icon"))).toBe(true);
    expect(box.contains(screen.getByPlaceholderText("hello"))).toBe(true);
  });

  it("gives what it is told to the field, not to the box", () => {
    render(<Input id="address" aria-invalid disabled suffix=".shpyrd.app" />);
    const field = screen.getByRole<HTMLInputElement>("textbox");
    expect(field.id).toBe("address");
    expect(field.getAttribute("aria-invalid")).toBe("true");
    expect(field.disabled).toBe(true);
  });

  it("puts the cursor in the field when what is beside it is pressed", () => {
    render(<Input suffix=".shpyrd.app" />);
    fireEvent.pointerDown(screen.getByText(".shpyrd.app"));
    expect(document.activeElement).toBe(screen.getByRole("textbox"));
  });
});
