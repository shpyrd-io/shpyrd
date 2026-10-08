import * as React from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { CodeInput } from "./code-input";

// The field as a screen uses it: it keeps the value it is given.
function Controlled(props: Partial<React.ComponentProps<typeof CodeInput>>) {
  const [value, setValue] = React.useState(props.value ?? "");
  return (
    <CodeInput
      {...props}
      value={value}
      onChange={(v) => {
        setValue(v);
        props.onChange?.(v);
      }}
    />
  );
}

const boxes = () => screen.getAllByRole<HTMLInputElement>("textbox");

describe("CodeInput", () => {
  it("draws a box per digit, each named for who cannot see", () => {
    render(<Controlled length={4} />);
    expect(boxes()).toHaveLength(4);
    expect(boxes()[0].getAttribute("aria-label")).toBe("Digit 1 of 4");
  });

  it("goes to the next box as a digit is typed", () => {
    render(<Controlled />);
    fireEvent.change(boxes()[0], { target: { value: "4" } });
    expect(boxes()[0].value).toBe("4");
    expect(document.activeElement).toBe(boxes()[1]);
  });

  it("ignores what is not a digit", () => {
    const onChange = vi.fn();
    render(<Controlled onChange={onChange} />);
    fireEvent.change(boxes()[0], { target: { value: "a" } });
    expect(onChange).not.toHaveBeenCalled();
  });

  it("fills every box from a code pasted into any of them, and says it is complete", () => {
    const onComplete = vi.fn();
    render(<Controlled onComplete={onComplete} />);
    fireEvent.paste(boxes()[3], { clipboardData: { getData: () => "12 34-56" } });
    expect(boxes().map((b) => b.value).join("")).toBe("123456");
    expect(onComplete).toHaveBeenCalledTimes(1);
    expect(onComplete).toHaveBeenCalledWith("123456");
  });

  it("goes back a box when an empty one is deleted", () => {
    const onChange = vi.fn();
    render(<Controlled value="12" onChange={onChange} />);
    fireEvent.keyDown(boxes()[2], { key: "Backspace" });
    expect(onChange).toHaveBeenLastCalledWith("1");
    expect(document.activeElement).toBe(boxes()[1]);
  });

  it("names each box after the field, for the form and analytics", () => {
    render(<Controlled name="otp" />);
    expect(boxes()[0].id).toBe("otp-1");
    expect(boxes()[5].getAttribute("name")).toBe("otp-6");
  });

  it("marks every box when the code is wrong", () => {
    render(<Controlled invalid />);
    for (const box of boxes()) expect(box.getAttribute("aria-invalid")).toBe("true");
  });
});
